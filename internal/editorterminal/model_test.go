package editorterminal

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/privateedit"
	"github.com/pders01/sp/internal/scratchpad"
)

const testPrivate = "SYNTHETIC-TERMINAL-PRIVATE-CANARY"

type fakeEditor struct {
	mu                         sync.Mutex
	r                          privateedit.Retention
	keys                       []string
	typed, pasted              []string
	finishes, cancels, resumes int
	finishErr, cancelErr       error
	receipt                    editorcore.InputReceipt
}

func fixtureEditor() *fakeEditor {
	return &fakeEditor{r: privateedit.Retention{Baseline: "baseline", Editor: editorcore.Retention{Mirror: editorcore.Snapshot{SessionID: "synthetic", DocumentID: "2000-01-01", Revision: 1, Text: testPrivate}}, Result: privateedit.Result{Disposition: privateedit.Active}}}
}
func (e *fakeEditor) Retained() privateedit.Retention { e.mu.Lock(); defer e.mu.Unlock(); return e.r }
func (e *fakeEditor) Frame() editorcore.Frame {
	return editorcore.Frame{Width: 39, Height: 6, Lines: []string{testPrivate, "", "", "", "", ""}}
}
func (e *fakeEditor) Changed() <-chan struct{} { return make(chan struct{}) }
func (e *fakeEditor) Type(_ context.Context, text string) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.typed = append(e.typed, text)
	e.r.Editor.Mirror.Text += text
	e.r.Editor.Mirror.Revision++
	e.receipt.RequestID++
	e.receipt.Known = true
	e.receipt.RequestedBytes = len(text)
	e.receipt.AcceptedBytes = len(text)
	return len(text), nil
}
func (e *fakeEditor) Key(_ context.Context, key string) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.keys = append(e.keys, key)
	if key == "Finish" || key == "Cancel" {
		e.r.Editor.Mirror.Frozen = true
		e.r.Editor.LatestUnknown = false
	}
	return 1, nil
}
func (e *fakeEditor) InsertText(_ context.Context, text string) (editorcore.Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pasted = append(e.pasted, text)
	e.r.Editor.Mirror.Text += text
	e.r.Editor.Mirror.Revision++
	return e.r.Editor.Mirror, nil
}
func (e *fakeEditor) InputReceipt() editorcore.InputReceipt {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.receipt
}
func (e *fakeEditor) Resize(context.Context, int, int) error { return nil }
func (e *fakeEditor) WaitAction(ctx context.Context) (editorcore.Action, error) {
	<-ctx.Done()
	return editorcore.Action{}, ctx.Err()
}
func (e *fakeEditor) Resolve(context.Context) error { return nil }
func (e *fakeEditor) Resume(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resumes++
	e.r.Editor.Mirror.Frozen = false
	return nil
}
func (e *fakeEditor) Finish(context.Context) (privateedit.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.finishes++
	if e.finishErr == nil {
		e.r.Editor.Closed = true
		e.r.Result.Closed = true
		e.r.Result.Published = true
		e.r.Result.Disposition = privateedit.Finished
	}
	return e.r.Result, e.finishErr
}
func (e *fakeEditor) Cancel(_ context.Context, consent bool) (privateedit.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cancels++
	if !consent {
		return e.r.Result, privateedit.ErrConfirmationRequired
	}
	if e.cancelErr == nil {
		e.r.Editor.Closed = true
		e.r.Result.Closed = true
		e.r.Result.Disposition = privateedit.Cancelled
		e.r.Result.Recoveries = []privateedit.RecoveryArtifact{{Kind: "frozen", Path: "/synthetic/recovery.age", Durable: true}}
	}
	return e.r.Result, e.cancelErr
}
func (e *fakeEditor) RecoverKnown(context.Context, bool) (privateedit.Result, error) {
	return privateedit.Result{}, errors.New("synthetic unavailable")
}

func fixtureModel() *model {
	m := newModel(context.Background(), context.Background(), nil, nil, &jobOwner{accepting: true}, 80, 24)
	m.editor = fixtureEditor()
	m.phase = "editing"
	return m
}
func physical(r rune) tea.KeyMsg          { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
func apply(m *model, msg tea.Msg) tea.Cmd { _, cmd := m.Update(msg); return cmd }
func runJob(t *testing.T, m *model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected owned operation")
	}
	message := cmd()
	if _, ok := message.(jobMsg); !ok {
		t.Fatal("not an owned operation")
	}
	return apply(m, message)
}

