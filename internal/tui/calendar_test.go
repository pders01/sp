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

	model, _ := cal.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cal = model.(*Calendar)

	if cal.selected != want {
		t.Errorf("selected = %q, want %q", cal.selected, want)
	}
	if cal.quitting {
		t.Error("plain Enter no longer quits the sub-view; the router pops to notebook")
	}
	if cal.IsDirectEdit() {
		t.Error("plain Enter should drill via notebook, not direct edit")
	}
}

func TestCalendarMonthEKeyDirectEdits(t *testing.T) {
	cal := NewCalendar(nil)
	want := cal.cursor.Format("2006-01-02")

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'e'}},
		{Type: tea.KeyRunes, Runes: []rune{'i'}},
	} {
		c := NewCalendar(nil)
		// No editor wired here — fallback path: state set, router quits.
		model, _ := c.Update(key)
		c = model.(*Calendar)
		if c.GetSelectedDate() != want {
			t.Errorf("key %v: selected = %q, want %q", key, c.GetSelectedDate(), want)
		}
		if !c.IsDirectEdit() {
			t.Errorf("key %v: expected directEdit=true", key)
		}
		if !c.quitting {
			t.Errorf("key %v: expected quitting=true", key)
		}
	}
	_ = cal
}

func TestCalendarYearEKeyIgnored(t *testing.T) {
	cal := NewCalendar(nil)
	cal.view = ViewYear

	model, _ := cal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	cal = model.(*Calendar)

	if cal.IsDirectEdit() {
		t.Error("e in year view should not trigger direct edit")
	}
	if cal.quitting {
		t.Error("e in year view should not quit")
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
		// Sub-view sets quitting=true; router decides whether to emit tea.Quit.
		model, _ := cal.Update(key)
		cal = model.(*Calendar)
		if !cal.quitting {
			t.Errorf("key %v: expected quitting=true", key)
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

func TestExtractPreview(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "first line\nsecond", "first line"},
		{"heading", "# Heading\nbody", "Heading"},
		{"multi-hash", "### Sub\nmore", "Sub"},
		{"leading blank", "\n\n  hello\n", "hello"},
		{"only hash", "###\nbody", "body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractPreview(tt.in); got != tt.want {
				t.Errorf("extractPreview(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCalendarSetContentsBuildsPreviews(t *testing.T) {
	cal := NewCalendar([]string{"2024-03-10"})
	cal.SetContents(map[string]string{
		"2024-03-10": "# Daily standup\nNotes...",
		"2024-03-11": "Inline thought",
	})

	if got := cal.previews["2024-03-10"]; got != "Daily standup" {
		t.Errorf("preview for 2024-03-10 = %q, want %q", got, "Daily standup")
	}
	if got := cal.previews["2024-03-11"]; got != "Inline thought" {
		t.Errorf("preview for 2024-03-11 = %q, want %q", got, "Inline thought")
	}
	// SetContents implies hasData when preview non-empty.
	if !cal.hasData["2024-03-11"] {
		t.Error("expected hasData[2024-03-11] = true after SetContents")
	}
}

func TestCalendarFocusLineIncludesPreview(t *testing.T) {
	cal := NewCalendar(nil)
	date := cal.cursor.Format("2006-01-02")
	cal.SetContents(map[string]string{date: "# Coffee with team"})

	out := cal.View()
	if !strings.Contains(out, "Coffee with team") {
		t.Errorf("expected preview in focus line, got: %q", out)
	}
}

func TestCalendarMonthViewShowsAnnotation(t *testing.T) {
	cal := NewCalendar([]string{"2024-03-10"})
	cal.width = 140
	cal.height = 40
	cal.cursor = time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC)
	cal.SetContents(map[string]string{"2024-03-10": "# Sprint kickoff"})

	out := cal.View()
	if !strings.Contains(out, "Sprint kickoff") {
		t.Errorf("expected day annotation in month view, got: %q", out)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		w    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hell…"},
		{"hi", 1, "…"},
		{"hi", 0, "hi"},
	}
	for _, tt := range tests {
		if got := truncate(tt.s, tt.w); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.w, got, tt.want)
		}
	}
}

func TestCalendarInitArmsThemeWatcher(t *testing.T) {
	cal := NewCalendar(nil)
	defer cal.Close()
	if cmd := cal.Init(); cmd == nil {
		t.Error("Init() returned nil; expected theme-wait cmd")
	}
}

func TestCalendarAccessors(t *testing.T) {
	cal := NewCalendar(nil)

	if got := cal.GetSelectedDate(); got != "" {
		t.Errorf("GetSelectedDate() before selection = %q, want empty", got)
	}
	if cal.IsQuitting() {
		t.Error("IsQuitting() before quit should be false")
	}

	cal.selected = "2024-04-04"
	cal.quitting = true
	if got := cal.GetSelectedDate(); got != "2024-04-04" {
		t.Errorf("GetSelectedDate() = %q, want %q", got, "2024-04-04")
	}
	if !cal.IsQuitting() {
		t.Error("IsQuitting() after quit should be true")
	}
}

func TestCalendarSetIcons(t *testing.T) {
	cal := NewCalendar(nil)
	cal.SetIcons(nerdIcons)
	if cal.icons != nerdIcons {
		t.Errorf("SetIcons did not apply: icons = %+v", cal.icons)
	}
}

func TestCalendarUpdateIgnoresUnknownMsg(t *testing.T) {
	cal := NewCalendar(nil)
	before := cal.cursor

	model, cmd := cal.Update("not a key")
	cal = model.(*Calendar)

	if cmd != nil {
		t.Errorf("unknown msg returned cmd %v, want nil", cmd)
	}
	if !cal.cursor.Equal(before) {
		t.Errorf("unknown msg moved cursor from %v to %v", before, cal.cursor)
	}
}

func TestCalendarWindowSizeMsgUpdatesDims(t *testing.T) {
	cal := NewCalendar(nil)
	model, _ := cal.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	cal = model.(*Calendar)
	if cal.width != 200 || cal.height != 50 {
		t.Errorf("dims = %dx%d, want 200x50", cal.width, cal.height)
	}
}

func TestCalendarMonthKeysFullCoverage(t *testing.T) {
	cases := []struct {
		key    tea.KeyMsg
		deltaD int
		deltaM int
		deltaY int
	}{
		{tea.KeyMsg{Type: tea.KeyLeft}, -1, 0, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}, -1, 0, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}, 1, 0, 0},
		{tea.KeyMsg{Type: tea.KeyUp}, -7, 0, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}, -7, 0, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, 7, 0, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}}, 0, -1, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}}, 0, 1, 0},
	}
	for _, tc := range cases {
		cal := NewCalendar(nil)
		start := cal.cursor
		want := start.AddDate(tc.deltaY, tc.deltaM, tc.deltaD)
		model, _ := cal.Update(tc.key)
		cal = model.(*Calendar)
		if !cal.cursor.Equal(want) {
			t.Errorf("key %v: cursor = %v, want %v", tc.key, cal.cursor, want)
		}
	}
}

