package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestWrappedHelpKeepsShortBindingsTogether(t *testing.T) {
	got := wrappedHelp([]helpEntry{
		{keys: "Enter", label: "edit", visible: true},
		{keys: "F1", label: "help", visible: true},
		{keys: "q", label: "quit", visible: true},
		{keys: "hidden", label: "unavailable", visible: false},
	}, 24)
	for _, binding := range []string{"Enter: edit", "F1: help", "q: quit"} {
		if !strings.Contains(got, binding) {
			t.Fatalf("short binding was hidden or split: %q", got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if lipgloss.Width(line) > 24 || strings.Contains(line, "hidden") {
			t.Fatalf("wrapped help exceeds its budget or context: %q", got)
		}
	}
}

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
