package editorterminal

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/privateedit"
	"github.com/pders01/sp/internal/scratchpad"
)

func TestInMemoryRequiresExplicitNonNativeFactory(t *testing.T) {
	called := false
	native := forbiddenDriver{called: &called}
	for _, o := range []Options{{}, {Driver: native}, {InMemory: func(context.Context, editorcore.Options, string) (editorcore.UI, error) {
		called = true
		return nil, nil
	}, Driver: native}} {
		if _, err := RunInMemory(context.Background(), o); !errors.Is(err, ErrRetained) || called {
			t.Fatal("in-memory route selected an implicit/native factory")
		}
	}
}
func TestContinuationRejectsWrongProvenanceBeforeTerminal(t *testing.T) {
	m := new(scratchpad.Manager)
	e := new(privateedit.Embedded)
	r := Report{Editor: e, PendingInput: true, pending: []Input{{Text: "synthetic pending"}}, continuation: &continuation{manager: m, document: "2000-01-01", editor: e}}
	factory := func(context.Context, editorcore.Options, string) (editorcore.UI, error) {
		t.Fatal("continuation opened a new UI")
		return nil, nil
	}
	for _, o := range []Options{{Manager: new(scratchpad.Manager), DocumentID: "2000-01-01", InMemory: factory}, {Manager: m, DocumentID: "2000-01-02", InMemory: factory}, {Manager: m, DocumentID: "2000-01-01"}} {
		got, err := ResumeInMemory(context.Background(), o, r)
		if !errors.Is(err, ErrRetained) || got.Editor != e || !got.PendingInput || len(got.RetainedInput()) != 1 {
			t.Fatal("invalid continuation replaced owner/input")
		}
	}
	r.continuation.used = true
	got, err := ResumeInMemory(context.Background(), Options{Manager: m, DocumentID: "2000-01-01", InMemory: factory}, r)
	if !errors.Is(err, ErrRetained) || got.Editor != e {
		t.Fatal("consumed continuation was reused")
	}
}
func TestContinuationRestoresHeldInputWithoutReplay(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	r := Report{PendingInput: true, pending: []Input{{Text: "synthetic uncertain", Partial: true}, {Text: "synthetic tail"}}}
	restoreContinuation(m, r)
	m.finished(jobMsg{kind: "open", owner: e}) // Don't run returned frame/action watchers.
	if m.phase != "error" || !m.inputPaused || m.held == nil || !m.held.Partial || len(m.queue) != 1 || len(e.typed) != 0 {
		t.Fatal("reentry replayed/lost pending events or opened editing")
	}
	apply(m, actionMsg{epoch: m.epoch, kind: "finish"})
	if len(m.queue) != 1 || m.held == nil || e.finishes != 0 {
		t.Fatal("old frozen action discarded pending reentry input")
	}
}
func TestContinuationCannotResolvePendingInputWithLaterReceipt(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	r := Report{PendingInput: true, pending: []Input{{Key: "Escape", Partial: true}, {Text: "synthetic tail"}}}
	restoreContinuation(m, r)
	m.finished(jobMsg{kind: "open", owner: e}) // Watchers are not executed.
	// A failed recovery attempt may have issued Cancel after the original
	// uncertain input. Its full receipt is NOT acknowledgement of Escape.
	e.r.Editor.Mirror.Frozen = true
	e.receipt = editorcore.InputReceipt{RequestID: 9, RequestedBytes: 6, AcceptedBytes: 6, Known: true}
	if cmd := apply(m, physical('r')); cmd != nil {
		runJob(t, m, cmd)
	}
	if m.held == nil || len(m.queue) != 1 || m.phase != "error" || len(e.typed) != 0 {
		t.Fatal("unrelated receipt released or replayed transport-retained input")
	}
}

