package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPrivatePageStatesDistinguishHiddenAbsentSharedAndUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		page   PrivatePage
		want   string
		absent []string
	}{
		{"hidden", PrivatePage{Locked: true, HasPrivate: true}, "Note locked. Press u", []string{"No personal note", "unavailable"}},
		{"absent locked", PrivatePage{Locked: true}, "No personal note for this day", []string{"Note locked.", "# Private notes", "Agent contributions"}},
		{"absent unlocked", PrivatePage{}, "No personal note for this day", []string{"Note locked.", "# Private notes", "Agent contributions"}},
		{"shared-only locked", PrivatePage{Locked: true, Shared: "public bullet"}, "No personal note for this day", []string{"Note locked.", "public bullet", "# Private notes"}},
		{"shared-only unlocked", PrivatePage{Shared: "public bullet"}, "No personal note for this day", []string{"Note locked.", "public bullet", "# Private notes"}},
		{"empty private", PrivatePage{HasPrivate: true}, "This note is empty", []string{"No personal note", "Note locked.", "Agent contributions"}},
		{"unavailable", PrivatePage{LoadError: "damaged document"}, "Personal note unavailable: damaged document", []string{"No personal note", "Note locked.", "is empty"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := NewNotebook(nil)
			defer n.Close()
			n.privacyEnabled = true
			view := n.renderPrivateAreas(tc.page)
			if !strings.Contains(view, tc.want) {
				t.Fatalf("page state = %q, want %q", view, tc.want)
			}
			for _, absent := range tc.absent {
				if strings.Contains(view, absent) {
					t.Errorf("page state %q incorrectly contains %q", view, absent)
				}
			}
		})
	}
}

func TestCalendarDistinguishesEmptySavedPageFromMissingDate(t *testing.T) {
	app := workflowApp(t, ModeCalendar, map[string]string{"2024-01-01": ""})
	app.cal.SetCursor("2024-01-01")
	if view := app.View(); !strings.Contains(view, "Entry is empty.") || strings.Contains(view, "No entry for this day") {
		t.Fatal("empty saved page was described as absent")
	}
	app.cal.SetCursor("2024-01-02")
	if view := app.View(); !strings.Contains(view, "No entry for this day") || strings.Contains(view, "Entry is empty.") {
		t.Fatal("missing date was described as a saved empty page")
	}
}

func TestPrivateCalendarUsesPublicPresenceWithoutDecrypting(t *testing.T) {
	const privateDate, sharedDate, absentDate = "2024-01-01", "2024-01-02", "2024-01-03"
	store := &workflowPrivateStore{
		private: map[string]string{privateDate: "PRIVATE_HIDDEN"},
		shared:  map[string]string{sharedDate: "public contribution"},
		locked:  true,
	}
	app := workflowApp(t, ModeCalendar, map[string]string{privateDate: "", sharedDate: ""})
	app.SetPrivacy(store.hooks())
	for _, unlocked := range []bool{false, true} {
		store.locked = !unlocked
		app.cal.SetCursor(privateDate)
		view := app.View()
		if !strings.Contains(view, "Private note hidden") || strings.Contains(view, "No entry for this day") {
			t.Fatal("private presence was represented as an empty date")
		}
		if !strings.Contains(app.cal.focusLine(), "private note hidden") {
			t.Fatal("short calendar focus did not identify hidden private content")
		}
		app.cal.SetCursor(sharedDate)
		if view := app.View(); !strings.Contains(view, "public contribution") || !strings.Contains(strings.ToLower(view), "shared") {
			t.Fatal("shared-only preview was not available or labelled")
		}
		app.cal.SetCursor(absentDate)
		if view := app.View(); !strings.Contains(view, "No entry for this day") || strings.Contains(view, "Private note hidden") {
			t.Fatal("absent date was represented as hidden private content")
		}
	}
	if len(store.loads) != 0 {
		t.Fatalf("calendar decrypted private pages: %v", store.loads)
	}
	assertPublicCaches(t, app)
}

func TestPrivatePageLoadErrorsAreNotPresentedAsLockedOrEmpty(t *testing.T) {
	const date = "2024-01-01"
	store := &workflowPrivateStore{private: map[string]string{date: "PRIVATE_ORIGINAL"}, shared: map[string]string{}}
	app := workflowApp(t, ModeNotebook, map[string]string{date: ""})
	app.SetPrivacy(store.hooks())
	store.loadErr = errors.New("damaged document")
	app.refreshPrivatePage()
	view := app.View()
	if !strings.Contains(view, "Personal note unavailable") || strings.Contains(view, "PRIVATE_ORIGINAL") || strings.Contains(view, "Locked.") || strings.Contains(view, "No entry") {
		t.Fatalf("failed private load has misleading state: %q", view)
	}
	app.canPop = true
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if view := app.View(); !strings.Contains(view, "Entry unavailable") || strings.Contains(view, "No entry for this day") {
		t.Fatal("failed private load was lost when returning to the calendar")
	}
	assertPublicCaches(t, app)
}

func TestPrivatePublicInspectionErrorsDoNotMasqueradeAsEmptyDates(t *testing.T) {
	const date = "2024-01-01"
	store := &workflowPrivateStore{
		private: map[string]string{date: "PRIVATE_HIDDEN"}, shared: map[string]string{date: "previous shared preview"},
	}
	app := workflowApp(t, ModeCalendar, map[string]string{date: ""})
	app.SetPrivacy(store.hooks())
	app.cal.SetCursor(date)
	store.inspectErr = errors.New("cannot read document")
	app.refreshPublicPage(date)
	if view := app.View(); !strings.Contains(view, "Entry unavailable") || strings.Contains(view, "No entry for this day") {
		t.Fatal("failed metadata inspection looked like an empty date")
	}
	if !strings.Contains(app.cal.focusLine(), "unavailable") {
		t.Fatal("short calendar showed a stale preview instead of the load error")
	}
	if len(store.loads) != 0 {
		t.Fatal("failed public inspection fell back to private loading")
	}
}
