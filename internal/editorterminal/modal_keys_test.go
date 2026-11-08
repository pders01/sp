package editorterminal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModalProfileForwardsEditorControlsWithoutShutdown(t *testing.T) {
	for _, tc := range []struct {
		key  tea.KeyType
		want string
	}{
		{tea.KeyCtrlA, "CtrlA"}, {tea.KeyCtrlC, "CtrlC"}, {tea.KeyCtrlD, "CtrlD"},
		{tea.KeyCtrlL, "CtrlL"}, {tea.KeyCtrlR, "CtrlR"}, {tea.KeyCtrlU, "CtrlU"},
		{tea.KeyEsc, "Escape"}, {tea.KeyEnter, "Enter"}, {tea.KeyTab, "Tab"}, {tea.KeyInsert, "Insert"},
		{tea.KeyF2, "Finish"}, {tea.KeyF3, "Cancel"}, {tea.KeyCtrlS, "Save"},
	} {
		m := fixtureModel()
		m.forwardEditorControls, m.busy = true, true // Inspect queued ownership, not a native transport.
		apply(m, tea.KeyMsg{Type: tc.key})
		if m.stopping || m.relock || len(m.queue) != 1 || m.queue[0].Key != tc.want {
			t.Fatal("modal control routing changed shutdown or lost the owned event")
		}
	}
}

func TestModalProfileKeepsExplicitParentExitKeys(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlQ, tea.KeyCtrlZ} {
		m := fixtureModel()
		m.forwardEditorControls, m.busy = true, true
		apply(m, tea.KeyMsg{Type: key})
		if !m.stopping || !m.relock || len(m.queue) != 0 {
			t.Fatal("parent shutdown key leaked into the engine")
		}
	}
}

func TestDefaultControlRoutingRemainsUnchanged(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyCtrlD, tea.KeyCtrlL, tea.KeyCtrlQ, tea.KeyCtrlZ} {
		m := fixtureModel()
		m.busy = true
		apply(m, tea.KeyMsg{Type: key})
		if !m.stopping || !m.relock {
			t.Fatal("default control routing was changed by modal opt-in")
		}
	}
	m := fixtureModel()
	m.busy = true
	apply(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if len(m.queue) != 1 || m.queue[0].Key != "Redo" {
		t.Fatal("default redo key changed")
	}
}

func TestDefaultProfileDoesNotForwardInsertToggle(t *testing.T) {
	m := fixtureModel()
	m.busy = true
	apply(m, tea.KeyMsg{Type: tea.KeyInsert})
	if len(m.queue) != 0 || m.stopping || m.status == "" {
		t.Fatal("modal Insert toggle leaked into default profile")
	}
}

func TestTypedCommandFreezeStopsQueuedInputBeforeActionBroadcast(t *testing.T) {
	m := fixtureModel()
	m.forwardEditorControls = true
	e := m.editor.(*fakeEditor)
	cmd := apply(m, tea.KeyMsg{Type: tea.KeyEnter})
	apply(m, physical('x')) // Arrives while the exact Enter operation is owned.
	e.mu.Lock()
	e.r.Editor.Mirror.Frozen = true // Synchronous guest completed :wq on Enter.
	e.mu.Unlock()
	if next := runJob(t, m, cmd); next != nil || !m.waitingFreeze || len(m.queue) != 1 {
		t.Fatal("typed command freeze dispatched trailing keys before its action")
	}
	finish := apply(m, actionMsg{epoch: m.epoch, kind: "finish"})
	if len(m.queue) != 0 {
		t.Fatal("trailing command input was not discarded at action handoff")
	}
	runJob(t, m, finish)
	if e.finishes != 1 || !e.Retained().Editor.Closed {
		t.Fatal("typed finish action did not reach owned publication")
	}
}

func TestModalProfileDoesNotForwardConfirmationOrPastedConsent(t *testing.T) {
	m := fixtureModel()
	m.forwardEditorControls, m.phase = true, "confirm"
	if cmd := apply(m, tea.KeyMsg{Type: tea.KeyCtrlD}); cmd != nil || len(m.queue) != 0 {
		t.Fatal("confirmation forwarded engine input")
	}
	if cmd := apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}, Paste: true}); cmd != nil || len(m.queue) != 0 {
		t.Fatal("paste authorized a modal confirmation")
	}
	m.width, m.height = 20, 5
	if !strings.Contains(m.View(), "Ctrl+Q/Z") {
		t.Fatal("modal tiny-screen shutdown help is inaccurate")
	}
}
