package sharedmcp

import (
	"bufio"
	"io"
	"os"
	"time"
)

const operationBudget = 10 * time.Second
const drainGrace = time.Second

type frame struct {
	data []byte
	err  error
}

func readFrames(input io.Reader, frames chan<- frame, done <-chan struct{}) {
	r := bufio.NewReaderSize(input, maxFrame)
	for {
		line, err := r.ReadSlice('\n')
		if err != nil {
			if err == io.EOF && len(line) != 0 {
				err = errInvalid
			}
			select {
			case frames <- frame{err: err}:
			case <-done:
			}
			return
		}
		// ReadSlice's backing buffer is reused. At most this frame and the
		// reader's single blocked handoff exist; there is no request queue.
		data := append([]byte(nil), line...)
		select {
		case frames <- frame{data: data}:
		case <-done:
			return
		}
	}
}

type writeJob struct {
	data []byte
	done chan error
}

func writeFrames(output io.Writer, jobs <-chan writeJob, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case job := <-jobs:
			data := job.data
			var err error
			for len(data) != 0 && err == nil {
				var n int
				n, err = output.Write(data)
				if n < 0 || n > len(data) {
					err = io.ErrShortWrite
					break
				}
				data = data[n:]
				if n == 0 && err == nil {
					err = io.ErrShortWrite
				}
			}
			job.done <- err
			if err != nil {
				return
			}
		}
	}
}

type activeCall struct {
	id       any
	call     toolCall
	result   <-chan toolResult
	deadline time.Time
}