func TestCalendarYearKeysFullCoverage(t *testing.T) {
	cases := []struct {
		key    tea.KeyMsg
		deltaD int
		deltaM int
		deltaY int
	}{
		{tea.KeyMsg{Type: tea.KeyLeft}, 0, -1, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}, 0, -1, 0},
		{tea.KeyMsg{Type: tea.KeyUp}, 0, -3, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}, 0, -3, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, 0, 3, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}}, 0, 0, -1},
	}
	for _, tc := range cases {
		cal := NewCalendar(nil)
		cal.view = ViewYear
		start := cal.cursor
		want := start.AddDate(tc.deltaY, tc.deltaM, tc.deltaD)
		model, _ := cal.Update(tc.key)
		cal = model.(*Calendar)
		if !cal.cursor.Equal(want) {
			t.Errorf("key %v: cursor = %v, want %v", tc.key, cal.cursor, want)
		}
	}
}

func TestClampBoundaries(t *testing.T) {
	cases := []struct{ v, lo, hi, want int }{
		{5, 1, 10, 5},
		{0, 1, 10, 1},
		{15, 1, 10, 10},
		{1, 1, 10, 1},
		{10, 1, 10, 10},
	}
	for _, tc := range cases {
		if got := clamp(tc.v, tc.lo, tc.hi); got != tc.want {
			t.Errorf("clamp(%d, %d, %d) = %d, want %d", tc.v, tc.lo, tc.hi, got, tc.want)
		}
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
