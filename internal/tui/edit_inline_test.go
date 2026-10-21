package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeEditor satisfies the bits of *editor.Editor we use without
// actually launching a process. We only exercise the wired-vs-not
// branching in startEdit; the ExecProcess path needs a real *exec.Cmd
// and is covered by integration testing through the real binary.

func TestNotebookEnterFallsBackWhenEditorMissing(t *testing.T) {
	pages := []string{"2024-01-01"}
	nb := NewNotebook(pages)
	defer nb.Close()

	model, _ := nb.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nb = model.(*Notebook)

	if !nb.IsQuitting() {
		t.Error("Enter without editor should set quitting")
	}
	if nb.GetSelectedDate() == "" {
		t.Error("Enter without editor should set selected")
	}
}

func TestCalendarEFallsBackWhenEditorMissing(t *testing.T) {
	cal := NewCalendar(nil)
	defer cal.Close()

	model, _ := cal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	cal = model.(*Calendar)

	if !cal.IsDirectEdit() {
		t.Error("e without editor should set directEdit")
	}
	if !cal.quitting {
		t.Error("e without editor should set quitting")
	}
}

func TestNotebookFinishEditPersistsChanges(t *testing.T) {
	tmp := t.TempDir()
	tmpFile := filepath.Join(tmp, "edit.md")
	if err := os.WriteFile(tmpFile, []byte("# Updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pages := []string{"2024-01-01"}
	nb := NewNotebook(pages)
	defer nb.Close()
	nb.SetContents(map[string]string{"2024-01-01": "# Old\n"})

	saved := map[string]string{}
	nb.save = func(date, content string) error {
		saved[date] = content
		return nil
	}

	cleaned := false
	model, _ := nb.Update(editDoneMsg{
		date: "2024-01-01",
		path: tmpFile,
		cleanup: func() {
			cleaned = true
			if err := os.Remove(tmpFile); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		},
	})
	nb = model.(*Notebook)

	if got := nb.contents["2024-01-01"]; got != "# Updated\n" {
		t.Errorf("contents not refreshed: %q", got)
	}
	if got := saved["2024-01-01"]; got != "# Updated\n" {
		t.Errorf("saver not called with new content: %q", got)
	}
	if !cleaned {
		t.Error("successful save should clean up the editor buffer")
	}
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Errorf("editor buffer remains after successful save: %v", err)
	}
}

func TestNotebookFinishEditPreservesBufferOnFailure(t *testing.T) {
	for _, stage := range []string{"editor", "read", "save"} {
		t.Run(stage, func(t *testing.T) {
			const date = "2024-01-01"
			path := filepath.Join(t.TempDir(), "edit.md")
			if stage == "read" {
				// A directory reliably makes ReadEdited fail on every platform.
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("# Updated\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			nb := NewNotebook([]string{date})
			defer nb.Close()
			nb.SetContents(map[string]string{date: "# Old\n"})
			saveCalls := 0
			nb.save = func(_, _ string) error {
				saveCalls++
				return errors.New("disk full")
			}
			cleaned := false
			msg := editDoneMsg{
				date: date,
				path: path,
				cleanup: func() {
					cleaned = true
					if err := os.Remove(path); err != nil {
						t.Errorf("cleanup: %v", err)
					}
				},
			}
			if stage == "editor" {
				msg.err = errors.New("editor crashed")
			}

			_, cmd := nb.Update(msg)
			if cleaned {
				t.Error("failure must not clean up the editor buffer")
			}
			if _, err := os.Stat(path); err != nil {
				t.Errorf("recovery path missing: %v", err)
			}
			if stage != "read" {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "# Updated\n" {
					t.Errorf("recovery content = %q, error = %v", data, err)
				}
			}
			if got := nb.contents[date]; got != "# Old\n" {
				t.Errorf("failed edit changed saved-content cache: %q", got)
			}
			wantCalls := 0
			if stage == "save" {
				wantCalls = 1
			}
			if saveCalls != wantCalls {
				t.Errorf("save called %d times, want %d", saveCalls, wantCalls)
			}
			if status := nb.theme.StatusText(); !strings.Contains(status, path) || !strings.Contains(status, stage+":") {
				t.Errorf("recovery status lacks error or buffer path: %q", status)
			}
			if cmd != nil {
				t.Error("recovery status should not schedule expiration")
			}
			// An old timer already in flight must not erase the recovery path.
			nb.Update(statusExpireMsg{})
			if !strings.Contains(nb.theme.StatusText(), path) {
				t.Error("an earlier status timer cleared the recovery path")
			}
		})
	}
}

func TestNotebookFinishEditRetriesAfterSaveFailure(t *testing.T) {
	const date = "2024-01-01"
	path := filepath.Join(t.TempDir(), "edit.md")
	if err := os.WriteFile(path, []byte("# Updated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nb := NewNotebook([]string{date})
	defer nb.Close()
	nb.SetContents(map[string]string{date: "# Old\n"})
	saveCalls := 0
	nb.save = func(gotDate, content string) error {
		saveCalls++
		if gotDate != date || content != "# Updated\n" {
			t.Errorf("save(%q, %q)", gotDate, content)
		}
		if nb.contents[date] != "# Old\n" {
			t.Error("cache updated before successful persistence")
		}
		if saveCalls == 1 {
			return errors.New("disk full")
		}
		return nil
	}
	cleaned := 0
	msg := editDoneMsg{
		date: date,
		path: path,
		cleanup: func() {
			cleaned++
			if err := os.Remove(path); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		},
	}
	nb.Update(msg)
	if cleaned != 0 {
		t.Fatal("failed save deleted the buffer before retry")
	}
	nb.Update(msg)
	if saveCalls != 2 {
		t.Errorf("retry called saver %d times, want 2", saveCalls)
	}
	if nb.contents[date] != "# Updated\n" {
		t.Error("successful retry did not update cached content")
	}
	if cleaned != 1 {
		t.Errorf("cleanup called %d times, want 1", cleaned)
	}
	if nb.theme.StatusText() != "Saved" {
		t.Errorf("retry status = %q, want Saved", nb.theme.StatusText())
	}
}

func TestNotebookFinishEditCleansUnchangedBuffer(t *testing.T) {
	const date = "2024-01-01"
	path := filepath.Join(t.TempDir(), "edit.md")
	if err := os.WriteFile(path, []byte("# Old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nb := NewNotebook([]string{date})
	defer nb.Close()
	nb.SetContents(map[string]string{date: "# Old\n"})
	nb.save = func(_, _ string) error {
		t.Error("unchanged buffer should not be saved")
		return nil
	}
	_, cmd := nb.Update(editDoneMsg{
		date: date,
		path: path,
		cleanup: func() {
			if err := os.Remove(path); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		},
	})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("unchanged buffer not cleaned up: %v", err)
	}
	if cmd != nil {
		t.Error("unchanged buffer should not schedule a command")
	}
}

func TestNotebookFinishEditSurfacesError(t *testing.T) {
	pages := []string{"2024-01-01"}
	nb := NewNotebook(pages)
	defer nb.Close()
	nb.SetContents(map[string]string{"2024-01-01": "# Old"})

	model, _ := nb.Update(editDoneMsg{
		date: "2024-01-01",
		err:  errors.New("editor crashed"),
	})
	nb = model.(*Notebook)

	if nb.theme.StatusText() == "" {
		t.Error("expected an error banner; got none")
	}
	if nb.contents["2024-01-01"] != "# Old" {
		t.Error("content should not have changed on editor error")
	}
}

func TestCalendarFinishEditUpdatesHasData(t *testing.T) {
	tmp := t.TempDir()
	tmpFile := filepath.Join(tmp, "edit.md")
	if err := os.WriteFile(tmpFile, []byte("# New entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cal := NewCalendar(nil)
	defer cal.Close()
	cal.save = func(_, _ string) error { return nil }

	model, _ := cal.Update(editDoneMsg{
		date: "2024-05-04",
		path: tmpFile,
	})
	cal = model.(*Calendar)

	if !cal.hasData["2024-05-04"] {
		t.Error("calendar should mark the date as having data after edit")
	}
	if got := cal.previews["2024-05-04"]; got != "New entry" {
		t.Errorf("preview = %q, want %q", got, "New entry")
	}
	if got := cal.contents["2024-05-04"]; got != "# New entry\n" {
		t.Errorf("content = %q, want edited document", got)
	}
}
