package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewCalendarPopulatesHasData(t *testing.T) {
	dates := []string{"2024-01-01", "2024-01-02"}
	cal := NewCalendar(dates)
	for _, d := range dates {
		if !cal.hasData[d] {
			t.Errorf("expected hasData[%q] = true", d)
		}
	}
}

func TestNewCalendarCursorIsToday(t *testing.T) {
	cal := NewCalendar(nil)
	want := time.Now().Format("2006-01-02")
	if got := cal.cursor.Format("2006-01-02"); got != want {
		t.Errorf("cursor = %q, want %q", got, want)
	}
}

func TestCalendarMonthNavigation(t *testing.T) {
	cal := NewCalendar(nil)
	start := cal.cursor

	model, _ := cal.Update(tea.KeyMsg{Type: tea.KeyRight})
	cal = model.(*Calendar)
	if !cal.cursor.Equal(start.AddDate(0, 0, 1)) {
		t.Errorf("right: cursor = %v, want %v", cal.cursor, start.AddDate(0, 0, 1))
	}

	model, _ = cal.Update(tea.KeyMsg{Type: tea.KeyDown})
	cal = model.(*Calendar)
	if !cal.cursor.Equal(start.AddDate(0, 0, 8)) {
		t.Errorf("down: cursor = %v, want %v", cal.cursor, start.AddDate(0, 0, 8))
	}
}

func TestCalendarYearNavigation(t *testing.T) {
	cal := NewCalendar(nil)
	cal.view = ViewYear
	start := cal.cursor

	model, _ := cal.Update(tea.KeyMsg{Type: tea.KeyRight})
	cal = model.(*Calendar)
	if !cal.cursor.Equal(start.AddDate(0, 1, 0)) {
		t.Errorf("right: cursor = %v, want %v", cal.cursor, start.AddDate(0, 1, 0))
	}

	model, _ = cal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	cal = model.(*Calendar)
	if !cal.cursor.Equal(start.AddDate(1, 1, 0)) {
		t.Errorf("L: cursor = %v, want %v", cal.cursor, start.AddDate(1, 1, 0))
	}
}

func TestCalendarToggleViews(t *testing.T) {
	cal := NewCalendar(nil)

	model, _ := cal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	cal = model.(*Calendar)
	if cal.view != ViewYear {
		t.Errorf("expected ViewYear, got %v", cal.view)
	}

	model, _ = cal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	cal = model.(*Calendar)
	if cal.view != ViewMonth {
		t.Errorf("expected ViewMonth, got %v", cal.view)
	}
}

func TestCalendarYearEnterDrillsDown(t *testing.T) {
	cal := NewCalendar(nil)
	cal.view = ViewYear

	model, _ := cal.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cal = model.(*Calendar)

	if cal.view != ViewMonth {
		t.Errorf("expected ViewMonth after enter, got %v", cal.view)
	}
	if cal.quitting {
		t.Error("year-view enter should not quit")
	}
	if cal.selected != "" {
		t.Errorf("year-view enter should not set selected, got %q", cal.selected)
	}
}

func TestCalendarMonthEnterSelectsDay(t *testing.T) {
	cal := NewCalendar(nil)
	want := cal.cursor.Format("2006-01-02")

	model, cmd := cal.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cal = model.(*Calendar)

	if cal.selected != want {
		t.Errorf("selected = %q, want %q", cal.selected, want)
	}
	if !cal.quitting {
		t.Error("month-view enter should quit")
	}
	if cmd == nil {
		t.Error("month-view enter should return tea.Quit cmd")
	}
}

func TestCalendarTodayResetsCursor(t *testing.T) {
	cal := NewCalendar(nil)
	cal.cursor = cal.today.AddDate(0, 6, 0)

	model, _ := cal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	cal = model.(*Calendar)

	if !cal.cursor.Equal(cal.today) {
		t.Errorf("cursor = %v, want %v", cal.cursor, cal.today)
	}
}

func TestCalendarQuitKeys(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyEsc},
	} {
		cal := NewCalendar(nil)
		model, cmd := cal.Update(key)
		cal = model.(*Calendar)
		if !cal.quitting {
			t.Errorf("key %v: expected quitting=true", key)
		}
		if cmd == nil {
			t.Errorf("key %v: expected tea.Quit cmd", key)
		}
	}
}

func TestCalendarViewRendersHeaderAndFooter(t *testing.T) {
	cal := NewCalendar([]string{"2024-01-15"})
	cal.width = 80
	cal.height = 24

	out := cal.View()

	if !strings.Contains(out, "Calendar ·") {
		t.Errorf("expected header in output, got: %q", out)
	}
	if !strings.Contains(out, "Focus:") {
		t.Errorf("expected focus line in output, got: %q", out)
	}
	if !strings.Contains(out, "enter: open") {
		t.Errorf("expected keybindings in output, got: %q", out)
	}
}

func TestCalendarViewQuittingReturnsEmpty(t *testing.T) {
	cal := NewCalendar(nil)
	cal.quitting = true
	if got := cal.View(); got != "" {
		t.Errorf("expected empty view when quitting, got %q", got)
	}
}

func TestCalendarYearViewRenders(t *testing.T) {
	cal := NewCalendar([]string{"2024-03-10", "2024-03-11"})
	cal.width = 100
	cal.height = 30
	cal.view = ViewYear
	cal.cursor = time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC)

	out := cal.View()
	if !strings.Contains(out, "2024") {
		t.Errorf("expected year in output, got: %q", out)
	}
	if !strings.Contains(out, "Mar") {
		t.Errorf("expected month tile labels, got: %q", out)
	}
}
