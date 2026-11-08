package editorterminal

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/privateedit"
	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/tui"
)

const actionTimeout = 4 * time.Second
const maxQueueEvents = 128
const maxQueueBytes = 1 << 20

type editor interface {
	Retained() privateedit.Retention
	Frame() editorcore.Frame
	Changed() <-chan struct{}
	Type(context.Context, string) (int, error)
	Key(context.Context, string) (int, error)
	InsertText(context.Context, string) (editorcore.Snapshot, error)
	InputReceipt() editorcore.InputReceipt
	Resize(context.Context, int, int) error
	WaitAction(context.Context) (editorcore.Action, error)
	Resolve(context.Context) error
	Resume(context.Context) error
	Finish(context.Context) (privateedit.Result, error)
	Cancel(context.Context, bool) (privateedit.Result, error)
	RecoverKnown(context.Context, bool) (privateedit.Result, error)
}

// checkpointEditor is optional so existing editors and lifecycle fakes need not
// implement save-without-close. A save action never falls back to Finish.
type checkpointEditor interface {
	Checkpoint(context.Context) (privateedit.Result, error)
}

type jobMsg struct {
	sequence uint64
	kind     string
	owner    editor
	input    Input
	before   uint64
	count    int
	receipt  editorcore.InputReceipt // Captured before another operation can replace it.
	outcome  privateedit.Result
	err      error
}
type frameMsg struct{}
type actionMsg struct {
	epoch uint64
	kind  string
	err   error
}
type stopMsg struct{ reason string }

type jobOwner struct {
	mu        sync.Mutex
	wg        sync.WaitGroup
	accepting bool
	running   bool
	current   editor
	sequence  uint64
	last      jobMsg
}

func (j *jobOwner) command(fn func() jobMsg) tea.Cmd {
	return func() tea.Msg {
		j.mu.Lock()
		if !j.accepting || j.running {
			j.mu.Unlock()
			return nil
		}
		j.running = true
		j.wg.Add(1)
		j.sequence++
		sequence := j.sequence
		j.mu.Unlock()
		defer j.wg.Done()
		result := fn()
		result.sequence = sequence
		j.mu.Lock()
		j.last = result
		j.running = false
		if result.owner != nil {
			j.current = result.owner
		}
		j.mu.Unlock()
		return result
	}
}
func (j *jobOwner) stopAndWait()       { j.mu.Lock(); j.accepting = false; j.mu.Unlock(); j.wg.Wait() }
func (j *jobOwner) owner() editor      { j.mu.Lock(); defer j.mu.Unlock(); return j.current }
func (j *jobOwner) lastResult() jobMsg { j.mu.Lock(); defer j.mu.Unlock(); return j.last }

type model struct {
	watch, caller                                      context.Context
	signals                                            <-chan os.Signal
	outputFailed                                       <-chan struct{}
	inputEnded                                         chan struct{}
	inputOnce                                          sync.Once
	jobs                                               *jobOwner
	open                                               func(int, int) (editor, error)
	editor                                             editor
	width, height                                      int
	phase, status, lastOp, actionKind                  string
	busy, waitingFreeze, stopping, relock, resizeDirty bool
	epoch                                              uint64
	completed                                          uint64
	queue                                              []Input
	held                                               *Input
	inputPaused                                        bool // One overflow event stays at the FIFO tail; reject further intake until drained.
	heldBefore                                         uint64
	activeInput                                        *Input // Own a scheduled event even if Tea never starts its command.
	shutdownFailed                                     bool   // Incidental messages cannot retry a failed shutdown.
	exitErr                                            error
	forwardEditorControls                              bool
	reopening, transportRecovery                       bool
}

func newModel(watch, caller context.Context, signals <-chan os.Signal, output <-chan struct{}, jobs *jobOwner, width, height int) *model {
	return &model{watch: watch, caller: caller, signals: signals, outputFailed: output, jobs: jobs, width: width, height: height, phase: "loading", inputEnded: make(chan struct{}), epoch: 1}
}
func (m *model) TerminalInputEnded() { m.inputOnce.Do(func() { close(m.inputEnded) }) }

