package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCalendarPeriodJumpsClampWithoutLosingPreferredDay(t *testing.T) {
	for _, tc := range []struct {
		name, start string
		keys        []string
		want        []string
	}{
		{"forward short month", "2026-01-31", []string{"L", "L"}, []string{"2026-02-28", "2026-03-31"}},
		{"backward short month", "2026-03-31", []string{"H", "H"}, []string{"2026-02-28", "2026-01-31"}},
		{"leap February", "2024-01-31", []string{"L", "L"}, []string{"2024-02-29", "2024-03-31"}},
		{"month round trip", "2026-01-31", []string{"L", "H"}, []string{"2026-02-28", "2026-01-31"}},
		{"year boundary", "2026-12-31", []string{"L", "H"}, []string{"2027-01-31", "2026-12-31"}},
		{"thirty is not end-of-month mode", "2026-04-30", []string{"L"}, []string{"2026-05-30"}},
		{"leap day across years", "2024-02-29", []string{"y", "L", "L", "L", "L"}, []string{"2024-02-29", "2025-02-28", "2026-02-28", "2027-02-28", "2028-02-29"}},
		{"year jump round trip", "2024-02-29", []string{"y", "L", "H"}, []string{"2024-02-29", "2025-02-28", "2024-02-29"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cal := NewCalendar(nil)
			defer cal.Close()
			cal.SetCursor(tc.start)
			for i, key := range tc.keys {
				cal.Update(privacyKey(key))
				if got := cal.cursor.Format(time.DateOnly); got != tc.want[i] {
					t.Fatalf("key %q: got %s, want %s", key, got, tc.want[i])
				}
			}
		})
	}
}

func TestCalendarDayAndWeekMovesResetPreferredDay(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyType
		want string
	}{
		{"left", tea.KeyLeft, "2026-03-27"},
		{"right", tea.KeyRight, "2026-04-01"},
		{"up", tea.KeyUp, "2026-03-21"},
		{"down", tea.KeyDown, "2026-04-07"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cal := NewCalendar(nil)
			defer cal.Close()
			cal.SetCursor("2026-01-31")
			cal.Update(privacyKey("L"))
			cal.Update(rawPhysical(tc.key))
			cal.Update(privacyKey("L"))
			if got := cal.CursorDate(); got != tc.want {
				t.Fatalf("period jump after %s = %s, want %s", tc.name, got, tc.want)
			}
		})
	}
}

func TestCalendarExplicitDateAndTodayResetPreferredDay(t *testing.T) {
	cal := NewCalendar(nil)
	defer cal.Close()
	cal.SetCursor("2026-01-31")
	cal.Update(privacyKey("L"))
	cal.SetCursor("2026-04-12")
	cal.Update(privacyKey("L"))
	if got := cal.CursorDate(); got != "2026-05-12" {
		t.Fatalf("jump after SetCursor = %s", got)
	}
	cal.SetCursor("2026-01-31")
	cal.Update(privacyKey("L"))
	cal.today = time.Date(2026, time.June, 15, 0, 0, 0, 0, cal.cursor.Location())
	cal.Update(privacyKey("t"))
	cal.Update(privacyKey("L"))
	if got := cal.CursorDate(); got != "2026-07-15" {
		t.Fatalf("jump after today = %s", got)
	}
}

func TestCalendarPeriodJumpsPreserveLocalMidnight(t *testing.T) {
	zone := time.FixedZone("synthetic-offset", 5*60*60+30*60)
	cal := NewCalendar(nil)
	defer cal.Close()
	cal.cursor = time.Date(2026, time.January, 31, 0, 0, 0, 0, zone)
	cal.Update(privacyKey("L"))
	want := time.Date(2026, time.February, 28, 0, 0, 0, 0, zone)
	if !cal.cursor.Equal(want) || cal.cursor.Location() != zone || cal.cursor.Hour() != 0 {
		t.Fatalf("local date changed unexpectedly: %v", cal.cursor)
	}
}

func TestCalendarYearArrowsFollowFourColumnGrid(t *testing.T) {
	for _, tc := range []struct {
		name, start, want string
		keys              []tea.KeyMsg
	}{
		{"January down", "2026-01-15", "2026-05-15", []tea.KeyMsg{rawPhysical(tea.KeyDown), privacyKey("j")}},
		{"February down", "2026-02-15", "2026-06-15", []tea.KeyMsg{rawPhysical(tea.KeyDown), privacyKey("j")}},
		{"May up", "2026-05-15", "2026-01-15", []tea.KeyMsg{rawPhysical(tea.KeyUp), privacyKey("k")}},
		{"last row down wraps year", "2026-12-15", "2027-04-15", []tea.KeyMsg{rawPhysical(tea.KeyDown), privacyKey("j")}},
		{"first row up wraps year", "2026-01-15", "2025-09-15", []tea.KeyMsg{rawPhysical(tea.KeyUp), privacyKey("k")}},
		{"December right", "2026-12-15", "2027-01-15", []tea.KeyMsg{rawPhysical(tea.KeyRight), privacyKey("l")}},
		{"January left", "2026-01-15", "2025-12-15", []tea.KeyMsg{rawPhysical(tea.KeyLeft), privacyKey("h")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range tc.keys {
				cal := NewCalendar(nil)
				cal.SetCursor(tc.start)
				cal.Update(privacyKey("y"))
				cal.Update(key)
				if cal.view != ViewYear || cal.cursor.Format(time.DateOnly) != tc.want {
					t.Errorf("year key %q: got %s, want %s", key.String(), cal.cursor.Format(time.DateOnly), tc.want)
				}
				cal.Update(rawPhysical(tea.KeyEnter))
				if cal.view != ViewMonth || cal.CursorDate() != tc.want || cal.GetSelectedDate() != "" {
					t.Error("opening the selected month changed the date or skipped the month view")
				}
				cal.Close()
			}
		})
	}
}
