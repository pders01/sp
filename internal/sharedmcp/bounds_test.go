package sharedmcp

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendDecodedByteBoundaryAndPreflight(t *testing.T) {
	cfg := config{append: map[string]bool{testDate: true}}
	labels := len(testDate) + len("Updates") + len("synthetic")
	bullets := []string{strings.Repeat("<", 8192), strings.Repeat("&", 8192), strings.Repeat(">", 8192), strings.Repeat("<", maxAppend-labels-3*8192)}
	p := callParams("sp_shared_append", appendArgs(bullets...))
	call, rejected := prepare(p, cfg, strings.Repeat("\x00", 128))
	if rejected != nil {
		t.Fatal("exact decoded-byte limit rejected")
	}
	if len(call.bullets) != 4 {
		t.Fatal("preflight changed bullets")
	}
	worst := success(appendResult{call.date, call.section, call.source, call.bullets, call.bullets, true, provenance})
	b, ok := encodeResponse(strings.Repeat("\x00", 128), worst, nil)
	if !ok || len(b) > maxOutput {
		t.Fatal("preflight did not reserve dual representations")
	}
	bullets[3] += "x"
	_, rejected = prepare(callParams("sp_shared_append", appendArgs(bullets...)), cfg, 1)
	if rejected == nil {
		t.Fatal("decoded-byte limit bypassed")
	}
	toolError(t, *rejected, "INVALID_INPUT", "not_attempted")
}

func TestCompleteFrameBoundaryAtomicPublication(t *testing.T) {
	request := map[string]any{"jsonrpc": "2.0", "id": 10, "method": "tools/call", "params": callParams("sp_shared_append", appendArgs("public"))}
	prefix := string(mustJSON(t, request))
	for _, extra := range []int{0, 1} {
		t.Run(string(rune('0'+extra)), func(t *testing.T) {
			store := t.TempDir()
			s := startSession(t, store, "", "")
			s.initialized(t)
			frame := prefix + strings.Repeat(" ", maxFrame-len(prefix)-1+extra) + "\n"
			s.raw(t, frame)
			if extra == 0 {
				if s.read(t)["result"].(map[string]any)["isError"] != false {
					t.Fatal("exact frame boundary rejected")
				}
				_ = s.in.Close()
				s.wait(t, true)
				if _, err := os.Stat(filepath.Join(store, testDate+".json")); err != nil {
					t.Fatal("accepted append missing")
				}
			} else {
				s.wait(t, false)
				if _, err := os.Stat(filepath.Join(store, testDate+".json")); !os.IsNotExist(err) {
					t.Fatal("oversized frame prefix published")
				}
			}
		})
	}
}

func TestCancellationUnknownIDAndBeforeInitializationAreInert(t *testing.T) {
	store := t.TempDir()
	s := startSession(t, store, "", "")
	// No queued work exists before dispatch. Cancellation of an unknown ID
	// neither reserves that ID nor authorizes a future request.
	s.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": 10}})
	s.call(t, 10, true)
	if s.read(t)["error"] == nil {
		t.Fatal("pre-initialization call dispatched")
	}
	if _, err := os.Stat(filepath.Join(store, testDate+".json")); !os.IsNotExist(err) {
		t.Fatal("pre-initialization mutation")
	}
	s.initialized(t)
	s.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": 999}})
	s.call(t, 10, true)
	if s.read(t)["result"].(map[string]any)["isError"] != false {
		t.Fatal("unknown cancellation poisoned a future request")
	}
	_ = s.in.Close()
	s.wait(t, true)
}

func TestWriteFramesShortWriterAndErrors(t *testing.T) {
	for _, fail := range []bool{false, true} {
		w := &shortWriter{fail: fail}
		jobs := make(chan writeJob)
		done := make(chan struct{})
		finished := make(chan struct{})
		go func() { writeFrames(w, jobs, done); close(finished) }()
		job := writeJob{data: []byte("synthetic\n"), done: make(chan error, 1)}
		jobs <- job
		err := <-job.done
		if fail && err == nil {
			t.Fatal("zero-byte write accepted")
		}
		if !fail && (err != nil || w.buf.String() != "synthetic\n") {
			t.Fatal("short writes not completed")
		}
		close(done)
		<-finished
	}
}

type shortWriter struct {
	buf  bytes.Buffer
	fail bool
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if w.fail {
		return 0, nil
	}
	if len(p) > 2 {
		p = p[:2]
	}
	return w.buf.Write(p)
}

func TestReadFramesRejectsPartialAndBounds(t *testing.T) {
	for _, input := range []string{"{}", strings.Repeat("x", maxFrame), "{}\n"} {
		frames := make(chan frame)
		done := make(chan struct{})
		finished := make(chan struct{})
		go func() { readFrames(strings.NewReader(input), frames, done); close(finished) }()
		f := <-frames
		if strings.HasSuffix(input, "\n") {
			if f.err != nil || string(f.data) != input {
				t.Fatal("complete frame changed")
			}
			f = <-frames
			if f.err != io.EOF {
				t.Fatal("missing EOF")
			}
		} else if f.err == nil || len(f.data) != 0 {
			t.Fatal("unterminated/oversized frame leaked prefix")
		}
		close(done)
		<-finished
	}
}
