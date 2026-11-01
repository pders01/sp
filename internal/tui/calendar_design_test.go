package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestCalendarDesignPreviewPrioritizesProseBeforeGrowingCells(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 100, Height: 30}, {Width: 80, Height: 24}} {
		cal := NewCalendar([]string{"2026-10-31"})
		cal.SetCursor("2026-10-31")
		cal.SetContents(map[string]string{"2026-10-31": "# A note title\n\nA useful paragraph, not just its heading.\n\n## More\n\nDetails follow.\n"})
		cal.Update(size)
		if !strings.Contains(ansi.Strip(cal.View()), "A useful paragraph") {
			t.Fatalf("%dx%d excerpt spent its budget on headings/empty cells", size.Width, size.Height)
		}
		cal.Close()
	}
}

func TestCalendarDesignQuietYearTiles(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	for _, theme := range []string{ThemePrefDark, ThemePrefLight} {
		t.Run(theme, func(t *testing.T) {
			cal := NewCalendar([]string{"2026-03-31"})
			defer cal.Close()
			cal.SetThemePref(theme)
			cal.SetCursor("2026-01-31")
			p := cal.theme.Palette()
			for _, tile := range []struct {
				month time.Month
				title string
			}{
				{time.January, p.SelectedDate.Render("▌ January ▐")},
				{time.February, p.MutedText.Render("February")},
				{time.March, p.Header.Render("March")},
			} {
				view := cal.renderMonthTile(2026, tile.month, 20, 6)
				if !strings.Contains(view, tile.title) {
					t.Fatalf("month %d lost semantic theme styling: %q", tile.month, view)
				}
				plain := ansi.Strip(view)
				if tile.month != time.March && (strings.Contains(plain, "no entries") || strings.ContainsAny(plain, "·■")) {
					t.Fatalf("empty month has competing activity chrome: %q", plain)
				}
				if tile.month == time.March && (!strings.Contains(plain, "1 entry") || !strings.Contains(plain, "■")) {
					t.Fatal("quiet styling lost end-of-month activity")
				}
			}
		})
	}
}

func TestCalendarDesignBrandedPreviewAndWarmSelection(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	for _, theme := range []string{ThemePrefDark, ThemePrefLight} {
		t.Run(theme, func(t *testing.T) {
			cal := NewCalendar(nil)
			defer cal.Close()
			cal.SetThemePref(theme)
			cal.SetCursor("2020-08-31")
			cal.SetContents(map[string]string{"2020-08-31": "# Public heading\n\nUseful public sentence."})
			p := cal.theme.Palette()
			preview := cal.renderDocumentPreview(80, 8)
			if !strings.Contains(preview, p.Header.Render("Preview · 2020-08-31 · Monday")) {
				t.Fatal("preview label does not use the semantic teal heading")
			}
			// The shared renderer owns Markdown safety, width and the two-cell
			// inset. Compare its visible excerpt, not standard Glamour styles.
			document := renderDocument(cal.contents["2020-08-31"], cal.theme.Style(), 80)
			for _, line := range strings.Split(document, "\n") {
				if strings.TrimSpace(ansi.Strip(line)) != "" && !strings.Contains(preview, line) {
					t.Fatalf("preview does not use branded Markdown: %q", line)
				}
			}
			teal := regexp.MustCompile(`38;2;\d+;\d+;\d+`).FindString(p.Header.Render("heading"))
			if teal == "" {
				t.Fatal("semantic heading style has no true-color foreground")
			}
			headingFound := false
			for _, line := range strings.Split(preview, "\n") {
				if strings.Contains(ansi.Strip(line), "Public heading") {
					headingFound = strings.Contains(line, teal)
				}
			}
			if !headingFound {
				t.Fatalf("Markdown heading does not use the active semantic teal %q", teal)
			}
			if strings.Contains(preview, "48;") || strings.Contains(preview, "Enter: full note") {
				t.Fatal("ordinary heading excerpt has a colored block or duplicate open action")
			}
			for _, line := range strings.Split(ansi.Strip(preview), "\n") {
				if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "  ") {
					t.Fatalf("preview lost the common body inset: %q", line)
				}
			}
			cell := cal.renderDayCell(cal.cursor, cal.cursor.Month(), 10, 2)
			warm := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true).Underline(true).Render("31")
			if !strings.Contains(cell, warm) || !strings.Contains(ansi.Strip(cell), "▌31•") {
				t.Fatal("selected date lost warm styling or non-color presence marker")
			}
		})
	}
}

