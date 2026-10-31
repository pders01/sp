package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestCalendarPreviewUXOrdinaryFrameShowsDocument(t *testing.T) {
	cal := NewCalendar(nil)
	defer cal.Close()
	cal.SetCursor("2020-08-31") // Six weeks, including the focused last day.
	cal.SetContents(map[string]string{
		"2020-08-31": "# Synthetic heading\n\nUseful preview sentence beyond the cell annotation.",
	})
	cal.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := ansi.Strip(cal.View())
	if !strings.Contains(view, "Useful preview sentence") {
		t.Fatalf("ordinary frame lost the document preview:\n%s", view)
	}
}

func TestCalendarPreviewUXFrameMatrix(t *testing.T) {
	for _, month := range []struct {
		date  string
		weeks int
	}{
		{"2021-02-28", 4},
		{"2024-03-31", 5},
		{"2020-08-31", 6},
	} {
		for _, size := range []tea.WindowSizeMsg{
			{Width: 80, Height: 24}, {Width: 120, Height: 30},
			{Width: 80, Height: 50}, {Width: 42, Height: 24},
		} {
			t.Run(fmt.Sprintf("%s/%dx%d", month.date, size.Width, size.Height), func(t *testing.T) {
				cal := NewCalendar(nil)
				defer cal.Close()
				cal.SetCursor(month.date)
				cal.SetContents(map[string]string{month.date: "Synthetic title\nPUBLIC SECOND LINE"})
				cal.Update(size)
				view := cal.View()
				assertFrameFits(t, view, size)
				plain := ansi.Strip(view)
				readable := strings.Join(strings.Fields(plain), " ")
				for _, want := range []string{"Preview · " + month.date, "PUBLIC SECOND LINE", "Enter: full note", "Focus: " + month.date, "▌"} {
					if !strings.Contains(readable, want) {
						t.Fatalf("missing %q:\n%s", want, plain)
					}
				}
				if cal.monthRows() != month.weeks || cal.CursorDate() != month.date {
					t.Fatal("rendering changed month geometry or focus")
				}
				cal.Update(tea.KeyMsg{Type: tea.KeyEnter})
				if cal.GetSelectedDate() != month.date {
					t.Fatal("Enter no longer opens the focused date")
				}
			})
		}
	}
}

func TestCalendarPreviewUXAllocationAndCompleteWeeks(t *testing.T) {
	for _, date := range []string{"2021-02-28", "2024-03-31", "2020-08-31"} {
		t.Run(date, func(t *testing.T) {
			cal := NewCalendar([]string{date})
			defer cal.Close()
			cal.SetCursor(date)
			cal.SetContents(map[string]string{date: "Synthetic title\nPUBLIC SECOND LINE"})
			weeks := cal.monthRows()
			minimum := weekHeaderRow + weeks*minCellHeight
			for height := minimum; height <= weekHeaderRow+weeks*maxCellHeight+12; height++ {
				layout := cal.renderMonthLayout(80, height)
				assertFrameFits(t, layout, tea.WindowSizeMsg{Width: 80, Height: height})
				lines := strings.Split(ansi.Strip(layout), "\n")
				gridHeight := 0
				for i, line := range lines {
					if line == strings.Repeat("─", 80) {
						gridHeight = i
						break
					}
				}
				if height < minimum+1+minPreviewHeight {
					if gridHeight != 0 || strings.Contains(layout, "Preview ·") {
						t.Fatalf("height %d squeezed preview into minimum grid", height)
					}
					gridHeight = len(lines)
				} else {
					if gridHeight < minimum || height-gridHeight-1 < minPreviewHeight {
						t.Fatalf("height %d allocated grid %d without useful preview", height, gridHeight)
					}
					if !strings.Contains(layout, "PUBLIC SECOND LINE") {
						t.Fatalf("height %d lost useful preview text", height)
					}
				}
				cellHeight := (gridHeight - weekHeaderRow) / weeks
				if cellHeight < minCellHeight || cellHeight > maxCellHeight || gridHeight != weekHeaderRow+weeks*cellHeight {
					t.Fatalf("height %d broke whole-week geometry: grid %d", height, gridHeight)
				}
				first := cal.cursor.AddDate(0, 0, 1-cal.cursor.Day())
				offset := (int(first.Weekday()) + 6) % 7
				start := first.AddDate(0, 0, -offset)
				for week := range weeks {
					labels := strings.Fields(lines[weekHeaderRow+week*cellHeight])
					if len(labels) != 7 {
						t.Fatalf("height %d week %d has %d dates: %q", height, week, len(labels), labels)
					}
					for day, label := range labels {
						want := start.AddDate(0, 0, week*7+day).Format("2")
						if strings.Trim(label, "▌•") != want {
							t.Fatalf("height %d week %d day %d: %q, want %q", height, week, day, label, want)
						}
					}
				}
				if cellHeight == minCellHeight && !strings.Contains(layout, "▌"+cal.cursor.Format("2")+"•") {
					t.Fatal("compact focused cell lost its public presence marker")
				}
			}
		})
	}
}