// quarantined input came from another terminal program. No later receipt,
// action or resume can settle it; only confirmed recovery/close releases it.
func (m *model) quarantined() bool {
	return m.transportRecovery && (m.held != nil || len(m.queue) != 0)
}
func (m *model) small() bool { return m.width < 40 || m.height < 10 }
func (m *model) geometry() (width, height int) {
	chrome := 4
	if m.forwardEditorControls {
		chrome = 0 // The modal frame supplies its own status/prompt rows.
	}
	return min(m.width-1, editorcore.MaxColumns), min(m.height-chrome, editorcore.MaxRows)
}
func (m *model) Init() tea.Cmd {
	return tea.Batch(m.watchStop(m.caller.Done(), "context"), m.watchStop(m.inputEnded, "input"), m.watchStop(m.outputFailed, "output"), m.watchSignal(), m.next())
}
func (m *model) watchStop(source <-chan struct{}, reason string) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-source:
			return stopMsg{reason}
		case <-m.watch.Done():
			return nil
		}
	}
}
func (m *model) watchSignal() tea.Cmd {
	return func() tea.Msg {
		select {
		case <-m.signals:
			return stopMsg{"signal"}
		case <-m.watch.Done():
			return nil
		}
	}
}
func (m *model) watchFrame() tea.Cmd {
	e, ctx := m.editor, m.watch
	return func() tea.Msg {
		select {
		case <-e.Changed():
			return frameMsg{}
		case <-ctx.Done():
			return nil
		}
	}
}
func (m *model) watchAction() tea.Cmd {
	e, ctx, epoch := m.editor, m.watch, m.epoch
	return func() tea.Msg { a, err := e.WaitAction(ctx); return actionMsg{epoch: epoch, kind: a.Kind, err: err} }
}

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(message)
	return m, cmd
}

func (m *model) update(message tea.Msg) tea.Cmd {
	if _, paste := tui.TerminalLiteralPaste(message); paste {
		text, _ := tui.TerminalLiteralPaste(message)
		if m.phase == "editing" && !m.small() && !m.stopping && m.actionKind == "" && !m.waitingFreeze {
			m.enqueue(Input{Text: text, Paste: true})
		}
		return m.next()
	}
	if _, bad := tui.TerminalRejection(message); bad {
		m.status = "Input rejected; re-enter the complete event."
		return nil
	}
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeDirty = true
		return m.next()
	case frameMsg:
		return m.watchFrame()
	case stopMsg:
		m.requestStop()
		m.relock = true
		if msg.reason == "output" {
			m.exitErr = ErrOutput
		}
		var signalCmd tea.Cmd
		if msg.reason == "signal" {
			signalCmd = m.watchSignal()
		}
		return tea.Batch(signalCmd, m.next())
	case actionMsg:
		if msg.epoch != m.epoch || m.phase == "done" {
			return nil
		}
		// Reentry is recovery-only for quarantined events. An old action or
		// later input receipt cannot settle them or replace physical consent.
		if m.quarantined() {
			return nil
		}
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				return nil
			}
			m.phase = "error"
			m.status = "Editor fault; draft remains owned. K: known-copy recovery."
			m.lastOp = "fault"
			return nil
		}
		m.waitingFreeze = false
		m.actionKind = msg.kind
		// Events after any frozen action (including typed :w) belong to the
		// handoff/modal, never the automatically or explicitly resumed draft.
		m.queue = nil
		m.inputPaused = false
		return m.next()
	case jobMsg:
		return m.finished(msg)
	case tea.KeyMsg:
		return m.key(msg)
	default:
		return nil
	}
}

func (m *model) enqueue(event Input) {
	if m.held != nil || m.inputPaused {
		m.status = "Input paused; prior events and the last accepted event remain owned."
		return
	}
	size := len(event.Text)
	for _, entry := range m.queue {
		size += len(entry.Text)
	}
	if len(m.queue) >= maxQueueEvents || size > maxQueueBytes {
		m.inputPaused = true
		m.status = "Input queue full; last accepted event remains at the FIFO tail."
		// One bounded overflow event is owned in order, never retried ahead of
		// older input. Further events are rejected visibly until this drains.
		m.queue = append(m.queue, event)
		return
	}
	m.queue = append(m.queue, event)
}

