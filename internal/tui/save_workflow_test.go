package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
)

func workflowEdit(t *testing.T, date, original, content string) editDoneMsg {
	t.Helper()
	path := filepath.Join(t.TempDir(), "edit.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return editDoneMsg{date: date, original: original, path: path, cleanup: func() {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
	}}
}

func workflowApp(t *testing.T, mode AppMode, contents map[string]string) *App {
	t.Helper()
	dates := make([]string, 0, len(contents))
	for date := range contents {
		dates = append(dates, date)
	}
	app := NewApp(NewCalendar(dates), NewNotebook(dates), mode)
	t.Cleanup(app.Close)
	app.cal.SetContents(contents)
	owned := make(map[string]string, len(contents))
	for date, content := range contents {
		owned[date] = content
	}
	app.nb.SetContents(owned)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	return app
}

func TestSaveWorkflowExternalEditsSynchronizeBothViews(t *testing.T) {
	for _, mode := range []AppMode{ModeCalendar, ModeNotebook} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			const first, second = "2024-01-02", "2024-01-01"
			app := workflowApp(t, mode, map[string]string{first: "old first", second: "old second"})
			saves := 0
			save := func(_, _, _ string) error { saves++; return nil }
			app.cal.save, app.nb.save = save, save
			app.nb.SetCurrentDate(first)
			app.Update(workflowEdit(t, first, "old first", "updated first"))
			if mode == ModeNotebook {
				app.Update(tea.KeyMsg{Type: tea.KeyRight})
			}
			app.Update(workflowEdit(t, second, "old second", "updated second"))
			if saves != 2 {
				t.Fatalf("saves = %d", saves)
			}
			for date, want := range map[string]string{first: "updated first", second: "updated second"} {
				if app.nb.contents[date] != want || app.cal.contents[date] != want || app.cal.previews[date] != want {
					t.Fatalf("views disagree for %s: notebook=%q calendar=%q preview=%q", date,
						app.nb.contents[date], app.cal.contents[date], app.cal.previews[date])
				}
			}
		})
	}
}

func TestSaveWorkflowCurrentPageRefreshPreservesReadingPosition(t *testing.T) {
	const date = "2024-01-01"
	original := strings.Repeat("line\n\n", 60)
	app := workflowApp(t, ModeNotebook, map[string]string{date: original})
	app.nb.viewport.SetYOffset(10)
	app.nb.save = func(_, _, _ string) error { return nil }
	app.Update(workflowEdit(t, date, original, original+"new line\n"))
	if app.nb.viewport.YOffset != 10 {
		t.Fatal("refreshing the current page unexpectedly reset its scroll position")
	}
}

func TestSaveWorkflowCalendarCreationPreservesNotebookFocus(t *testing.T) {
	const oldDate, newDate = "2024-01-01", "2024-01-02"
	app := workflowApp(t, ModeCalendar, map[string]string{oldDate: "existing note"})
	app.cal.SetCursor(newDate)
	app.cal.save = func(_, _, _ string) error { return nil }
	app.Update(workflowEdit(t, newDate, "", "new note"))
	if app.nb.GetCurrentPage() != oldDate || !app.cal.HasData(newDate) {
		t.Fatal("new publication changed notebook focus or was not marked in the calendar")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	date, content := app.nb.CurrentContent()
	if date != newDate || content != "new note" {
		t.Fatalf("drill after save = %q, %q", date, content)
	}
}

func TestSaveWorkflowClearingNoteRemovesStalePreview(t *testing.T) {
	for _, shared := range []string{"", "shared contribution"} {
		t.Run("shared="+shared, func(t *testing.T) {
			const date = "2024-01-01"
			app := workflowApp(t, ModeNotebook, map[string]string{date: "old private text"})
			app.SetSharedContents(map[string]string{date: shared})
			app.nb.save = func(_, _, _ string) error { return nil }
			app.Update(workflowEdit(t, date, "old private text", ""))
			if app.nb.contents[date] != "" || app.cal.contents[date] != shared || app.cal.previews[date] != shared {
				t.Fatal("clearing private text left a stale preview or hid shared content")
			}
			if !app.cal.HasData(date) {
				t.Fatal("an empty saved page should still exist")
			}
			app.canPop = true
			app.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if app.cal.contents[date] != shared {
				t.Fatal("back navigation overwrote the synchronized preview")
			}
		})
	}
}

func TestSaveWorkflowUnchangedAndFailedEditsDoNotCreatePages(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			const date = "2024-01-01"
			app := workflowApp(t, ModeCalendar, nil)
			calls := 0
			app.cal.save = func(_, _, _ string) error { calls++; return errors.New("disk full") }
			content := ""
			if failure {
				content = "unsaved draft"
			}
			msg := workflowEdit(t, date, "", content)
			app.Update(msg)
			if app.cal.HasData(date) || len(app.nb.pages) != 0 {
				t.Fatal("a non-publication created a phantom page")
			}
			if failure {
				if calls != 1 || !strings.Contains(app.cal.theme.StatusText(), "disk full") {
					t.Fatal("failed write was not reported")
				}
				if _, err := os.Stat(msg.path); err != nil {
					t.Fatal("failed write lost its buffer")
				}
			} else if calls != 0 || app.cal.theme.StatusText() != "No changes." {
				t.Fatal("unchanged edit wrote data or claimed it was saved")
			}
		})
	}
}