func TestCalendarPreviewUXMinimumGuidanceAndClipping(t *testing.T) {
	cal := NewCalendar(nil)
	defer cal.Close()
	cal.SetCursor("2020-08-31")
	cal.SetContents(map[string]string{"2020-08-31": strings.Repeat("Synthetic 笔记🙂\tline\n", 100) + "UNSEEN_TAIL"})
	cal.theme.SetStatus(strings.Repeat("synthetic status ", 30), 0)
	for _, size := range []tea.WindowSizeMsg{
		{Width: 0, Height: 0}, {Width: 1, Height: 1},
		{Width: 30, Height: 10}, {Width: 42, Height: 19},
		{Width: 80, Height: 24}, {Width: 120, Height: 30},
	} {
		cal.Update(size)
		assertFrameFits(t, cal.View(), size)
	}
	cal.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	if !strings.Contains(cal.View(), "Resize terminal") {
		t.Fatal("unsupported frame lost resize guidance")
	}
	cal.Update(tea.WindowSizeMsg{Width: 42, Height: 19})
	if strings.Contains(cal.View(), "Resize terminal") || strings.Contains(cal.View(), "Preview ·") {
		t.Fatal("supported minimum must preserve the full grid without a squeezed preview")
	}
	cal.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := ansi.Strip(cal.View())
	if !strings.Contains(view, "… · Enter: full note") || strings.Contains(view, "UNSEEN_TAIL") {
		t.Fatal("clipped excerpt did not distinguish itself from the full note")
	}
	if !strings.HasSuffix(cal.contents["2020-08-31"], "UNSEEN_TAIL") {
		t.Fatal("clipping mutated the stored document")
	}
}

func TestCalendarPreviewUXPrivacyPublicProjectionOnly(t *testing.T) {
	const date = "2020-08-31"
	for _, scenario := range []struct {
		name    string
		shared  string
		private bool
		want    string
	}{
		{"hidden", "", true, "Private note hidden"},
		{"shared", "Public title\nPUBLIC SECOND LINE", false, "PUBLIC SECOND LINE"},
		{"mixed", "Public title\nPUBLIC SECOND LINE", true, "PUBLIC SECOND LINE"},
		{"empty-shared", "", false, "Shared-only entry; no shared content."},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cal := NewCalendar([]string{date})
			cal.SetCursor(date)
			nb := NewNotebook([]string{date})
			app := NewApp(cal, nb, ModeCalendar)
			defer app.Close()
			loads := 0
			cal.loader = func(string) (string, error) {
				t.Fatal("calendar preview called the editor loader")
				return "", nil
			}
			app.SetPrivacy(PrivacyHooks{
				Inspect: func(string) (PublicPage, error) {
					return PublicPage{Shared: scenario.shared, HasPrivate: scenario.private}, nil
				},
				IsUnlocked: func() bool { return true },
				Load: func(string) (PrivatePage, error) {
					loads++
					return PrivatePage{Private: "PRIVATE_CANARY", Shared: scenario.shared}, nil
				},
			})
			// SetPrivacy deliberately clears the editor loader; rendering must
			// also remain read-only if a caller subsequently wires one.
			cal.loader = func(string) (string, error) {
				t.Fatal("preview requested document loading")
				return "", nil
			}
			cal.templatesAvailable, cal.dateJumpAvailable = true, true
			for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 120, Height: 30}, {Width: 42, Height: 26}} {
				app.Update(size)
				view := ansi.Strip(app.View())
				assertFrameFits(t, view, size)
				readable := strings.Join(strings.Fields(view), " ")
				if loads != 0 || strings.Contains(view, "PRIVATE_CANARY") || !strings.Contains(readable, scenario.want) {
					t.Fatalf("calendar failed public-only preview: loads=%d\n%s", loads, view)
				}
				if !strings.Contains(view, "Preview ·") || !strings.Contains(view, "Enter: full note") {
					t.Fatal("privacy controls crowded out the preview")
				}
				if scenario.shared != "" && !strings.Contains(view, "· shared") {
					t.Fatal("shared preview was not labeled")
				}
			}
			if cal.contents[date] != scenario.shared {
				t.Fatal("rendering changed the public projection")
			}
		})
	}
}

func TestCalendarPreviewUXEmptyAndUnavailable(t *testing.T) {
	cal := NewCalendar(nil)
	defer cal.Close()
	cal.SetCursor("2020-08-31")
	cal.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(cal.View(), "No entry for this day.") {
		t.Fatal("empty date has no useful preview state")
	}
	cal.MarkDate("2020-08-31", "STALE_PUBLIC_CONTENT")
	cal.loadErrors["2020-08-31"] = "synthetic load failure"
	view := ansi.Strip(cal.View())
	if !strings.Contains(view, "Entry unavailable") || strings.Contains(view, "STALE_PUBLIC_CONTENT") {
		t.Fatal("unavailable entry rendered stale content")
	}
}