func (m *model) key(k tea.KeyMsg) tea.Cmd {
	if k.Alt {
		return nil
	}
	if k.Type == tea.KeyCtrlQ || k.Type == tea.KeyCtrlZ || (!m.forwardEditorControls &&
		(k.Type == tea.KeyCtrlC || k.Type == tea.KeyCtrlD || k.Type == tea.KeyCtrlL)) {
		m.requestStop()
		m.relock = true
		return m.next()
	}
	if m.busy && m.phase != "editing" {
		return nil
	}
	if m.small() {
		m.status = "Input paused: resize before editing or confirmation."
		return nil
	}
	if m.phase == "confirm" || m.phase == "known-confirm" {
		if k.Type != tea.KeyRunes || len(k.Runes) != 1 {
			return nil
		}
		switch k.Runes[0] {
		case 'y', 'Y':
			if m.phase == "known-confirm" {
				return m.start("known", Input{})
			}
			return m.start("cancel", Input{})
		case 'n', 'N':
			if m.phase == "known-confirm" {
				m.phase = "error"
				return nil
			}
			return m.start("resume", Input{})
		}
		return nil
	}
	if m.phase == "error" {
		if k.Type != tea.KeyRunes || len(k.Runes) != 1 {
			return nil
		}
		switch k.Runes[0] {
		case 'r', 'R':
			if m.lastOp == "known" {
				m.confirmKnown()
				return nil
			}
			if m.quarantined() {
				m.status = "Retained input cannot be replayed or resolved here. K: confirmed possible-loss recovery."
				return nil
			}
			if m.lastOp == "retry-input" && m.held != nil && !m.held.Partial {
				event := *m.held
				m.held = nil
				m.phase = "editing"
				return m.startInput(event)
			}
			if m.lastOp == "resolve-input" || m.lastOp == "resolve-resume" {
				return m.start(m.lastOp, Input{})
			}
			if m.lastOp == "save" || m.lastOp == "finish" || m.lastOp == "cancel" {
				kind := m.lastOp
				if m.stopping {
					kind = "cancel"
				}
				m.shutdownFailed = false
				return m.start(kind, Input{})
			}
		case 'n', 'N':
			if m.held == nil && !m.stopping && !m.quarantined() {
				return m.start("resume", Input{})
			}
		case 'c', 'C':
			if m.held == nil && !m.quarantined() {
				m.shutdownFailed = false
				m.phase = "stopping"
				m.stopping = true
				return m.next()
			}
		case 'k', 'K':
			state := m.editor.Retained().Editor
			if m.transportRecovery || (m.held == nil && len(m.queue) == 0) || state.Fault != nil || state.LatestUnknown {
				m.confirmKnown()
			}
		}
		return nil
	}
	if m.phase != "editing" || m.stopping || m.actionKind != "" || m.waitingFreeze {
		return nil
	}
	var event Input
	switch k.Type {
	case tea.KeyRunes:
		if len(k.Runes) != 1 {
			return nil
		}
		event.Text = string(k.Runes)
	case tea.KeySpace:
		event.Text = " "
	default:
		keys := map[tea.KeyType]string{tea.KeyEsc: "Escape", tea.KeyEnter: "Enter", tea.KeyBackspace: "Backspace", tea.KeyDelete: "Delete", tea.KeyTab: "Tab", tea.KeyLeft: "Left", tea.KeyRight: "Right", tea.KeyUp: "Up", tea.KeyDown: "Down", tea.KeyHome: "Home", tea.KeyEnd: "End", tea.KeyPgUp: "PageUp", tea.KeyPgDown: "PageDown", tea.KeyCtrlR: "Redo", tea.KeyF2: "Finish", tea.KeyCtrlS: "Finish", tea.KeyF3: "Cancel"}
		var ok bool
		event.Key, ok = keys[k.Type]
		if m.forwardEditorControls && k.Type == tea.KeyInsert {
			event.Key, ok = "Insert", true
		}
		if m.forwardEditorControls && k.Type == tea.KeyCtrlS {
			event.Key, ok = "Save", true
		}
		if m.forwardEditorControls && k.Type == tea.KeyCtrlR {
			event.Key, ok = "CtrlR", true
		}
		if !ok && m.forwardEditorControls && k.Type >= tea.KeyCtrlA && k.Type <= tea.KeyCtrlZ {
			event.Key, ok = "Ctrl"+string(rune('A'+k.Type-tea.KeyCtrlA)), true
		}
		if !ok {
			m.status = "Unsupported key ignored by this prototype."
			return nil
		}
	}
	m.enqueue(event)
	return m.next()
}

