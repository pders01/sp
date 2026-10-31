package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestMonthSparklineRepresentsEveryDayAtEverySupportedWidth(t *testing.T) {
	for _, month := range []struct {
		year  int
		month time.Month
	}{
		{2026, time.February}, {2024, time.February}, {2026, time.April}, {2026, time.January},
	} {
		first := time.Date(month.year, month.month, 1, 0, 0, 0, 0, time.UTC)
		last := first.AddDate(0, 1, -1)
		days := last.Day()
		for _, width := range []int{4, 7, 10, 16, 31, 40} {
			t.Run(fmt.Sprintf("%s/width=%d", first.Format("2006-01"), width), func(t *testing.T) {
				cal := NewCalendar(nil)
				defer cal.Close()
				for day := 1; day <= days; day++ {
					date := first.AddDate(0, 0, day-1).Format(time.DateOnly)
					cal.hasData = map[string]bool{date: true}
					spark := cal.monthSparkline(first, last, width)
					cells := []rune(ansi.Strip(spark))
					if len(cells) != min(width, days) || lipgloss.Width(spark) > width {
						t.Fatalf("day %d: timeline width=%d, cells=%d", day, lipgloss.Width(spark), len(cells))
					}
					if strings.Count(string(cells), "■") != 1 {
						t.Fatalf("day %d is missing or duplicated in %q", day, string(cells))
					}
					// Each cell represents a contiguous proportional date range.
					bucket := (day*len(cells) - 1) / days
					if cells[bucket] != '■' {
						t.Fatalf("day %d appears in the wrong range: %q", day, string(cells))
					}
				}
			})
		}
	}
}

func TestMonthSparklineRetainsPerDayDetailWhenItFits(t *testing.T) {
	cal := NewCalendar([]string{"2026-01-01", "2026-01-15", "2026-01-31"})
	defer cal.Close()
	first := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	cells := []rune(ansi.Strip(cal.monthSparkline(first, last, 40)))
	if len(cells) != 31 {
		t.Fatalf("roomy timeline contains %d cells, want 31", len(cells))
	}
	for i, cell := range cells {
		want := '·'
		if i == 0 || i == 14 || i == 30 {
			want = '■'
		}
		if cell != want {
			t.Errorf("day %d: %q, want %q", i+1, cell, want)
		}
	}
}

func TestMonthSparklineIgnoresOtherMonthsAndFitsNarrowBounds(t *testing.T) {
	cal := NewCalendar([]string{"2025-12-31", "2026-02-01"})
	defer cal.Close()
	first := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	if spark := ansi.Strip(cal.monthSparkline(first, last, 16)); spark != strings.Repeat("·", 16) {
		t.Fatalf("outside-month entries affected timeline: %q", spark)
	}
	for _, width := range []int{-1, 0, 1, 2, 3} {
		if spark := cal.monthSparkline(first, last, width); spark != "" {
			t.Errorf("timeline should be omitted at width %d, got %q", width, spark)
		}
	}
}

func TestMonthSparklineOmitsInvalidDateRanges(t *testing.T) {
	cal := NewCalendar(nil)
	defer cal.Close()
	first := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
	for _, last := range []time.Time{first.AddDate(0, 0, -1), first.AddDate(0, 1, 0), first.AddDate(1, 0, 0)} {
		if spark := cal.monthSparkline(first, last, 16); spark != "" {
			t.Errorf("invalid month range through %s produced %q", last.Format(time.DateOnly), spark)
		}
	}
}

func TestYearTileShowsActivityOnTheLastDayOfTheMonth(t *testing.T) {
	cal := NewCalendar([]string{"2026-01-31"})
	defer cal.Close()
	cal.SetCursor("2026-01-31")
	// At 80 columns, the four-column layout allocates 20 columns per month.
	view := ansi.Strip(cal.renderMonthTile(2026, time.January, 20, 6))
	if !strings.Contains(view, "1 entry") || !strings.Contains(view, "■") {
		t.Fatalf("late-month entry was omitted from the year tile: %q", view)
	}
}

func TestYearViewExplainsWholeMonthDateRanges(t *testing.T) {
	cal := NewCalendar(nil)
	defer cal.Close()
	cal.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for month := time.January; month <= time.December; month++ {
		cal.SetCursor(fmt.Sprintf("2026-%02d-15", month))
		cal.Update(privacyKey("y"))
		focus := cal.focusLine()
		if !strings.Contains(focus, month.String()+" 2026") || !strings.Contains(focus, "full month") || !strings.Contains(focus, "active range") {
			t.Fatalf("missing month timeline legend: %q", focus)
		}
		if lipgloss.Width(focus) > 80 || !strings.Contains(ansi.Strip(cal.View()), "full month") {
			t.Fatal("whole-month explanation is hidden at a standard terminal width")
		}
	}
}
