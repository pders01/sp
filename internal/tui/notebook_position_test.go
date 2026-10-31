package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNotebookReadingPositionSurvivesCalendarRoundTripsAndInsertions(t *testing.T) {
	const first, second = "2026-02-02", "2026-02-01"
	app := workflowApp(t, ModeCalendar, map[string]string{
		first: strings.Repeat("first\n\n", 120), second: strings.Repeat("second\n\n", 120),
	})
	app.cal.SetCursor(first)
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app.nb.viewport.SetYOffset(12)
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.nb.viewport.YOffset != 12 {
		t.Fatal("re-entering the same date reset reading position")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app.cal.SetCursor(second)
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app.nb.viewport.SetYOffset(22)
	app.noteChanged("2026-02-03", "new future page")
	if app.nb.GetCurrentPage() != second {
		t.Fatal("insertion moved focus")
	}
	app.nb.SetCurrentDate(first)
	if app.nb.viewport.YOffset != 12 {
		t.Fatal("first date's position followed its old index")
	}
	app.nb.SetCurrentDate(second)
	if app.nb.viewport.YOffset != 22 {
		t.Fatal("second date's position followed its old index")
	}
	app.nb.SetCurrentDate(second)
	if app.nb.viewport.YOffset != 22 {
		t.Fatal("same-date selection reset the viewport")
	}
}

func TestPrivateReadingPositionSurvivesIntermediateAndFailedLoads(t *testing.T) {
	const first, second = "2026-02-02", "2026-02-01"
	app := workflowApp(t, ModeNotebook, map[string]string{first: "shared", second: "shared"})
	fail := false
	loads := 0
	app.SetPrivacy(PrivacyHooks{Load: func(date string) (PrivatePage, error) {
		loads++
		if fail {
			return PrivatePage{}, errors.New("synthetic read failure")
		}
		return PrivatePage{Private: strings.Repeat("PRIVATE_CANARY_"+date+"\n\n", 120), HasPrivate: true}, nil
	}})
	app.nb.viewport.SetYOffset(15)
	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	app.nb.viewport.SetYOffset(25)
	// Deliberately inspect the transition before the router loads the new page.
	app.nb.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if app.nb.viewportReady || app.nb.positions[first] != 15 {
		t.Fatal("locked placeholder overwrote the remembered private position")
	}
	app.refreshPrivatePage()
	if app.nb.viewport.YOffset != 15 {
		t.Fatal("private position was lost before the page finished loading")
	}
	fail = true
	app.refreshPrivatePage()
	if app.nb.viewportReady || app.nb.positions[first] != 15 {
		t.Fatal("failed load overwrote reading position")
	}
	fail = false
	app.refreshPrivatePage()
	if app.nb.viewport.YOffset != 15 || loads != 5 {
		t.Fatal("recovery from load failure lost position or loaded extra archive pages")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	if app.nb.viewport.YOffset != 25 {
		t.Fatal("other private page did not restore its own position")
	}
	for _, cache := range []map[string]string{app.nb.contents, app.cal.contents, app.cal.previews} {
		for _, value := range cache {
			if strings.Contains(value, "PRIVATE_CANARY") {
				t.Fatal("position tracking copied private text into public caches")
			}
		}
	}
}

func TestReadingPositionClampsOnEditsAndResizeAndRemembersTop(t *testing.T) {
	const first, second = "2026-02-02", "2026-02-01"
	app := workflowApp(t, ModeNotebook, map[string]string{
		first: strings.Repeat("first\n\n", 120), second: strings.Repeat("second\n\n", 120),
	})
	app.nb.viewport.SetYOffset(18)
	app.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if app.nb.viewport.YOffset != 18 {
		t.Fatal("ordinary reflow reset position")
	}
	app.nb.SetCurrentDate(second)
	app.nb.SetPageContent(first, "shortened")
	app.nb.SetCurrentDate(first)
	if app.nb.viewport.YOffset != 0 || app.nb.positions[first] != 0 {
		t.Fatal("shortened document did not clamp its remembered position")
	}
	app.nb.SetCurrentDate(second)
	app.nb.viewport.SetYOffset(20)
	app.Update(privacyKey("g"))
	app.nb.SetCurrentDate(first)
	app.nb.SetCurrentDate(second)
	if app.nb.viewport.YOffset != 0 {
		t.Fatal("explicit top navigation was not remembered")
	}
	app.nb.viewport.SetYOffset(20)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 1000})
	if app.nb.viewport.YOffset != 0 {
		t.Fatal("viewport did not clamp when the whole page fitted")
	}
}

func TestNotebookRestoresIndependentReadingPositions(t *testing.T) {
	app := workflowApp(t, ModeNotebook, map[string]string{
		"2026-02-02": strings.Repeat("newer line\n\n", 120),
		"2026-02-01": strings.Repeat("older line\n\n", 120),
	})
	app.nb.SetCurrentDate("2026-02-02")
	app.nb.viewport.SetYOffset(10)
	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	if app.nb.viewport.YOffset != 0 {
		t.Fatal("an unread page inherited the previous page's position")
	}
	app.nb.viewport.SetYOffset(20)
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if app.nb.viewport.YOffset != 10 {
		t.Fatalf("newer page restored row %d, want 10", app.nb.viewport.YOffset)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	if app.nb.viewport.YOffset != 20 {
		t.Fatalf("older page restored row %d, want 20", app.nb.viewport.YOffset)
	}
}
