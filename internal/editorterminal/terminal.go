package editorterminal

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/editorsandbox"
	"github.com/pders01/sp/internal/privateedit"
	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/tui"
	"golang.org/x/term"
)

var (
	ErrTerminal     = errors.New("editorterminal: matching supported live input/output terminals required")
	ErrForeground   = errors.New("editorterminal: controlling foreground terminal required")
	ErrRetained     = errors.New("editorterminal: editor/input remains owned; not safely closed")
	ErrInputPending = errors.New("editorterminal: unsubmitted or uncertain input retained")
	ErrOutput       = errors.New("editorterminal: terminal output failed")
)

type Options struct {
	Input, Output         *os.File
	Manager               *scratchpad.Manager
	DocumentID            string
	Driver                editorcore.Driver
	InMemory              editorcore.InMemoryFactory   // Only RunInMemory; no native or OS protection claim.
	Experiment            editorcore.ExperimentFactory // Only RunExperiment; never a protected fallback.
	ForwardEditorControls bool                         // Explicit modal-editor profile; Ctrl+Q/Z remain parent-owned shutdown.
}

// Input contains PRIVATE event text. It is never a reconstructible claim of the
// latest full note, and must not be logged. Partial input must not be replayed.
type Input struct {
	Text, Key      string
	Paste, Partial bool
}

type Report struct {
	Editor                 *privateedit.Embedded
	Outcome                privateedit.Result
	Relocked, PendingInput bool
	pending                []Input
	continuation           *continuation
}

func (r Report) RetainedInput() []Input { return append([]Input(nil), r.pending...) }

// CheckTerminal never opens a fallback terminal or changes terminal state.
func CheckTerminal(input, output *os.File) error {
	if input == nil || output == nil || !platformSupported() || !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) || !sameTerminal(input, output) {
		return ErrTerminal
	}
	if !foregroundTerminal(input) || !foregroundTerminal(output) {
		return ErrForeground
	}
	return nil
}

type outputOwner struct {
	file   *os.File
	failed chan struct{}
	once   sync.Once
}

func (o *outputOwner) Write(b []byte) (int, error) {
	n, err := o.file.Write(b)
	if err != nil {
		o.once.Do(func() { close(o.failed) })
	}
	return n, err
}
func (o *outputOwner) Fd() uintptr { return o.file.Fd() }

// Run returns an owner even on failure. Caller MUST retain it if not closed;
// neither a terminal failure nor context cancellation authorizes dropping a
// reachable draft. Store calls are never detached. Blocked-output/hard-reap and
// full terminal/command policy assurance are not established by this prototype.
func Run(ctx context.Context, o Options) (Report, error) {
	if err := editorsandbox.Require(ctx, editorsandbox.NoPersistence()); err != nil {
		return Report{Outcome: privateedit.Result{Disposition: privateedit.Cancelled, Closed: true}}, safeError(err)
	}
	// Protected admission never considers the experimental factory.
	o.Experiment = nil
	o.InMemory = nil
	return runWithFactory(ctx, o, nil)
}

// RunExperiment is solely for explicitly synthetic fixtures. It conveys no OS
// enforcement claim and is never called by Run after a failed protected open.
func RunExperiment(ctx context.Context, o Options) (Report, error) {
	if o.Experiment == nil || o.InMemory != nil {
		return Report{}, ErrRetained
	}
	return runWithFactory(ctx, o, nil)
}

// RunInMemory is explicit opt-in to a trusted in-process implementation. It is
// never a protected fallback and cannot select an executable or public asset path.
func RunInMemory(ctx context.Context, o Options) (Report, error) {
	if o.InMemory == nil || o.Experiment != nil || o.Driver != nil {
		return Report{}, ErrRetained
	}
	return runWithFactory(ctx, o, nil)
}

