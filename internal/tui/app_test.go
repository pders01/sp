package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestAppTemplateChooserAppliesSelectedSections(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	date := app.cal.CursorDate()
	app.SetTemplates(
		[]DayTemplate{{ID: "timebox", Name: "Workday timebox"}},
		nil,
		func(_ context.Context, gotDate string, selections []TemplateSelection) (TemplateApplyResult, error) {
			if gotDate != date || len(selections) != 1 || selections[0].ID != "timebox" || selections[0].Force {
				t.Fatalf("apply(%q, %v)", gotDate, selections)
			}
			return TemplateApplyResult{Content: "## Workday timebox\n", Applied: []string{"timebox"}}, nil
		},
	)

	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if app.templateChooser == nil || !strings.Contains(app.View(), "Workday timebox") {
		t.Fatalf("template chooser did not open: %q", app.View())
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("applying a template returned no command")
	}
	app.Update(cmd())

	if app.templateChooser != nil {
		t.Error("template chooser remained open after apply")
	}
	if got := app.cal.contents[date]; got != "## Workday timebox\n" {
		t.Errorf("calendar content = %q", got)
	}
	if !app.templateApplied(date, "timebox") {
		t.Error("template was not marked applied")
	}
}

func TestAppTemplateChooserQQuitsAndEscCancels(t *testing.T) {
	newApp := func() *App {
		app := newTestApp(ModeCalendar)
		app.SetTemplates(
			[]DayTemplate{{ID: "timebox", Name: "Workday timebox"}},
			nil,
			func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
				return TemplateApplyResult{}, nil
			},
		)
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		return app
	}

	cancelApp := newApp()
	defer cancelApp.Close()
	cancelApp.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cancelApp.templateChooser != nil || cancelApp.IsQuitting() {
		t.Error("Esc should cancel the chooser without quitting")
	}

	quitApp := newApp()
	defer quitApp.Close()
	_, cmd := quitApp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !quitApp.IsQuitting() || cmd == nil {
		t.Error("q should quit from the template chooser")
	}
}

func TestAppTemplateChooserCancelsApplyOnQuit(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	canceled := make(chan struct{})
	app.SetTemplates(
		[]DayTemplate{{ID: "slow", Name: "Slow"}},
		nil,
		func(ctx context.Context, _ string, _ []TemplateSelection) (TemplateApplyResult, error) {
			<-ctx.Done()
			close(canceled)
			return TemplateApplyResult{}, ctx.Err()
		},
	)
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	_, applyCmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	go applyCmd()

	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("q did not cancel the in-flight template application")
	}
	app.Update(templateAppliedMsg{
		date: app.cal.CursorDate(),
		result: TemplateApplyResult{
			Content: "must not be applied after quit",
			Applied: []string{"slow"},
		},
	})
	if strings.Contains(app.cal.contents[app.cal.CursorDate()], "must not") {
		t.Error("late template result was applied after quit")
	}
}

func TestAppTemplateChooserScrollsLongLists(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	options := make([]DayTemplate, 10)
	for i := range options {
		options[i] = DayTemplate{ID: fmt.Sprintf("template-%d", i), Name: fmt.Sprintf("Template %d", i)}
	}
	app.SetTemplates(options, nil, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
		return TemplateApplyResult{}, nil
	})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for range 9 {
		app.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	out := app.View()
	if !strings.Contains(out, "Template 9") || strings.Contains(out, "Template 0") {
		t.Errorf("chooser did not window around cursor: %q", out)
	}
}

func TestAppTemplateChooserCanForceReapply(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	date := app.cal.CursorDate()
	app.SetTemplates(
		[]DayTemplate{{ID: "timebox", Name: "Workday timebox"}},
		map[string][]string{date: {"timebox"}},
		func(_ context.Context, _ string, selections []TemplateSelection) (TemplateApplyResult, error) {
			if len(selections) != 1 || !selections[0].Force {
				t.Fatalf("selections = %+v, want forced reapply", selections)
			}
			return TemplateApplyResult{Content: "## Workday timebox\n", Applied: []string{"timebox"}}, nil
		},
	)

	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !strings.Contains(app.View(), "reapply") {
		t.Fatalf("chooser does not indicate forced reapply: %q", app.View())
	}
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("forced reapply returned no command")
	}
	app.Update(cmd())
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
