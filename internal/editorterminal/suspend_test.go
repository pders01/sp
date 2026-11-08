package editorterminal

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTerminalCtrlZCancelsAndRelocksInsteadOfSuspending(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	runJob(t, m, apply(m, tea.KeyMsg{Type: tea.KeyCtrlZ}))
	if len(e.keys) != 1 || e.keys[0] != "Cancel" || !m.stopping || !m.relock || e.finishes != 0 {
		t.Fatal("suspend key bypassed ordered encrypted cancellation")
	}
	runJob(t, m, apply(m, actionMsg{epoch: m.epoch, kind: "cancel"}))
	if !e.r.Editor.Closed || e.finishes != 0 || len(e.r.Result.Recoveries) != 1 {
		t.Fatal("suspend key did not preserve draft before close")
	}
}

func TestTerminalCancelledStartupNeverTouchesTerminalOrOpensEditor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	report, err := runWithFactory(ctx, Options{}, func(*model, int, int) (editor, error) { called = true; return nil, nil })
	if called || !errors.Is(err, context.Canceled) || !report.Outcome.Closed || report.PendingInput || report.Editor != nil {
		t.Fatal("cancelled startup touched terminal or retained a nonexistent owner")
	}
}
