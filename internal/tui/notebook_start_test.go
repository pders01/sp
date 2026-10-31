package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNotebookStartupPrefersTodayOverFuturePage(t *testing.T) {
	today := "2026-02-14"
	n := newNotebookAt([]string{"2026-02-13", today, "2026-02-15"}, today)
	defer n.Close()
	if n.GetCurrentPage() != today {
		t.Fatalf("notebook opened %s instead of today %s", n.GetCurrentPage(), today)
	}
}

func TestNotebookStartupFallbacksAreExplicitAndDoNotInsertToday(t *testing.T) {
	for _, tc := range []struct {
		pages        []string
		want, status string
	}{
		{[]string{"2026-02-13", "2026-03-01", "2026-02-12"}, "2026-02-13", "newest earlier"},
		{[]string{"2026-03-01", "2026-02-15"}, "2026-02-15", "nearest future"},
		{[]string{"2026-02-14", "2026-03-01"}, "2026-02-14", ""},
	} {
		n := newNotebookAt(tc.pages, "2026-02-14")
		app := NewApp(NewCalendar(tc.pages), n, ModeNotebook)
		if n.GetCurrentPage() != tc.want || len(n.pages) != len(tc.pages) || !strings.Contains(n.theme.StatusText(), tc.status) {
			t.Fatalf("startup selected %s, status %q", n.GetCurrentPage(), n.theme.StatusText())
		}
		app.Close()
	}
}

func TestNotebookEmptyPrivateCreationResumesAfterUnlockWithoutPhantomSave(t *testing.T) {
	for _, locked := range []bool{false, true} {
		app := workflowApp(t, ModeNotebook, nil)
		unlocked, saves := !locked, 0
		app.SetPrivacy(PrivacyHooks{
			IsUnlocked: func() bool { return unlocked },
			Unlock:     func([]byte) error { unlocked = true; return nil },
			Load: func(date string) (PrivatePage, error) {
				if date != app.nb.today {
					t.Fatal("creation loaded the wrong date")
				}
				return PrivatePage{Locked: !unlocked}, nil
			},
			Save: func(string, string, string) error { saves++; return nil },
		})
		app.Update(privacyKey("e"))
		if locked {
			if app.privacy.prompt == nil {
				t.Fatal("locked creation did not prompt")
			}
			app.Update(privacyKey("synthetic"))
			app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
		if app.privacy.editor == nil || app.nb.GetCurrentPage() != app.nb.today || len(app.cal.hasData) != 0 || saves != 0 {
			t.Fatal("creation failed or manufactured a saved note")
		}
		app.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if app.privacy.editor != nil || len(app.cal.hasData) != 0 || saves != 0 || !strings.Contains(app.View(), "No personal note for this day") {
			t.Fatal("unchanged cancellation persisted a note or hid absence")
		}
	}
}

func TestEmptyNotebookLiteralEnterTextDoesNotCreate(t *testing.T) {
	app := workflowApp(t, ModeNotebook, nil)
	app.Update(privacyKey("enter"))
	if len(app.nb.pages) != 0 || app.IsQuitting() {
		t.Fatal("literal key-name text triggered an empty-state creation")
	}
}

func TestNotebookEmptyViewIsBoundedAndClosingDoesNotCreate(t *testing.T) {
	app := workflowApp(t, ModeNotebook, nil)
	for _, size := range layoutSizes {
		app.Update(size)
		assertFrameFits(t, app.View(), size)
	}
	app.Update(privacyKey("q"))
	if len(app.nb.pages) != 0 || len(app.cal.hasData) != 0 {
		t.Fatal("closing the empty notebook created a note")
	}
}

func TestNotebookEmptyStateOffersCreationWithoutSavingOnOpen(t *testing.T) {
	app := workflowApp(t, ModeNotebook, nil)
	if app.nb.GetCurrentPage() != "" || len(app.cal.hasData) != 0 {
		t.Fatal("opening empty notebook created an entry")
	}
	if !strings.Contains(app.View(), "create today") {
		t.Fatal("empty notebook does not offer creation")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.nb.GetSelectedDate() != time.Now().Format(time.DateOnly) || !app.IsQuitting() {
		t.Fatal("unwired creation did not select today for the editor orchestrator")
	}
	if len(app.cal.hasData) != 0 {
		t.Fatal("creation intent was represented as a saved entry")
	}
}