func TestContinuationQuarantineSurvivesActionsResumeAndResolve(t *testing.T) {
	for _, partial := range []bool{false, true} {
		m := fixtureModel()
		e := m.editor.(*fakeEditor)
		restoreContinuation(m, Report{pending: []Input{{Text: "synthetic held", Partial: partial}, {Text: "synthetic tail"}}})
		m.finished(jobMsg{kind: "open", owner: e})
		apply(m, physical('k'))
		for _, event := range []actionMsg{{epoch: m.epoch, kind: "finish"}, {epoch: m.epoch, err: editorcore.ErrRemote}} {
			if cmd := apply(m, event); cmd != nil || m.phase != "known-confirm" || m.actionKind != "" {
				t.Fatal("late action replaced physical recovery consent")
			}
		}
		apply(m, physical('n'))
		if m.resumed() != nil || m.phase != "error" || m.held == nil || len(m.queue) != 1 {
			t.Fatal("resume released quarantined transport input")
		}
		m.finished(jobMsg{kind: "resolve-input"})
		if m.held == nil || len(m.queue) != 1 || m.phase != "error" || len(e.typed) != 0 {
			t.Fatal("resolve released quarantined transport input")
		}
		// Even if there is no held prefix, a quarantined tail is not editing.
		m.held = nil
		for _, key := range []rune{'r', 'n', 'c'} {
			if cmd := apply(m, physical(key)); cmd != nil || len(m.queue) != 1 {
				t.Fatal("reentry replayed or dropped a quarantined tail")
			}
		}
		if m.next() != nil || len(e.typed) != 0 {
			t.Fatal("quarantined input entered the ordinary queue")
		}
	}
}

func TestContinuationWithoutInputCanExplicitlyResumeEditing(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	e.r.Editor.Mirror.Frozen = true
	restoreContinuation(m, Report{})
	m.finished(jobMsg{kind: "open", owner: e})
	runJob(t, m, apply(m, physical('n')))
	if m.phase != "editing" || m.transportRecovery || e.resumes != 1 {
		t.Fatal("empty-input continuation could not explicitly resume")
	}
	runJob(t, m, apply(m, physical('x')))
	if len(e.typed) != 1 || e.typed[0] != "x" {
		t.Fatal("explicit resume left ordinary new input quarantined")
	}
}

func TestTransportRecoveryNeedsFreshPhysicalConsentAndDurableClose(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	m.transportRecovery = true
	m.phase = "error"
	e.r.Editor.Mirror.Frozen = true
	m.held = &Input{Text: "synthetic uncertain", Partial: true}
	m.queue = []Input{{Text: "synthetic tail"}}
	apply(m, physical('k'))
	if m.phase != "known-confirm" || e.cancels != 0 {
		t.Fatal("transport reentry authorized recovery without confirmation")
	}
	if cmd := apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}, Paste: true}); cmd != nil || e.cancels != 0 {
		t.Fatal("paste authorized transport loss")
	}
	e.cancelErr = privateedit.ErrRecoveryReceipt
	runJob(t, m, apply(m, physical('y')))
	if m.held == nil || len(m.queue) != 1 || m.phase != "error" {
		t.Fatal("failed transport recovery released input")
	}
	apply(m, physical('r'))
	if m.phase != "known-confirm" || e.cancels != 1 {
		t.Fatal("transport retry bypassed renewed consent")
	}
	e.cancelErr = nil
	runJob(t, m, apply(m, physical('y')))
	last := m.jobs.lastResult()
	if !e.Retained().Editor.Closed || m.held != nil || len(m.queue) != 0 || !last.outcome.MayHaveLostEdits {
		t.Fatal("transport confirmed durable close did not settle input")
	}
}
func TestAlreadyClosedTransportCannotDropInputBeforePhysicalConsent(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	m.transportRecovery = true
	m.phase = "error"
	m.held = &Input{Text: "synthetic pending"}
	e.r.Editor.Closed = true
	e.r.Result.Closed = true
	e.r.Result.DurabilityUnconfirmed = true
	apply(m, physical('k'))
	runJob(t, m, apply(m, physical('y')))
	if m.held == nil || m.phase != "error" {
		t.Fatal("ambiguous closed recovery dropped pending input")
	}
	e.r.Result.DurabilityUnconfirmed = false
	apply(m, physical('k'))
	runJob(t, m, apply(m, physical('y')))
	if m.held != nil || !m.jobs.lastResult().outcome.MayHaveLostEdits {
		t.Fatal("confirmed already-closed pending loss was not recorded")
	}
}