func TestSaveWorkflowExternalDurabilityWarningStillRefreshesViews(t *testing.T) {
	for _, mode := range []AppMode{ModeCalendar, ModeNotebook} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			const date = "2024-01-01"
			app := workflowApp(t, mode, map[string]string{date: "old"})
			save := func(_, _, _ string) error { return fmt.Errorf("sync: %w", scratchpad.ErrDurabilityUnconfirmed) }
			app.cal.save, app.nb.save = save, save
			msg := workflowEdit(t, date, "old", "published")
			_, cmd := app.Update(msg)
			if app.cal.contents[date] != "published" || app.nb.contents[date] != "published" {
				t.Fatal("published-but-unconfirmed content was not synchronized")
			}
			status := app.cal.theme.StatusText()
			if mode == ModeNotebook {
				status = app.nb.theme.StatusText()
			}
			if !strings.Contains(status, "Saved, but durability is unconfirmed") || !strings.Contains(status, msg.path) || cmd != nil {
				t.Fatalf("durability outcome = %q, expiration=%v", status, cmd != nil)
			}
			if _, err := os.Stat(msg.path); err != nil {
				t.Fatal("unconfirmed write discarded its recovery buffer")
			}
		})
	}
}

type workflowPrivateStore struct {
	private    map[string]string
	shared     map[string]string
	loads      []string
	locked     bool
	saveErr    error
	loadErr    error
	inspectErr error
}

func (s *workflowPrivateStore) hooks() PrivacyHooks {
	return PrivacyHooks{
		IsUnlocked: func() bool { return !s.locked },
		Inspect: func(date string) (PublicPage, error) {
			_, exists := s.private[date]
			return PublicPage{Shared: s.shared[date], HasPrivate: exists}, s.inspectErr
		},
		Load: func(date string) (PrivatePage, error) {
			s.loads = append(s.loads, date)
			_, exists := s.private[date]
			page := PrivatePage{Shared: s.shared[date], HasPrivate: exists, Locked: s.locked}
			if !s.locked {
				page.Private = s.private[date]
			}
			return page, s.loadErr
		},
		Save: func(date, original, content string) error {
			if original != s.private[date] {
				return scratchpad.ErrConflict
			}
			if s.saveErr == nil || errors.Is(s.saveErr, scratchpad.ErrDurabilityUnconfirmed) {
				s.private[date] = content
			}
			return s.saveErr
		},
		Recover: func(_, _ string) (string, error) { return "synthetic-recovery.age", nil },
	}
}

func assertPublicCaches(t *testing.T, app *App) {
	t.Helper()
	for _, cache := range []map[string]string{app.nb.contents, app.cal.contents, app.cal.previews} {
		for date, content := range cache {
			if strings.Contains(content, "PRIVATE_") {
				t.Fatalf("private content in public projection for %s", date)
			}
		}
	}
}

