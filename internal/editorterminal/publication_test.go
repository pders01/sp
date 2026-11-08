package editorterminal

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/privateedit"
	"github.com/pders01/sp/internal/scratchpad"
)

// A model-only durability observation, not a filesystem fault or native save.
// The native privateedit resume tests separately exercise the real saver and
// generation-bound advanced baseline with an injected uncertain observation.
type uncertainModelEditor struct{ *fakeEditor }

func (e uncertainModelEditor) Finish(context.Context) (privateedit.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.finishes++
	if e.finishes == 1 {
		e.r.Baseline = e.r.Editor.Mirror.Text
		e.r.Result.Published = true
		e.r.Result.DurabilityUnconfirmed = true
		return e.r.Result, scratchpad.ErrDurabilityUnconfirmed
	}
	if e.finishErr != nil {
		return e.r.Result, e.finishErr
	}
	e.r.Result.DurabilityUnconfirmed = false
	e.r.Result.Closed = true
	e.r.Result.Disposition = privateedit.Finished
	e.r.Editor.Closed = true
	return e.r.Result, nil
}

func uncertainModel(t *testing.T) (*model, *fakeEditor) {
	t.Helper()
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	m.editor = uncertainModelEditor{e}
	runJob(t, m, apply(m, tea.KeyMsg{Type: tea.KeyF2}))
	runJob(t, m, apply(m, actionMsg{epoch: m.epoch, kind: "finish"}))
	if m.phase != "error" || !e.r.Result.Published || !e.r.Result.DurabilityUnconfirmed || e.r.Editor.Closed || e.r.Baseline != e.r.Editor.Mirror.Text {
		t.Fatal("uncertain publication lost ownership or advanced baseline")
	}
	if strings.Count(m.status, "Published;") != 1 {
		t.Fatal("uncertain publication status duplicated or absent")
	}
	return m, e
}

func TestTerminalUncertainPublicationRequiresExplicitRetry(t *testing.T) {
	m, e := uncertainModel(t)
	for _, event := range []tea.Msg{tea.WindowSizeMsg{Width: 100, Height: 30}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}, Paste: true}} {
		if apply(m, event) != nil || e.finishes != 1 {
			t.Fatal("incidental event retried uncertain publication")
		}
	}
	runJob(t, m, apply(m, physical('r')))
	if e.finishes != 2 || !e.r.Result.Closed || e.r.Result.DurabilityUnconfirmed || !e.r.Result.Published {
		t.Fatal("explicit retry did not confirm publication truth")
	}
}

func TestTerminalUncertainPublicationConflictCanResumeAndRecover(t *testing.T) {
	m, e := uncertainModel(t)
	baseline := e.r.Baseline
	e.finishErr = scratchpad.ErrConflict
	runJob(t, m, apply(m, physical('r')))
	if !strings.Contains(m.status, "Published; durability unconfirmed") || !strings.Contains(m.status, "Conflict:") || strings.Contains(m.status, "not saved") {
		t.Fatal("conflict erased earlier publication facts")
	}
	cmd := apply(m, physical('n'))
	apply(m, cmd()) // Resume returns watcher commands, not owned I/O.
	if m.phase != "editing" || e.r.Baseline != baseline || !e.r.Result.DurabilityUnconfirmed {
		t.Fatal("resume rewound baseline or durability fact")
	}
	runJob(t, m, apply(m, tea.KeyMsg{Type: tea.KeyF3}))
	apply(m, actionMsg{epoch: m.epoch, kind: "cancel"})
	if m.phase != "confirm" {
		t.Fatal("unchanged but uncertain draft skipped recovery consent")
	}
	apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}, Paste: true})
	if e.cancels != 0 {
		t.Fatal("pasted consent authorized recovery")
	}
	runJob(t, m, apply(m, physical('y')))
	if !e.r.Result.Closed || !e.r.Result.Published || !e.r.Result.DurabilityUnconfirmed || len(e.r.Result.Recoveries) != 1 {
		t.Fatal("encrypted cancellation negated earlier uncertain publication")
	}
}

func TestTerminalRecoveryUncertaintyIsNotNoteSaveReceipt(t *testing.T) {
	for _, kind := range []string{"cancel", "known"} {
		status := failureStatus(kind, scratchpad.ErrDurabilityUnconfirmed, privateedit.Result{})
		if !strings.Contains(status, "Encrypted recovery published") || strings.Contains(status, "note publication succeeded") {
			t.Fatal("recovery uncertainty confused with note publication")
		}
	}
	status := failureStatus("cancel", scratchpad.ErrDurabilityUnconfirmed, privateedit.Result{Published: true})
	if !strings.Contains(status, "Earlier note publication succeeded") || !strings.Contains(status, "Encrypted recovery published") {
		t.Fatal("recovery uncertainty erased independent earlier note publication")
	}
}
