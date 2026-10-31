package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPrivateEditorUXOpensAtStartAndKeepsUndoLossless(t *testing.T) {
	const original = "\talpha\r\n笔记 🙂\nlast\n\n"
	e := newPrivateEditor("2026-01-01", original, nil, nil)
	if e.input.pos != 0 || e.input.Value() != original {
		t.Fatal("opening moved bytes or started at the end")
	}
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	e.Update(privacyKey("extra"))
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if e.input.Value() != original {
		t.Fatal("undo normalized the original")
	}
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if e.input.Value() != original+"extra" {
		t.Fatal("redo changed literal bytes")
	}
}

func TestPrivateEditorUXFindOwnsSaveAndCancelAndHasContextualHelp(t *testing.T) {
	saves := 0
	e := newPrivateEditor("2026-01-01", "alpha beta alpha", func(string, string, string) error { saves++; return nil }, nil)
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	e.Update(privacyKey("alpha"))
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if saves != 0 || e.done || !e.input.Finding() {
		t.Fatal("find forwarded save")
	}
	e.Update(tea.KeyMsg{Type: tea.KeyF1})
	if e.keyHelp == nil || !strings.Contains(e.keyHelp.content, "this draft") {
		t.Fatal("find lacked contextual help")
	}
	e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !e.input.Finding() {
		t.Fatal("help closed the find layer")
	}
	e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if e.done || e.confirm || e.input.Finding() || e.input.Value() != "alpha beta alpha" {
		t.Fatal("find cancellation discarded or edited draft")
	}
}

func TestPrivateEditorUXOrientationIsBudgeted(t *testing.T) {
	e := newPrivateEditor("2026-01-01", strings.Repeat("long prose ", 40), nil, nil)
	for _, size := range layoutSizes {
		e.Update(size)
		assertFrameFits(t, e.View(), size)
	}
	e.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if !strings.Contains(e.View(), "Ln 1/1 Col 1") || !strings.Contains(e.input.Status(), "right hidden") {
		t.Fatal("line/column or offscreen orientation missing")
	}
}