func TestSaveWorkflowPrivateCalendarSavePublishesOnlyPublicState(t *testing.T) {
	const oldDate, newDate = "2024-01-01", "2024-01-02"
	store := &workflowPrivateStore{private: map[string]string{oldDate: "PRIVATE_ARCHIVE"}, shared: map[string]string{}}
	app := workflowApp(t, ModeCalendar, map[string]string{oldDate: ""})
	app.SetPrivacy(store.hooks())
	if len(store.loads) != 0 {
		t.Fatal("calendar inspected the private archive")
	}
	app.cal.SetCursor(newDate)
	app.Update(privacyKey("e"))
	app.privacy.editor.input.SetValue("PRIVATE_NEW")
	store.shared[newDate] = "concurrent shared bullet"
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if store.private[newDate] != "PRIVATE_NEW" || !app.cal.HasData(newDate) || !app.cal.privateDates[newDate] {
		t.Fatal("private publication was not reflected in public presence")
	}
	if app.nb.GetCurrentPage() != oldDate || len(app.nb.pages) != 2 {
		t.Fatal("private creation changed focus or failed to insert the page")
	}
	if app.cal.contents[newDate] != store.shared[newDate] || app.nb.contents[newDate] != store.shared[newDate] {
		t.Fatal("private save did not refresh the shared projection")
	}
	if len(store.loads) != 1 || store.loads[0] != newDate {
		t.Fatalf("calendar save loaded private dates: %v", store.loads)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.nb.GetCurrentPage() != newDate || !strings.Contains(app.View(), "PRIVATE_NEW") {
		t.Fatal("new private note was not available after drilling into the notebook")
	}
	assertPublicCaches(t, app)
}

func TestSaveWorkflowPrivateNoOpOutcomes(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEsc, tea.KeyCtrlC, tea.KeyCtrlS} {
		t.Run(key.String(), func(t *testing.T) {
			app := workflowApp(t, ModeCalendar, nil)
			store := &workflowPrivateStore{private: map[string]string{}, shared: map[string]string{}}
			app.SetPrivacy(store.hooks())
			app.Update(privacyKey("e"))
			app.Update(tea.KeyMsg{Type: key})
			if app.privacy.editor != nil || len(store.private) != 0 || len(app.nb.pages) != 0 || app.cal.HasData(app.cal.CursorDate()) {
				t.Fatal("unchanged or cancelled private edit manufactured a saved entry")
			}
			status := app.cal.theme.StatusText()
			if strings.Contains(strings.ToLower(status), "saved") || !strings.Contains(strings.ToLower(status), "no changes") {
				t.Fatalf("no-op outcome = %q", status)
			}
		})
	}
}

func TestSaveWorkflowPrivateFailureAndDurabilityOutcomes(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(fmt.Sprint(published), func(t *testing.T) {
			const date = "2024-01-01"
			store := &workflowPrivateStore{private: map[string]string{}, shared: map[string]string{}}
			store.saveErr = errors.New("disk full")
			if published {
				store.saveErr = scratchpad.ErrDurabilityUnconfirmed
			}
			app := workflowApp(t, ModeCalendar, nil)
			app.cal.SetCursor(date)
			app.SetPrivacy(store.hooks())
			app.Update(privacyKey("e"))
			app.privacy.editor.input.SetValue("PRIVATE_DRAFT")
			app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
			e := app.privacy.editor
			if e == nil || e.input.Value() != "PRIVATE_DRAFT" || app.cal.HasData(date) != published || (len(app.nb.pages) == 1) != published {
				t.Fatal("private failure/publication outcome was not preserved")
			}
			if published {
				if strings.Contains(e.status, "Save failed") || !strings.Contains(e.status, "durability is unconfirmed") {
					t.Fatalf("uncertain publication = %q", e.status)
				}
				store.saveErr = nil
				app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
				if app.privacy.editor != nil || app.cal.theme.StatusText() != "Private note saved." {
					t.Fatal("retry did not use the published baseline")
				}
			} else if !strings.Contains(e.status, "Save failed") {
				t.Fatalf("failed publication = %q", e.status)
			}
			assertPublicCaches(t, app)
		})
	}
}