func (m *model) requestStop() {
	m.stopping = true
	if !m.shutdownFailed && m.held == nil && (m.phase == "error" || m.phase == "confirm" || m.phase == "known-confirm") {
		m.phase = "stopping"
	}
}

func (m *model) confirmKnown() {
	m.phase = "known-confirm"
	m.status = "Known copies only: latest edits may be lost and pending keys discarded. Recover and close? [y/n]"
}

func (m *model) next() tea.Cmd {
	if m.busy || m.phase == "done" {
		return nil
	}
	if m.editor == nil {
		if m.stopping {
			m.phase = "done"
			return tea.Quit
		}
		if m.small() {
			return nil
		}
		return m.start("open", Input{})
	}
	if m.held != nil || m.quarantined() {
		return nil
	}
	if m.phase == "error" || m.phase == "confirm" || m.phase == "known-confirm" {
		return nil
	}
	if m.actionKind != "" {
		if m.stopping {
			return m.start("cancel", Input{})
		}
		if m.small() {
			m.phase = "action"
			return nil
		}
		if m.actionKind == "save" || m.actionKind == "finish" {
			return m.start(m.actionKind, Input{})
		}
		r := m.editor.Retained()
		if r.Editor.Mirror.Text != r.Baseline || r.Result.DurabilityUnconfirmed {
			m.phase = "confirm"
			m.status = "Recover encrypted draft and close? [y/n]"
			return nil
		}
		return m.start("cancel", Input{})
	}
	if m.waitingFreeze {
		return nil
	}
	if m.resizeDirty && !m.small() {
		m.resizeDirty = false
		return m.start("resize", Input{})
	}
	if len(m.queue) > 0 {
		event := m.queue[0]
		if m.stopping && (event.Key == "Finish" || event.Key == "Save") {
			event.Key = "Cancel"
		}
		if !m.stopping && m.small() && (event.Key == "Finish" || event.Key == "Cancel" || event.Key == "Save") {
			return nil
		}
		m.queue = m.queue[1:]
		if len(m.queue) == 0 {
			m.inputPaused = false
		}
		return m.startInput(event)
	}
	if m.stopping {
		m.waitingFreeze = true
		m.phase = "waiting"
		return m.startInput(Input{Key: "Cancel"})
	}
	m.phase = "editing"
	return nil
}
func (m *model) startInput(event Input) tea.Cmd {
	if m.busy || m.phase == "done" {
		return nil
	}
	owned := event
	m.activeInput = &owned
	if event.Key == "Finish" || event.Key == "Cancel" || event.Key == "Save" {
		m.waitingFreeze = true
		m.phase = "waiting"
	}
	return m.start("input", event)
}

func (m *model) start(kind string, event Input) tea.Cmd {
	if m.busy || m.phase == "done" {
		return nil
	}
	m.busy = true
	if kind != "input" && kind != "resize" {
		m.phase = "working"
	}
	e, open, w, h := m.editor, m.open, 0, 0
	if !m.small() {
		w, h = m.geometry()
	}
	before := uint64(0)
	if e != nil {
		before = e.InputReceipt().RequestID
	}
	return m.jobs.command(func() jobMsg {
		r := jobMsg{kind: kind, input: event, before: before, owner: e}
		ctx := context.Background()
		switch kind {
		case "open":
			r.owner, r.err = open(w, h)
		case "input":
			switch {
			case event.Paste:
				_, r.err = e.InsertText(ctx, event.Text)
			case event.Key != "":
				r.count, r.err = e.Key(ctx, event.Key)
			default:
				r.count, r.err = e.Type(ctx, event.Text)
			}
			r.receipt = e.InputReceipt()
		case "resize":
			r.err = e.Resize(ctx, w, h)
		case "save":
			if checkpoint, ok := e.(checkpointEditor); ok {
				r.outcome, r.err = checkpoint.Checkpoint(ctx)
			} else {
				r.err = editorcore.ErrUnsupported
			}
		case "finish":
			r.outcome, r.err = e.Finish(ctx)
		case "cancel":
			r.outcome, r.err = e.Cancel(ctx, true)
		case "resume":
			r.err = e.Resume(ctx)
		case "resolve-input", "resolve-resume":
			r.err = e.Resolve(ctx)
		case "known":
			state := e.Retained()
			if m.transportRecovery && !state.Editor.LatestUnknown && state.Editor.Fault == nil {
				if state.Editor.Closed {
					r.outcome = state.Result
					if state.Result.DurabilityUnconfirmed {
						r.err = ErrRetained
					}
				} else {
					r.err = settleCancellation(e)
					r.outcome = e.Retained().Result
				}
				if r.err == nil && r.outcome.Closed {
					r.outcome.MayHaveLostEdits = true
				}
			} else {
				r.outcome, r.err = e.RecoverKnown(ctx, true)
			}
		}
		return r
	})
}

