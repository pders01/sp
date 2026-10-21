package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
)

func testManager(t *testing.T) *scratchpad.Manager {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	mgr, err := scratchpad.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

func TestMakeSaverChecksOriginalContent(t *testing.T) {
	mgr := testManager(t)
	note := &scratchpad.Scratchpad{Date: "2024-01-01", Content: "original"}
	if err := mgr.Save(note); err != nil {
		t.Fatal(err)
	}
	note.Content = "newer content"
	if err := mgr.Save(note); err != nil {
		t.Fatal(err)
	}
	save := makeSaver(mgr)
	if err := save(note.Date, "original", "stale edits"); !errors.Is(err, scratchpad.ErrConflict) {
		t.Fatalf("stale save = %v, want ErrConflict", err)
	}
	got, err := mgr.GetByDate(note.Date)
	if err != nil || got.Content != "newer content" {
		t.Fatalf("stale edit overwrote current content: %+v, %v", got, err)
	}
	if err := save(note.Date, "newer content", "fresh edits"); err != nil {
		t.Fatalf("save after reloading = %v", err)
	}
}

func TestEditAndSaveRecovery(t *testing.T) {
	if strings.ContainsAny(os.Args[0], " \t\n") {
		t.Skip("editor command parser does not yet support quoted executable paths")
	}
	for _, mode := range []string{"success", "unchanged", "conflict", "failure"} {
		t.Run(mode, func(t *testing.T) {
			mgr := testManager(t)
			note := &scratchpad.Scratchpad{Date: "2024-01-01", Content: "original"}
			if err := mgr.Save(note); err != nil {
				t.Fatal(err)
			}
			tmp := t.TempDir()
			for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
				t.Setenv(name, tmp)
			}
			record := filepath.Join(tmp, "buffer-path")
			t.Setenv("SP_CLI_EDITOR_HELPER", mode)
			t.Setenv("SP_CLI_BUFFER_RECORD", record)
			t.Setenv("EDITOR", os.Args[0]+" -test.run=^TestCLIEditorHelperProcess$ --")
			ed, err := editor.NewEditor()
			if err != nil {
				t.Fatal(err)
			}
			err = editAndSave(mgr, ed, note.Date)
			wantError := mode == "conflict" || mode == "failure"
			if (err != nil) != wantError {
				t.Fatalf("editAndSave error = %v, mode = %s", err, mode)
			}
			if mode == "conflict" && !errors.Is(err, scratchpad.ErrConflict) {
				t.Errorf("save error = %v, want ErrConflict", err)
			}
			path, readErr := os.ReadFile(record)
			if readErr != nil {
				t.Fatal(readErr)
			}
			buffer, bufferErr := os.ReadFile(string(path))
			if wantError {
				if bufferErr != nil || string(buffer) != "edited" {
					t.Errorf("recovery buffer = %q, %v", buffer, bufferErr)
				}
				if !strings.Contains(err.Error(), string(path)) {
					t.Errorf("error lacks recovery path: %v", err)
				}
			} else if !os.IsNotExist(bufferErr) {
				t.Errorf("successful/unchanged edit left a buffer: %v", bufferErr)
			}
			got, loadErr := mgr.GetByDate(note.Date)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			want := "original"
			if mode == "success" {
				want = "edited"
			} else if mode == "conflict" {
				want = "other writer"
			}
			if got.Content != want {
				t.Errorf("stored content = %q, want %q", got.Content, want)
			}
		})
	}
}

// Run as the editor; the conflict case saves through a second SP process while
// the first process still holds its original edit snapshot.
func TestCLIEditorHelperProcess(_ *testing.T) {
	mode := os.Getenv("SP_CLI_EDITOR_HELPER")
	if mode == "" {
		return
	}
	path := os.Args[len(os.Args)-1]
	if err := os.WriteFile(os.Getenv("SP_CLI_BUFFER_RECORD"), []byte(path), 0o600); err != nil {
		os.Exit(2)
	}
	if mode != "unchanged" {
		if err := os.WriteFile(path, []byte("edited"), 0o600); err != nil {
			os.Exit(2)
		}
	}
	if mode == "conflict" {
		mgr, err := scratchpad.NewManager()
		if err != nil {
			os.Exit(2)
		}
		note, err := mgr.GetByDate("2024-01-01")
		if err != nil {
			os.Exit(2)
		}
		note.Content = "other writer"
		if err := mgr.Save(note); err != nil {
			os.Exit(2)
		}
	}
	if mode == "failure" {
		os.Exit(3)
	}
	os.Exit(0)
}
