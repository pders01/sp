package editorterminal

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestControlSCheckpointIsOptInAndShutdownCannotSaveQueuedInput(t *testing.T) {
	for _, modal := range []bool{false, true} {
		m := fixtureModel()
		m.forwardEditorControls = modal
		e := m.editor.(*fakeEditor)
		runJob(t, m, apply(m, tea.KeyMsg{Type: tea.KeyCtrlS}))
		want := "Finish"
		if modal {
			want = "Save"
		}
		if len(e.keys) != 1 || e.keys[0] != want {
			t.Fatal("CtrlS changed the wrong key profile")
		}
	}
	m := fixtureModel()
	m.forwardEditorControls, m.stopping = true, true
	m.queue = []Input{{Key: "Save"}}
	e := m.editor.(*fakeEditor)
	runJob(t, m, m.next())
	if len(e.keys) != 1 || e.keys[0] != "Cancel" {
		t.Fatal("shutdown executed a queued checkpoint")
	}
}