// serveProcess is NOT a reusable server API. Its sole production caller MUST
// immediately os.Exit with its return code. Backend locks have no contexts:
// even a fatal return may still own a blocked storage goroutine. Only whole-
// process termination, not closing a Go channel or returning to a host, ends
// that worker. Tests of lifecycle use subprocesses for the same reason.
func serveProcess(cfg config, store sharedStore, input io.Reader, output io.Writer, signals <-chan os.Signal, budget, grace time.Duration) int {
	done := make(chan struct{})
	defer close(done)
	frames := make(chan frame)
	jobs := make(chan writeJob)
	go readFrames(input, frames, done)
	go writeFrames(output, jobs, done)
	emitBefore := func(id, result any, rpcErr *rpcError, limit time.Time) bool {
		data, ok := encodeResponse(id, result, rpcErr)
		if !ok {
			data, _ = encodeResponse(id, failure("OUTPUT_TOO_LARGE", "not_applicable"), nil)
		}
		remaining := time.Until(limit)
		if remaining <= 0 {
			return false
		}
		timer := time.NewTimer(remaining)
		defer timer.Stop()
		job := writeJob{data: data, done: make(chan error, 1)}
		select {
		case jobs <- job:
		case <-signals:
			return false
		case <-timer.C:
			return false
		}
		select {
		case err := <-job.done:
			return err == nil && time.Now().Before(limit)
		case <-signals:
			return false
		case <-timer.C:
			return false
		}
	}
	emit := func(id, result any, rpcErr *rpcError) bool {
		return emitBefore(id, result, rpcErr, time.Now().Add(grace))
	}
	var active *activeCall
	// EOF/signals/cancellation stop accepting ALL calls. Wait at most the grace
	// or remaining operation budget. Never detach work and continue serving.
	drain := func(code int) int {
		if active == nil {
			return code
		}
		wait := grace
		if remaining := time.Until(active.deadline); remaining < wait {
			wait = remaining
		}
		if wait <= 0 {
			return 1
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-active.result:
			return code
		case <-timer.C:
			return 1
		}
	}
	state := 0 // 0: new; 1: initialize replied; 2: initialized notification
	for {
		var results <-chan toolResult
		var deadline <-chan time.Time
		var timer *time.Timer
		if active != nil {
			if !time.Now().Before(active.deadline) {
				return 1
			}
			results = active.result
			timer = time.NewTimer(time.Until(active.deadline))
			deadline = timer.C
		}
		var f frame
		select {
		case <-signals:
			if timer != nil {
				timer.Stop()
			}
			return drain(1)
		case <-deadline:
			// No final reply is promised: deadline ends the whole process.
			return 1
		case result := <-results:
			if timer != nil {
				timer.Stop()
			}
			if !time.Now().Before(active.deadline) {
				return 1
			}
			id := active.id
			active = nil
			if !emit(id, result, nil) {
				return 1
			}
			continue
		case f = <-frames:
			if timer != nil {
				timer.Stop()
			}
		}
		if active != nil && !time.Now().Before(active.deadline) {
			return 1
		}
		if f.err != nil {
			if f.err == io.EOF {
				return drain(0)
			}
			return drain(1) // oversized or partial frame: no prefix dispatch
		}
		r, code := decodeRequest(f.data)
		if code != 0 {
			if !emit(nil, nil, protocolError(code)) {
				return 1
			}
			continue
		}
		if r.id == nil { // Only the two defined notifications have effects.
			switch r.method {
			case "notifications/initialized":
				if _, ok := object(r.params, nil); state == 1 && ok {
					state = 2
				}
			case "notifications/cancelled":
				m, ok := object(r.params, []string{"requestId"}, "reason")
				if !ok || !validID(m["requestId"]) {
					continue
				}
				if reason, exists := m["reason"]; exists {
					if _, ok := text(reason, 128); !ok {
						continue
					}
				}
				if active != nil && sameID(active.id, m["requestId"]) {
					// Cancellation is NOT rollback. A cancelled session exits;
					// it cannot dispatch another call while this worker mutates.
					result := failure("STORE_UNAVAILABLE", "not_applicable")
					if active.call.append {
						result = failure("APPEND_UNCONFIRMED", "unknown")
					}
					limit := time.Now().Add(grace)
					if active.deadline.Before(limit) {
						limit = active.deadline
					}
					if !emitBefore(active.id, result, nil, limit) {
						return 1
					}
					if time.Now().After(limit) {
						return 1
					}
					remaining := time.Until(limit)
					if op := time.Until(active.deadline); op < remaining {
						remaining = op
					}
					if remaining <= 0 {
						return 1
					}
					t := time.NewTimer(remaining)
					select {
					case <-active.result:
						t.Stop()
						return 0
					case <-t.C:
						return 1
					}
				}
			}
			continue
		}
		if active != nil && sameID(active.id, r.id) {
			if !emit(nil, nil, protocolError(-32600)) {
				return 1
			}
			continue
		}
		var result any
		var rpcErr *rpcError
		switch r.method {
		case "ping":
			if _, ok := object(r.params, nil); !ok {
				rpcErr = protocolError(-32602)
			} else {
				result = map[string]any{}
			}
		case "initialize":
			if state != 0 {
				rpcErr = protocolError(-32600)
				break
			}
			var ok bool
			result, ok = initialize(r.params)
			if !ok {
				rpcErr = protocolError(-32602)
			} else {
				state = 1
			}
		case "tools/list":
			if state != 2 {
				rpcErr = protocolError(-32600)
				break
			}
			if _, ok := object(r.params, nil); !ok {
				rpcErr = protocolError(-32602)
			} else {
				result = toolsList()
			}
		case "tools/call":
			if state != 2 {
				rpcErr = protocolError(-32600)
				break
			}
			if active != nil {
				result = failure("BUSY", "not_attempted")
				break
			}
			call, rejected := prepare(r.params, cfg, r.id)
			if rejected != nil {
				result = *rejected
				break
			}
			// Check an already-delivered shutdown before dispatch. A signal or
			// cancellation racing dispatch cannot be a no-write guarantee.
			select {
			case <-signals:
				return 1
			default:
			}
			ch := make(chan toolResult, 1)
			active = &activeCall{r.id, call, ch, time.Now().Add(budget)}
			go func() {
				// Sanitize even unexpected backend panics; do not expose a stack
				// trace or diagnose them through the protocol.
				defer func() {
					if recover() != nil {
						if call.append {
							ch <- failure("APPEND_UNCONFIRMED", "unknown")
						} else {
							ch <- failure("STORE_UNAVAILABLE", "not_applicable")
						}
					}
				}()
				ch <- execute(store, call)
			}()
			continue
		default:
			rpcErr = protocolError(-32601)
		}
		if !emit(r.id, result, rpcErr) {
			return 1
		}
	}
}