func (m *model) finished(r jobMsg) tea.Cmd {
	if r.sequence != 0 && r.sequence <= m.completed {
		return nil
	}
	m.completed = r.sequence
	m.busy = false
	if r.kind == "input" {
		m.activeInput = nil
	}
	if r.kind == "open" {
		m.editor = r.owner
		if r.err != nil {
			m.exitErr = r.err
			if r.owner == nil || r.owner.Retained().Editor.Closed {
				m.phase = "done"
				return tea.Quit
			}
			m.phase = "error"
			m.status = "Startup failed; owner retained. K: known-copy recovery."
			return nil
		}
		if m.reopening {
			m.phase = "error"
			m.lastOp = "transport"
			m.status = "Prior terminal failed; owner/input retained. K: confirmed possible-loss recovery."
			if m.quarantined() {
				return m.watchFrame()
			}
			return tea.Batch(m.watchFrame(), m.watchAction())
		}
		m.phase = "editing"
		return tea.Batch(m.watchFrame(), m.watchAction(), m.next())
	}
	if r.err != nil {
		// A distinct unsupported rejection plus its OWN known zero-acceptance
		// receipt can be reported as rejected input, not uncertain transmission.
		// This covers over-budget paste without trapping an otherwise healthy
		// editor. Never infer this from a later mutable receipt or a generic error.
		if r.kind == "input" && r.input.Key != "Finish" && r.input.Key != "Save" && r.input.Key != "Cancel" &&
			r.count == 0 && errors.Is(r.err, editorcore.ErrUnsupported) && r.receipt.Known && r.receipt.RequestID > r.before && r.receipt.AcceptedBytes == 0 {
			state := m.editor.Retained().Editor
			if !state.LatestUnknown && state.Fault == nil && !state.Closed && !state.Mirror.Frozen {
				m.phase = "editing"
				m.status = "Input rejected before editing; document unchanged. Reduce input and try again."
				return m.next()
			}
		}
		m.phase = "error"
		m.lastOp = r.kind
		m.status = failureStatus(r.kind, r.err, m.editor.Retained().Result)
		if m.stopping {
			m.shutdownFailed = true
		}
		if r.kind == "input" {
			if errors.Is(r.err, context.DeadlineExceeded) || errors.Is(r.err, context.Canceled) || errors.Is(r.err, editorcore.ErrPending) {
				m.lastOp = "resolve-input"
			}
			if r.input.Key == "Finish" || r.input.Key == "Cancel" || r.input.Key == "Save" {
				m.waitingFreeze = false
			}
			if r.input.Text != "" || r.input.Paste || r.input.Key != "" {
				event := r.input
				event.Partial = true
				if errors.Is(r.err, editorcore.ErrBackpressure) && r.count == 0 {
					event.Partial = false
					m.lastOp = "retry-input"
				} else if errors.Is(r.err, context.DeadlineExceeded) || errors.Is(r.err, context.Canceled) || errors.Is(r.err, editorcore.ErrPending) {
					m.lastOp = "resolve-input"
				}
				m.held = &event
				m.heldBefore = r.before
			}
		}
		if r.kind == "resume" {
			m.lastOp = "resolve-resume"
		}
		// A close can fail after actual publication/reaping. Never turn that
		// outcome into "not saved" or keep a dead screen waiting for keys.
		if m.editor.Retained().Editor.Closed && !(r.kind == "known" && (m.held != nil || len(m.queue) != 0)) {
			m.exitErr = r.err
			m.phase = "done"
			return tea.Quit
		}
		return nil
	}
	if (m.forwardEditorControls || m.status == "Saved encrypted.") && r.kind == "input" && m.phase == "editing" && !m.inputPaused {
		// Acknowledged new input dismisses a transient hint, not a fault or
		// unsettled-input warning. Ordinary keystrokes need no busy footer.
		m.status = ""
	}
	// A synchronous editor may freeze from an ordinary typed Ex command, not
	// only a reserved Finish/Cancel key. Do not submit queued trailing input
	// while its action broadcast is still in flight to Tea.
	if r.kind == "input" && m.actionKind == "" && m.editor.Retained().Editor.Mirror.Frozen {
		m.waitingFreeze = true
		m.phase = "waiting"
	}
	switch r.kind {
	case "save":
		cmd := m.resumed()
		m.status = "Saved encrypted."
		return cmd
	case "finish", "cancel", "known":
		if r.outcome.Closed {
			if r.kind == "known" {
				// Only the separately confirmed known-copy operation authorizes
				// dropping unsent/uncertain events, AFTER durable recovery/close.
				if m.held != nil {
					*m.held = Input{}
				}
				clear(m.queue)
				m.held, m.queue, m.activeInput = nil, nil, nil
				m.inputPaused = false
			}
			m.phase = "done"
			return tea.Quit
		}
	case "resume":
		return m.resumed()
	case "resolve-resume":
		if !m.editor.Retained().Editor.Mirror.Frozen {
			return m.resumed()
		}
		m.phase = "error"
		m.status = "Resume remains unresolved; draft retained."
		return nil
	case "resolve-input":
		if m.quarantined() {
			m.phase, m.lastOp = "error", "transport"
			return nil
		}
		if m.held != nil {
			if !m.held.Paste {
				receipt := m.editor.InputReceipt()
				if !receipt.Known || receipt.RequestID <= m.heldBefore {
					m.phase = "error"
					m.status = "Input acknowledgement unknown; event retained."
					return nil
				}
				if receipt.AcceptedBytes != receipt.RequestedBytes {
					m.held.Partial = receipt.AcceptedBytes != 0
					m.phase = "error"
					m.lastOp = "retry-input"
					m.status = "Input was not fully queued; event retained."
					return nil
				}
			}
			m.held = nil
		}
		m.phase = "editing"
	}
	return m.next()
}
func (m *model) resumed() tea.Cmd {
	if m.quarantined() {
		m.phase, m.lastOp = "error", "transport"
		return nil
	}
	// Revoke queued handoff input and the old action watcher before starting
	// another epoch. In particular :w's trailing events must not auto-replay.
	clear(m.queue)
	m.queue = nil
	m.inputPaused = false
	m.epoch++
	m.actionKind = ""
	m.waitingFreeze = false
	m.phase = "editing"
	m.reopening, m.transportRecovery = false, false
	m.status = "Editing resumed; old queued keys discarded."
	return tea.Batch(m.watchAction(), m.next())
}

