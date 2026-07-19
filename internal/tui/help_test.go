package tui

import "testing"

func TestRenderHelpFiltersContextualEntries(t *testing.T) {
	got := renderHelp([]helpEntry{
		{keys: "enter", label: "edit", visible: true},
		{keys: "a", label: "templates", visible: false},
		{keys: "q", label: "quit", visible: true},
	})
	want := "enter: edit • q: quit"
	if got != want {
		t.Errorf("renderHelp() = %q, want %q", got, want)
	}
}
