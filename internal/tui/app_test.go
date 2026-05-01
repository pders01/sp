package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestApp(mode AppMode) *App {
	cal := NewCalendar([]string{"2024-01-15"})
	nb := NewNotebook([]string{"2024-01-15"})
	return NewApp(cal, nb, mode)
}

func TestAppInitStartsBothViews(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	if cmd := app.Init(); cmd == nil {
		t.Error("Init should return a tea.Batch covering both views")
	}
}

func TestAppRoutesWindowSizeToBothViews(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if app.cal.width != 120 || app.cal.height != 40 {
		t.Errorf("calendar dims not propagated: %dx%d", app.cal.width, app.cal.height)
	}
	if app.nb.width != 120 || app.nb.height != 40 {
		t.Errorf("notebook dims not propagated: %dx%d", app.nb.width, app.nb.height)
	}
}

func TestAppCalendarEnterDrillsToNotebook(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	cursorDate := app.cal.cursor.Format("2006-01-02")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if app.Mode() != ModeNotebook {
		t.Errorf("after Enter, mode = %v, want ModeNotebook", app.Mode())
	}
	if app.nb.GetCurrentPage() != cursorDate {
		t.Errorf("notebook positioned on %q, want %q", app.nb.GetCurrentPage(), cursorDate)
	}
	// Calendar state must be cleared so it's clean when popped back to.
	if app.cal.GetSelectedDate() != "" {
		t.Errorf("calendar selection not cleared: %q", app.cal.GetSelectedDate())
	}
	if app.IsQuitting() {
		t.Error("drilling should not quit the app")
	}
}

func TestAppNotebookEscPopsToCalendar(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Drill in, then pop out.
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if app.Mode() != ModeCalendar {
		t.Errorf("after Esc in notebook, mode = %v, want ModeCalendar", app.Mode())
	}
	if app.IsQuitting() {
		t.Error("Esc should pop, not quit")
	}
}

func TestAppNotebookEscQuitsWhenStandalone(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !app.IsQuitting() {
		t.Error("Esc without a calendar behind should quit")
	}
	if cmd == nil {
		t.Error("expected tea.Quit cmd on standalone Esc")
	}
}

func TestAppCalendarQuitsApp(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if !app.IsQuitting() {
		t.Error("calendar q should quit the app")
	}
	if cmd == nil {
		t.Error("expected tea.Quit cmd")
	}
}

func TestAppNotebookQuitKeyQuitsApp(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if !app.IsQuitting() {
		t.Error("notebook q should quit the app, not pop")
	}
	if cmd == nil {
		t.Error("expected tea.Quit cmd")
	}
}

func TestAppViewSwitchesByMode(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	calOut := app.View()
	if !strings.Contains(calOut, "Calendar ·") {
		t.Errorf("calendar view not rendered: %q", calOut)
	}

	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nbOut := app.View()
	if !strings.Contains(nbOut, "Notebook ·") {
		t.Errorf("notebook view not rendered after drill: %q", nbOut)
	}
}

func TestAppPopSyncsCalendarCursor(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Drill into the notebook, scroll forward via 'l', then pop. The
	// calendar should land on the page the notebook was showing, not
	// on the original drill date.
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Notebook starts on the drilled date; pages descending so 'l'
	// (right) advances to an older page. Push to the next page if
	// available, otherwise just verify cursor sync to current page.
	if len(app.nb.pages) > 1 {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	}
	wantDate, _ := app.nb.CurrentContent()
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if got := app.cal.cursor.Format("2006-01-02"); got != wantDate {
		t.Errorf("calendar cursor after pop = %q, want %q", got, wantDate)
	}
	if app.cal.view != ViewMonth {
		t.Errorf("pop should land on month view, got %v", app.cal.view)
	}
}

func TestAppCalendarDrillSeedsMissingDate(t *testing.T) {
	cal := NewCalendar(nil)
	nb := NewNotebook(nil)
	app := NewApp(cal, nb, ModeCalendar)
	defer app.Close()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	cursorDate := cal.cursor.Format("2006-01-02")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if app.nb.GetCurrentPage() != cursorDate {
		t.Errorf("notebook current = %q, want %q", app.nb.GetCurrentPage(), cursorDate)
	}
}