func failureStatus(kind string, err error, facts privateedit.Result) string {
	status := errorStatus(err)
	uncertain := errors.Is(err, scratchpad.ErrDurabilityUnconfirmed)
	if uncertain && (kind == "cancel" || kind == "known") {
		status = "Encrypted recovery published; durability unconfirmed. Owner retained. R retry."
	}
	if facts.Published && ((kind != "finish" && kind != "save") || !uncertain) {
		prefix := "Earlier note publication succeeded. "
		if facts.DurabilityUnconfirmed {
			prefix = "Published; durability unconfirmed. "
		}
		status = prefix + status
	}
	return status
}

func errorStatus(err error) string {
	switch {
	case errors.Is(err, scratchpad.ErrConflict):
		return "Conflict: latest save rejected; stored bytes preserved. R retry, N resume, C encrypted cancel."
	case errors.Is(err, scratchpad.ErrPrivacyPolicyChanged):
		return "Privacy policy changed; plaintext save refused. C encrypted cancel."
	case errors.Is(err, scratchpad.ErrLocked):
		return "Authentication changed/locked; save refused. C encrypted cancel."
	case errors.Is(err, scratchpad.ErrDurabilityUnconfirmed):
		return "Published; durability unconfirmed. R retry, C encrypted recovery."
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, editorcore.ErrPending):
		return "Operation pending; original request retained. R resolve; do not resend."
	case errors.Is(err, editorcore.ErrBackpressure):
		return "Input backpressure: event retained. R retries only after zero acceptance."
	default:
		return "Operation failed; draft remains owned. R retry/resolve, C encrypted cancel, K known copies."
	}
}
