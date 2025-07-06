package tui

import (
	"testing"
	"time"
)

func TestNewCalendarIncludesToday(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	cal := NewCalendar([]string{})
	found := false
	for i := 0; i < len(cal.list.Items()); i++ {
		item, ok := cal.list.Items()[i].(CalendarItem)
		if ok && item.date == today {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("today's date %q not found in calendar items", today)
	}
}

func TestNewCalendarWithDates(t *testing.T) {
	dates := []string{"2024-01-01", "2024-01-02"}
	cal := NewCalendar(dates)
	if len(cal.list.Items()) < len(dates) {
		t.Errorf("expected at least %d items, got %d", len(dates), len(cal.list.Items()))
	}
}

func TestCalendarSelectDate(t *testing.T) {
	dates := []string{"2024-01-01", "2024-01-02"}
	cal := NewCalendar(dates)
	// Simulate selecting the first item
	if item, ok := cal.list.Items()[0].(CalendarItem); ok {
		cal.selected = item.date
		if cal.GetSelectedDate() != item.date {
			t.Errorf("expected selected date %q, got %q", item.date, cal.GetSelectedDate())
		}
	} else {
		t.Error("first item is not a CalendarItem")
	}
}