// The private factory seam lets package tests drive real PTYs and Neovim while
// injecting lifecycle failures, without adding fault flags to the demo or CLI.
func runWithFactory(ctx context.Context, o Options, factory func(*model, int, int) (editor, error)) (result Report, resultErr error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return Report{Outcome: privateedit.Result{Disposition: privateedit.Cancelled, Closed: true}}, safeError(contextErr)
	}
	if err := CheckTerminal(o.Input, o.Output); err != nil {
		return Report{}, err
	}
	if o.Manager == nil {
		return Report{}, ErrRetained
	}
	width, height, err := term.GetSize(int(o.Output.Fd()))
	if err != nil {
		return Report{}, ErrTerminal
	}
	watch, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, terminationSignals()...)
	defer signal.Stop(signals)
	// Capture before Tea enters raw mode. Input and output are the same tty;
	// inner output/input lifecycle snapshots must not leave it raw on return.
	original, stateErr := term.GetState(int(o.Input.Fd()))
	if stateErr != nil {
		return Report{}, ErrTerminal
	}
	defer func() {
		if restoreErr := term.Restore(int(o.Input.Fd()), original); restoreErr != nil {
			resultErr = safeError(errors.Join(resultErr, ErrTerminal))
		}
	}()
	output := &outputOwner{file: o.Output, failed: make(chan struct{})}
	jobs := &jobOwner{accepting: true}
	m := newModel(watch, ctx, signals, output.failed, jobs, width, height)
	m.forwardEditorControls = o.ForwardEditorControls
	m.open = func(w, h int) (editor, error) {
		options := editorcore.Options{DocumentID: o.DocumentID, Width: w, Height: h}
		var owner *privateedit.Embedded
		var err error
		switch {
		case o.InMemory != nil:
			owner, err = privateedit.OpenInMemory(context.Background(), o.Manager, options, o.InMemory)
		case o.Experiment != nil:
			owner, err = privateedit.OpenExperimentWithDriver(context.Background(), o.Manager, options, o.Experiment)
		default:
			owner, err = privateedit.OpenDriver(context.Background(), o.Manager, options, o.Driver)
		}
		if owner == nil {
			return nil, err
		}
		return owner, err
	}
	if factory != nil {
		m.open = func(w, h int) (editor, error) { return factory(m, w, h) }
	}
	_, runErr := tui.RunTerminalProgram(m, o.Input, tea.WithAltScreen(), tea.WithOutput(output), tea.WithoutSignalHandler())
	cancel()
	jobs.stopAndWait()
	runErr = errors.Join(runErr, m.exitErr)
	owner := jobs.owner()
	if owner == nil {
		owner = m.editor
	}
	last := jobs.lastResult()
	report := Report{pending: unsettledInput(m, last)}
	lateFailure := last.sequence > m.completed && last.err != nil
	if lateFailure {
		runErr = errors.Join(runErr, last.err)
	}
	failedCancellation := m.shutdownFailed || (lateFailure && (last.kind == "cancel" || last.kind == "known"))
	report.PendingInput = len(report.pending) != 0
	if live, ok := owner.(*privateedit.Embedded); ok {
		report.Editor = live
	}
	if owner != nil {
		// Program errors/EOF may end Tea before an action is delivered. Only
		// exact encrypted cancellation is automatic; never a known-copy discard.
		if !owner.Retained().Editor.Closed {
			switch {
			case report.PendingInput:
				runErr = errors.Join(runErr, ErrInputPending)
			case failedCancellation:
				runErr = errors.Join(runErr, ErrRetained)
			default:
				cleanupErr := settleCancellation(owner)
				runErr = errors.Join(runErr, cleanupErr)
			}
		}
		report.Outcome = owner.Retained().Result
	} else {
		report.Outcome = privateedit.Result{Disposition: privateedit.Cancelled, Closed: true}
	}
	if report.Outcome.Closed && !report.PendingInput && m.relock {
		o.Manager.Lock()
		report.Relocked = true
	}
	select {
	case <-output.failed:
		runErr = errors.Join(runErr, ErrOutput)
	default:
	}
	if !report.Outcome.Closed || report.PendingInput {
		runErr = errors.Join(runErr, ErrRetained)
	}
	if o.InMemory != nil && report.Editor != nil && (!report.Outcome.Closed || report.PendingInput) {
		report.continuation = &continuation{manager: o.Manager, document: o.DocumentID, editor: report.Editor}
	}
	if last.kind == "known" && last.err == nil && last.outcome.Closed && last.outcome.MayHaveLostEdits {
		report.Outcome.MayHaveLostEdits = true
	}
	return report, safeError(runErr)
}

// Called after jobs.stopAndWait: a scheduled-but-unstarted event is still
// owned; only an exact completed successful input result can release it.
func unsettledInput(m *model, last jobMsg) []Input {
	pending := append([]Input(nil), m.queue...)
	if m.held != nil {
		pending = append([]Input{*m.held}, pending...)
	}
	if m.activeInput != nil {
		event := *m.activeInput
		if last.sequence > m.completed && last.kind == "input" {
			if last.err == nil {
				return pending
			}
			event.Partial = !errors.Is(last.err, editorcore.ErrBackpressure) || last.count != 0
		}
		pending = append([]Input{event}, pending...)
	} else if last.sequence > m.completed && last.kind == "input" && last.err != nil {
		event := last.input
		event.Partial = true
		pending = append([]Input{event}, pending...)
	}
	return pending
}

func settleCancellation(e editor) error {
	ctx := context.Background()
	if err := e.Resolve(ctx); err != nil {
		return ErrRetained
	}
	if !e.Retained().Editor.Mirror.Frozen {
		if _, err := e.Key(ctx, "Cancel"); err != nil && !errors.Is(err, editorcore.ErrFrozen) {
			return ErrRetained
		}
		wait, cancel := context.WithTimeout(ctx, actionTimeout)
		defer cancel()
		if _, err := e.WaitAction(wait); err != nil {
			return ErrRetained
		}
	}
	_, err := e.Cancel(ctx, true)
	return err
}

type terminalError struct{ cause error }

func (e *terminalError) Error() string {
	return "editorterminal: terminal session did not complete safely"
}
func (e *terminalError) Unwrap() error { return e.cause }
func safeError(err error) error {
	if err == nil {
		return nil
	}
	return &terminalError{err}
}
