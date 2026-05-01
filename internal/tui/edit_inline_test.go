package tui

import (
	"errors"
	"os"
	"path/filepath"
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

	model, cmd := nb.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nb = model.(*Notebook)

	if !nb.IsQuitting() {
		t.Error("Enter without editor should quit")
	}
	if nb.GetSelectedDate() == "" {
		t.Error("Enter without editor should set selected")
	}
	if cmd == nil {
		t.Error("Enter without editor should return tea.Quit cmd")
	}
}

func TestCalendarEFallsBackWhenEditorMissing(t *testing.T) {
	cal := NewCalendar(nil)
	defer cal.Close()

	model, cmd := cal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	cal = model.(*Calendar)

	if !cal.IsDirectEdit() {
		t.Error("e without editor should set directEdit")
	}
	if !cal.quitting {
		t.Error("e without editor should quit")
	}
	if cmd == nil {
		t.Error("e without editor should return tea.Quit cmd")
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

	model, _ := nb.Update(editDoneMsg{
		date: "2024-01-01",
		path: tmpFile,
		err:  nil,
	})
	nb = model.(*Notebook)

	if got := nb.contents["2024-01-01"]; got != "# Updated\n" {
		t.Errorf("contents not refreshed: %q", got)
	}
	if got := saved["2024-01-01"]; got != "# Updated\n" {
		t.Errorf("saver not called with new content: %q", got)
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
}