func TestSaveWorkflowTemplatesCreateSelectablePagesWithoutMovingFocus(t *testing.T) {
	for _, private := range []bool{false, true} {
		t.Run(fmt.Sprint(private), func(t *testing.T) {
			const oldDate, newDate = "2024-01-01", "2024-01-02"
			app := workflowApp(t, ModeCalendar, map[string]string{oldDate: ""})
			store := &workflowPrivateStore{private: map[string]string{}, shared: map[string]string{}}
			if private {
				app.SetPrivacy(store.hooks())
			}
			app.cal.SetCursor(newDate)
			app.SetTemplates([]DayTemplate{{ID: "one", Name: "One"}}, nil,
				func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
					content := "template note"
					if private {
						content = "PRIVATE_TEMPLATE"
						store.private[newDate] = content
					}
					return TemplateApplyResult{Content: content, Applied: []string{"one"}}, nil
				})
			app.Update(privacyKey("a"))
			app.Update(privacyKey(" "))
			_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Fatal("template application did not start")
			}
			app.Update(cmd())
			if len(app.nb.pages) != 2 || app.nb.GetCurrentPage() != oldDate || !app.cal.HasData(newDate) {
				t.Fatal("template publication lost the new page or moved focus")
			}
			if private {
				assertPublicCaches(t, app)
			} else {
				app.nb.SetCurrentDate(newDate)
				if _, content := app.nb.CurrentContent(); content != "template note" {
					t.Fatal("new template page was not selectable with its persisted content")
				}
			}
		})
	}
}

func TestSaveWorkflowUnchangedExternalEditRefreshesLoadedSnapshot(t *testing.T) {
	for _, mode := range []AppMode{ModeCalendar, ModeNotebook} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			const date = "2024-01-01"
			app := workflowApp(t, mode, map[string]string{date: "stale preview"})
			dir := t.TempDir()
			for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
				t.Setenv(name, dir)
			}
			save := func(_, _, _ string) error { t.Fatal("unchanged edit should not save"); return nil }
			load := func(string) (string, error) { return "fresh on disk", nil }
			// Prepare the real buffer without running an interactive process.
			app.cal.SetEditor(&editor.Editor{}, save, load)
			app.nb.SetEditor(&editor.Editor{}, save, load)
			if mode == ModeCalendar {
				app.cal.startEdit(date)
			} else {
				app.nb.startEdit(date)
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 1 {
				t.Fatalf("prepared buffers: %v, %v", files, err)
			}
			path := filepath.Join(dir, files[0].Name())
			app.Update(editDoneMsg{date: date, original: "fresh on disk", path: path, cleanup: func() { _ = os.Remove(path) }})
			if app.cal.contents[date] != "fresh on disk" || app.nb.contents[date] != "fresh on disk" {
				t.Fatal("returning unchanged from an editor left another view stale")
			}
		})
	}
}

func TestSaveWorkflowTemplateDurabilityOutcomePreservesPublishedState(t *testing.T) {
	for _, private := range []bool{false, true} {
		t.Run(fmt.Sprint(private), func(t *testing.T) {
			const date = "2024-01-01"
			app := workflowApp(t, ModeCalendar, nil)
			content := "published template"
			if private {
				content = "PRIVATE_TEMPLATE"
				store := &workflowPrivateStore{private: map[string]string{date: content}, shared: map[string]string{}}
				app.SetPrivacy(store.hooks())
			}
			_, cmd := app.Update(templateAppliedMsg{
				date: date, err: fmt.Errorf("sync: %w", scratchpad.ErrDurabilityUnconfirmed),
				result: TemplateApplyResult{Content: content, Applied: []string{"one"}, Published: true},
			})
			if !app.cal.HasData(date) || len(app.nb.pages) != 1 || !app.templateApplied(date, "one") {
				t.Fatal("published template was treated as an unpublished failure")
			}
			if cmd != nil || app.templateChooser != nil || (app.privacy != nil && app.privacy.editor != nil) {
				t.Fatal("durability warning was hidden by expiration or reopening an editor")
			}
			if !strings.Contains(app.cal.theme.StatusText(), "Templates saved, but durability is unconfirmed") {
				t.Fatal("template publication warning was not explicit")
			}
			if private {
				assertPublicCaches(t, app)
			} else if app.nb.contents[date] != content || app.cal.contents[date] != content {
				t.Fatal("published template content was not synchronized")
			}
		})
	}
}

func TestSaveWorkflowVisitingMissingDateDoesNotMarkItSaved(t *testing.T) {
	app := workflowApp(t, ModeCalendar, nil)
	date := app.cal.CursorDate()
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.cal.HasData(date) {
		t.Fatal("visiting and leaving an unsaved page created a calendar entry")
	}
}
