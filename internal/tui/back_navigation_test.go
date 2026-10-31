package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestYearEscapeReturnsToMonthWithoutQuittingOrChangingFocus(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	app.cal.SetCursor("2026-01-31")
	app.cal.preferredDay = 31
	app.Update(privacyKey("y"))
	app.Update(privacyKey("?"))
	if !strings.Contains(app.cal.keyHelp.content, "return to month") {
		t.Fatal("year help does not identify Escape's destination")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.cal.view != ViewYear {
		t.Fatal("closing help changed the underlying calendar view")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.IsQuitting() || app.cal.view != ViewMonth || app.cal.CursorDate() != "2026-01-31" || app.cal.preferredDay != 31 {
		t.Fatal("year Escape quit or changed focus/preference")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !app.IsQuitting() {
		t.Fatal("month Escape did not quit the root view")
	}
}

func TestQuitKeysStillQuitYearDirectly(t *testing.T) {
	for _, key := range []tea.KeyMsg{privacyKey("q"), {Type: tea.KeyCtrlC}} {
		app := newTestApp(ModeCalendar)
		app.Update(privacyKey("y"))
		app.Update(key)
		if !app.IsQuitting() {
			t.Fatal("hard quit was interpreted as back-navigation")
		}
		app.Close()
	}
}

func TestNotebookBackHelpMatchesStandaloneAndNestedDestination(t *testing.T) {
	for _, mode := range []AppMode{ModeNotebook, ModeCalendar} {
		app := newTestApp(mode)
		want := "no parent view"
		if mode == ModeCalendar {
			app.Update(tea.KeyMsg{Type: tea.KeyEnter})
			want = "return to calendar"
		}
		app.Update(privacyKey("?"))
		if !strings.Contains(app.nb.keyHelp.content, want) {
			t.Fatal("notebook back help describes the wrong parent")
		}
		app.Update(tea.KeyMsg{Type: tea.KeyEsc})
		app.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if app.IsQuitting() != (mode == ModeNotebook) || mode == ModeCalendar && app.Mode() != ModeCalendar {
			t.Fatal("notebook Escape used the wrong destination")
		}
		app.Close()
	}
}