func TestTerminalPasteCannotAuthorizeCancellation(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	runJob(t, m, apply(m, tea.KeyMsg{Type: tea.KeyF3}))
	apply(m, actionMsg{epoch: m.epoch, kind: "cancel"})
	if m.phase != "confirm" {
		t.Fatal("changed cancellation did not require confirmation")
	}
	if cmd := apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y"), Paste: true}); cmd != nil || e.cancels != 0 {
		t.Fatal("paste authorized recovery/close")
	}
	runJob(t, m, apply(m, physical('y')))
	if e.cancels != 1 || !e.Retained().Editor.Closed {
		t.Fatal("physical confirmation did not seal/close")
	}
}

func TestTerminalDeclinedCancelResumesWithoutReplayingQueuedKeys(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	runJob(t, m, apply(m, tea.KeyMsg{Type: tea.KeyF3}))
	m.queue = []Input{{Text: "must-not-leak"}}
	apply(m, actionMsg{epoch: m.epoch, kind: "cancel"})
	cmd := apply(m, physical('n'))
	message := cmd()
	if _, ok := message.(jobMsg); !ok {
		t.Fatal("resume missing")
	}
	apply(m, message)
	if e.resumes != 1 || m.actionKind != "" || m.phase != "editing" || len(m.queue) != 0 || len(e.typed) != 0 {
		t.Fatal("cancel decline lost mode/generation ownership")
	}
	runJob(t, m, apply(m, physical('x')))
	if len(e.typed) != 1 || e.typed[0] != "x" {
		t.Fatal("resumed typing was not literal")
	}
}

func TestTerminalSmallScreenAndHiddenActionsDoNotSubmit(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	apply(m, tea.WindowSizeMsg{Width: 20, Height: 6})
	for _, msg := range []tea.Msg{physical('y'), tea.KeyMsg{Type: tea.KeyF2}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y"), Paste: true}} {
		apply(m, msg)
	}
	if e.finishes != 0 || e.cancels != 0 || len(e.typed) != 0 || strings.Contains(m.View(), testPrivate) {
		t.Fatal("small screen exposed/mutated/authorized hidden content")
	}
	m.phase = "confirm"
	if cmd := apply(m, physical('y')); cmd != nil {
		t.Fatal("hidden confirmation submitted")
	}
}

func TestTerminalResizeDoesNotRetryFailedPublication(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	e.finishErr = scratchpad.ErrConflict
	runJob(t, m, apply(m, tea.KeyMsg{Type: tea.KeyF2}))
	runJob(t, m, apply(m, actionMsg{epoch: m.epoch, kind: "finish"}))
	if cmd := apply(m, tea.WindowSizeMsg{Width: 100, Height: 30}); cmd != nil || e.finishes != 1 {
		t.Fatal("resize blindly retried conflicting save")
	}
	if !strings.Contains(m.status, "Conflict") || e.Retained().Editor.Closed {
		t.Fatal("failed publication lost owner or status")
	}
}

func TestTerminalGracefulSignalWaitsForCurrentOperation(t *testing.T) {
	m := fixtureModel()
	m.busy = true
	if cmd := apply(m, stopMsg{"signal"}); cmd == nil {
		t.Fatal("signal watcher was not rearmed")
	}
	if !m.stopping || !m.relock || m.editor.(*fakeEditor).cancels != 0 {
		t.Fatal("signal bypassed active operation")
	}
	m.busy = false
	cmd := m.next()
	runJob(t, m, cmd)
	if len(m.editor.(*fakeEditor).keys) != 1 || m.editor.(*fakeEditor).keys[0] != "Cancel" {
		t.Fatal("signal did not request ordered freeze")
	}
}

func TestOwnedJobsCannotStartAfterProgramExit(t *testing.T) {
	j := &jobOwner{accepting: true}
	called := false
	cmd := j.command(func() jobMsg { called = true; return jobMsg{} })
	j.stopAndWait()
	if cmd() != nil || called {
		t.Fatal("late Tea command started a detached operation")
	}
}

func TestTerminalRefusesRedirectedDescriptors(t *testing.T) {
	if CheckTerminal(nil, nil) != ErrTerminal {
		t.Fatal("missing terminal accepted")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if CheckTerminal(r, w) != ErrTerminal {
		t.Fatal("redirected terminal accepted")
	}
}

func TestTerminalErrorAndViewsDoNotReflectBackendCause(t *testing.T) {
	m := fixtureModel()
	m.phase = "error"
	m.status = errorStatus(errors.New(testPrivate + "/synthetic/private-path"))
	if strings.Contains(m.status, testPrivate) || strings.Contains(safeError(errors.New(testPrivate)).Error(), testPrivate) {
		t.Fatal("terminal diagnostics reflected private input")
	}
	m.width = 39
	m.height = 6
	if strings.Contains(m.View(), testPrivate) {
		t.Fatal("small-screen view contained private grid")
	}
}