func TestCalendarDesignChromeAndResizeMatrix(t *testing.T) {
	for _, theme := range []string{ThemePrefDark, ThemePrefLight} {
		for _, date := range []string{"2021-02-28", "2024-03-31", "2020-08-31"} {
			for _, year := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/year=%v", theme, date, year), func(t *testing.T) {
					cal := NewCalendar([]string{date})
					defer cal.Close()
					cal.SetThemePref(theme)
					cal.SetCursor(date)
					cal.SetContents(map[string]string{date: "Public title\nUseful public sentence."})
					cal.privacyEnabled, cal.templatesAvailable, cal.dateJumpAvailable = true, true, true
					cal.canRelock = true
					cal.privacyState = func() string { return "unlocked" }
					if year {
						cal.view = ViewYear
					}
					for _, size := range []tea.WindowSizeMsg{
						{Width: 42, Height: 19}, {Width: 56, Height: 20},
						{Width: 80, Height: 24}, {Width: 100, Height: 30}, {Width: 120, Height: 50},
					} {
						cal.Update(size)
						view := cal.View()
						assertFrameFits(t, view, size)
						plain := ansi.Strip(view)
						if year && size.Width < 56 {
							if !strings.Contains(plain, "Resize terminal") {
								t.Fatal("four-column year grid has no resize guidance")
							}
							continue
						}
						for _, want := range []string{"sp / Calendar", "m/y: view", "?/F1: help", "q: quit"} {
							if !strings.Contains(plain, want) {
								t.Fatalf("%dx%d lost %q:\n%s", size.Width, size.Height, want, plain)
							}
						}
						if strings.Contains(plain, "Focus:") || strings.Contains(plain, "no entries") {
							t.Fatal("default view repeats focus/empty-entry diagnostics")
						}
						if year {
							for month := time.January; month <= time.December; month++ {
								if !strings.Contains(plain, month.String()) {
									t.Fatalf("year grid lost %s", month)
								}
							}
							if !strings.Contains(plain, "full month: ■ active range · none") {
								t.Fatal("quiet year view lost full-month legend")
							}
						} else if size.Width >= 80 && size.Height >= 24 {
							if !strings.Contains(plain, "Preview · "+date) || !strings.Contains(plain, "Useful public sentence.") {
								t.Fatal("ordinary frame lost a labelled useful preview")
							}
						}
					}
					cal.Update(tea.KeyMsg{Type: tea.KeyF1})
					if cal.keyHelp == nil || !strings.Contains(cal.keyHelp.content, cal.focusLine()) {
						t.Fatal("quieter view lost truthful focus details in F1")
					}
					for _, key := range []string{"m/y", "t/T", "Ctrl+T", "d", "Ctrl+L"} {
						if !strings.Contains(cal.keyHelp.content, key) {
							t.Fatalf("secondary action %q is no longer discoverable through F1", key)
						}
					}
					if !year && (!strings.Contains(cal.keyHelp.content, "e/i") || !strings.Contains(cal.keyHelp.content, "choose template")) {
						t.Fatal("month actions removed from footer are absent from F1")
					}
				})
			}
		}
	}
}

func TestCalendarDesignCompactProjectionLabelsAndFullErrorHelp(t *testing.T) {
	cal := NewCalendar(nil)
	defer cal.Close()
	cal.SetCursor("2026-07-01") // A long weekday must not crowd out “shared”.
	cal.privacyEnabled = true
	cal.privacyState = func() string { return "locked" }
	cal.SetContents(map[string]string{"2026-07-01": "Public excerpt."})
	preview := ansi.Strip(cal.renderDocumentPreview(42, 6))
	if !strings.Contains(preview, "Preview · 2026-07-01 · shared") || strings.Contains(preview, "Wednesday") {
		t.Fatalf("compact preview lost its projection label: %q", preview)
	}
	cal.Update(tea.KeyMsg{Type: tea.KeyF1})
	if !strings.Contains(cal.keyHelp.content, "unlock private") {
		t.Fatal("unlock removed from the footer is no longer available in help")
	}
	cal.Update(tea.KeyMsg{Type: tea.KeyEsc})
	cal.loadErrors["2026-07-01"] = strings.Repeat("synthetic failure ", 30) + "ERROR_END"
	cal.Update(tea.KeyMsg{Type: tea.KeyF1})
	if !strings.Contains(cal.keyHelp.content, "ERROR_END") || strings.Contains(cal.keyHelp.content, "Public excerpt.") {
		t.Fatal("focused error details were clipped or replaced by a stale excerpt")
	}
}

func TestCalendarDesignUnlockedHistoryNeverLoadsPrivatePreviews(t *testing.T) {
	dates := []string{"2020-08-01", "2020-08-31", "2020-09-01"}
	cal := NewCalendar(dates)
	cal.SetCursor("2020-08-31")
	app := NewApp(cal, NewNotebook(dates), ModeCalendar)
	defer app.Close()
	loads := 0
	app.SetPrivacy(PrivacyHooks{
		Inspect: func(string) (PublicPage, error) {
			return PublicPage{HasPrivate: true}, nil
		},
		IsUnlocked: func() bool { return true },
		Load: func(string) (PrivatePage, error) {
			loads++
			return PrivatePage{Private: "PRIVATE_HISTORY_CANARY", HasPrivate: true}, nil
		},
	})
	cal.loader = func(string) (string, error) {
		t.Fatal("calendar rendering/navigation called the editor loader")
		return "", nil
	}
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyLeft}, {Type: tea.KeyRight}, privacyKey("H"), privacyKey("L"),
		privacyKey("y"), {Type: tea.KeyDown}, {Type: tea.KeyUp}, privacyKey("m"),
		{Type: tea.KeyF1}, {Type: tea.KeyEsc},
	} {
		app.Update(key)
		for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 100, Height: 30}} {
			app.Update(size)
			view := app.View()
			assertFrameFits(t, view, size)
			if loads != 0 || strings.Contains(view, "PRIVATE_HISTORY_CANARY") {
				t.Fatal("unlocked calendar decrypted private history")
			}
		}
	}
	if !strings.Contains(ansi.Strip(app.View()), "Private note hidden") {
		t.Fatal("unlocked session stopped labeling private previews as hidden")
	}
	cal.Update(tea.KeyMsg{Type: tea.KeyF1})
	if !strings.Contains(cal.keyHelp.content, "private notes stay hidden even while unlocked") {
		t.Fatal("F1 does not explain the calendar's public-only preview policy")
	}
	for _, date := range dates {
		if cal.contents[date] != "" || cal.previews[date] != "" {
			t.Fatal("rendering/navigation populated private preview caches")
		}
	}
}
