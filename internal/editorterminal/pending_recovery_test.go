package editorterminal

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/privateedit"
)

type pendingRecoveryEditor struct {
	*fakeEditor
	attempts int
	fail     bool
}

func (e *pendingRecoveryEditor) RecoverKnown(_ context.Context, consent bool) (privateedit.Result, error) {
	e.attempts++
	if !consent || e.fail {
		return e.r.Result, privateedit.ErrRecoveryReceipt
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.r.Editor.Closed = true
	e.r.Result = privateedit.Result{Disposition: privateedit.RecoveredKnown, Closed: true, MayHaveLostEdits: true,
		Recoveries: []privateedit.RecoveryArtifact{{Kind: "mirror", Durable: true, Path: "/synthetic/recovery.age"}}}
	return e.r.Result, nil
}

func pendingFaultModel() (*model, *pendingRecoveryEditor) {
	m := fixtureModel()
	e := &pendingRecoveryEditor{fakeEditor: fixtureEditor()}
	e.r.Editor.Fault = errors.New("synthetic guest fault")
	e.r.Editor.LatestUnknown = true
	m.editor, m.phase, m.lastOp = e, "error", "input"
	m.held = &Input{Text: "synthetic partial keys", Partial: true}
	m.queue = []Input{{Text: "synthetic unsubmitted tail"}}
	return m, e
}

func TestFaultedPendingInputNeedsFreshPhysicalKnownLossConsent(t *testing.T) {
	m, e := pendingFaultModel()
	if cmd := apply(m, physical('k')); cmd != nil || m.phase != "known-confirm" || e.attempts != 0 {
		t.Fatal("pending input blocked or implicitly started known-copy recovery")
	}
	if cmd := apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}, Paste: true}); cmd != nil || e.attempts != 0 {
		t.Fatal("paste authorized loss of pending input")
	}
	apply(m, physical('n'))
	if m.phase != "error" || m.held == nil || len(m.queue) != 1 {
		t.Fatal("declined loss dropped pending input")
	}
	apply(m, physical('k'))
	runJob(t, m, apply(m, physical('y')))
	if e.attempts != 1 || !e.Retained().Editor.Closed || m.held != nil || len(m.queue) != 0 || len(unsettledInput(m, m.jobs.lastResult())) != 0 {
		t.Fatal("confirmed durable recovery did not settle pending input ownership")
	}
}

func TestFailedPendingRecoveryRetainsInputAndRenewsLossConsent(t *testing.T) {
	m, e := pendingFaultModel()
	e.fail = true
	apply(m, physical('k'))
	runJob(t, m, apply(m, physical('y')))
	if m.phase != "error" || m.held == nil || len(m.queue) != 1 || e.attempts != 1 {
		t.Fatal("failed recovery dropped pending input")
	}
	if cmd := apply(m, physical('r')); cmd != nil || m.phase != "known-confirm" || e.attempts != 1 {
		t.Fatal("retry bypassed renewed physical possible-loss consent")
	}
	apply(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if e.attempts != 1 {
		t.Fatal("resize retried failed recovery")
	}
	e.fail = false
	runJob(t, m, apply(m, physical('y')))
	if e.attempts != 2 || !e.Retained().Editor.Closed || m.held != nil || len(m.queue) != 0 {
		t.Fatal("explicit recovery retry failed to settle owned input")
	}
}
