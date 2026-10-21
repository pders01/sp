package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
)

func TestNotebookLoadsCurrentContentBeforeEditing(t *testing.T) {
	// Prepare creates a buffer, but this test never starts an interactive editor.
	tmp := t.TempDir()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, tmp)
	}
	const date = "2024-01-01"
	nb := NewNotebook([]string{date})
	defer nb.Close()
	nb.SetContents(map[string]string{date: "stale preview"})
	loaded := false
	nb.SetEditor(&editor.Editor{}, func(_, _, _ string) error { return nil }, func(gotDate string) (string, error) {
		loaded = true
		if gotDate != date {
			t.Errorf("load date = %q", gotDate)
		}
		return "latest on disk", nil
	})
	if cmd := nb.startEdit(date); cmd == nil {
		t.Fatal("expected a prepared editor command")
	}
	if !loaded || nb.contents[date] != "latest on disk" {
		t.Error("editor did not refresh its baseline from disk")
	}
}

func TestNotebookFinishEditUsesOriginalSnapshot(t *testing.T) {
	const date = "2024-01-01"
	path := filepath.Join(t.TempDir(), "edit.md")
	if err := os.WriteFile(path, []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	nb := NewNotebook([]string{date})
	defer nb.Close()
	// A model cache change must not replace the snapshot from editor startup.
	nb.SetContents(map[string]string{date: "edited"})
	calls := 0
	nb.save = func(gotDate, original, content string) error {
		calls++
		if gotDate != date || original != "original" || content != "edited" {
			t.Errorf("save(%q, %q, %q)", gotDate, original, content)
		}
		return scratchpad.ErrConflict
	}
	nb.Update(editDoneMsg{date: date, original: "original", path: path, cleanup: func() {
		t.Error("conflict must retain the editor buffer")
	}})
	if calls != 1 {
		t.Errorf("save called %d times, want 1 even though cache matches edited text", calls)
	}
	if !strings.Contains(nb.theme.StatusText(), path) {
		t.Error("conflict did not display a recovery path")
	}
}

func TestCalendarConflictRetainsBuffer(t *testing.T) {
	const date = "2024-01-01"
	path := filepath.Join(t.TempDir(), "edit.md")
	if err := os.WriteFile(path, []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	cal := NewCalendar([]string{date})
	defer cal.Close()
	cal.SetContents(map[string]string{date: "original"})
	cal.save = func(gotDate, original, content string) error {
		if gotDate != date || original != "original" || content != "edited" {
			t.Errorf("save(%q, %q, %q)", gotDate, original, content)
		}
		return scratchpad.ErrConflict
	}
	_, cmd := cal.Update(editDoneMsg{date: date, original: "original", path: path, cleanup: func() {
		t.Error("calendar conflict must retain the buffer")
	}})
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "edited" {
		t.Errorf("recovery buffer = %q, %v", data, err)
	}
	if cal.contents[date] != "original" {
		t.Error("conflict updated the saved-content cache")
	}
	if cmd != nil || !strings.Contains(cal.theme.StatusText(), path) {
		t.Error("conflict should show a recovery path without an expiration timer")
	}
	cal.Update(statusExpireMsg{})
	if !strings.Contains(cal.theme.StatusText(), path) {
		t.Error("an old status timer cleared the recovery path")
	}
}
