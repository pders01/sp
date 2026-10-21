package scratchpad

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func assertNoSaveTemps(t *testing.T, mgr *Manager) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(mgr.storageDir, ".sp-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Errorf("temporary save files remain: %v", paths)
	}
}

func TestSaveRejectsNil(t *testing.T) {
	mgr := setupTestManager(t)
	if err := mgr.Save(nil); err == nil {
		t.Fatal("Save(nil) should return an error")
	}
	assertNoSaveTemps(t, mgr)
}

func TestSaveReplacesExistingNote(t *testing.T) {
	mgr := setupTestManager(t)
	created := time.Now().Add(-time.Hour)
	note := &Scratchpad{Date: "2024-02-29", Content: "old content", Created: created}
	if err := mgr.Save(note); err != nil {
		t.Fatal(err)
	}
	note.Content = "new content"
	note.AppliedTemplates = []string{"tasks"}
	if err := mgr.Save(note); err != nil {
		t.Fatal(err)
	}
	got, err := mgr.GetByDate(note.Date)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != note.Content || got.Date != note.Date {
		t.Errorf("replacement = %+v, want %+v", got, note)
	}
	if !got.Modified.Equal(note.Modified) || !got.Created.Equal(created) {
		t.Errorf("timestamps on disk differ from saved note: %+v", got)
	}
	if len(got.AppliedTemplates) != 1 || got.AppliedTemplates[0] != "tasks" {
		t.Errorf("replacement metadata = %v", got.AppliedTemplates)
	}
	assertNoSaveTemps(t, mgr)
}

func TestSaveFailureLeavesModifiedUnchanged(t *testing.T) {
	for _, stage := range []string{"marshal", "create", "read"} {
		t.Run(stage, func(t *testing.T) {
			mgr := setupTestManager(t)
			old := time.Now().Add(-time.Hour)
			note := &Scratchpad{Date: "2024-01-01", Content: "new", Created: old, Modified: old}
			switch stage {
			case "marshal":
				// time.Time refuses years that cannot be encoded as RFC 3339.
				note.Created = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			case "create":
				mgr.storageDir = filepath.Join(mgr.storageDir, "missing")
			case "read":
				// A directory cannot be read as a note when checking its revision.
				target := filepath.Join(mgr.storageDir, note.Date+".json")
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(target, "sentinel"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := mgr.save(note, func(string) error {
				t.Error("directory sync must not run before successful replacement")
				return nil
			}); err == nil {
				t.Fatal("expected save to fail")
			}
			if !note.Modified.Equal(old) {
				t.Errorf("failed save changed Modified from %v to %v", old, note.Modified)
			}
			assertNoSaveTemps(t, mgr)
			if stage == "read" {
				data, err := os.ReadFile(filepath.Join(mgr.storageDir, note.Date+".json", "sentinel"))
				if err != nil || string(data) != "keep" {
					t.Errorf("failed replacement changed original target: %q, %v", data, err)
				}
			}
		})
	}
}

func TestSaveMarshalFailurePreservesOriginal(t *testing.T) {
	mgr := setupTestManager(t)
	note := &Scratchpad{Date: "2024-01-01", Content: "original", Created: time.Now()}
	if err := mgr.Save(note); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(mgr.storageDir, note.Date+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	note.Content = "replacement"
	note.Created = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := mgr.Save(note); err == nil {
		t.Fatal("expected marshal failure")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Errorf("failed save changed original: %q, error = %v", after, err)
	}
	assertNoSaveTemps(t, mgr)
}

func TestSaveReportsDirectorySyncFailureAfterReplacement(t *testing.T) {
	mgr := setupTestManager(t)
	old := time.Now().Add(-time.Hour)
	note := &Scratchpad{Date: "2024-01-01", Content: "original", Created: old}
	if err := mgr.Save(note); err != nil {
		t.Fatal(err)
	}
	previousModified := note.Modified
	note.Content = "replacement"
	syncErr := errors.New("directory sync failed")
	calls := 0
	err := mgr.save(note, func(dir string) error {
		calls++
		if dir != mgr.storageDir {
			t.Errorf("sync directory = %q, want %q", dir, mgr.storageDir)
		}
		got, readErr := mgr.GetByDate(note.Date)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if got.Content != note.Content || !got.Modified.Equal(note.Modified) {
			t.Errorf("directory sync ran before replacement/timestamp update: %+v", got)
		}
		return syncErr
	})
	if !errors.Is(err, ErrDurabilityUnconfirmed) || !errors.Is(err, syncErr) {
		t.Fatalf("Save error = %v, want published-but-unconfirmed durability", err)
	}
	if !strings.Contains(err.Error(), "replaced") {
		t.Errorf("error does not explain that replacement already happened: %v", err)
	}
	if calls != 1 {
		t.Errorf("directory sync called %d times, want 1", calls)
	}
	if !note.Modified.After(previousModified) {
		t.Error("published note's Modified was not updated")
	}
	// A published revision must remain usable even when directory sync failed.
	note.Content = "next edit"
	if err := mgr.Save(note); err != nil {
		t.Errorf("save after unconfirmed durability = %v", err)
	}
	assertNoSaveTemps(t, mgr)
}

func TestDateOperationsRejectInvalidDates(t *testing.T) {
	for _, date := range []string{
		"", "../outside", "2024/01/01", `2024\01\01`,
		"2024-1-01", "2024-01-1", "2023-02-29", "2024-02-30",
		"2024-13-01", "2024-01-01.new", "2024-01-01\n",
	} {
		t.Run(date, func(t *testing.T) {
			root := t.TempDir()
			mgr := &Manager{storageDir: filepath.Join(root, "notes")}
			if err := os.Mkdir(mgr.storageDir, 0o700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(root, "outside.json")
			const sentinel = `{"date":"2024-01-01","content":"keep"}`
			if err := os.WriteFile(outside, []byte(sentinel), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := mgr.GetByDate(date); err == nil {
				t.Error("GetByDate accepted invalid date")
			}
			old := time.Now().Add(-time.Hour)
			note := &Scratchpad{Date: date, Content: "replacement", Modified: old}
			if err := mgr.Save(note); err == nil {
				t.Error("Save accepted invalid date")
			}
			if !note.Modified.Equal(old) {
				t.Error("invalid date changed Modified")
			}
			if err := mgr.Delete(date); err == nil {
				t.Error("Delete accepted invalid date")
			}
			data, err := os.ReadFile(outside)
			if err != nil || string(data) != sentinel {
				t.Errorf("invalid date modified a file outside storage: %q, %v", data, err)
			}
			entries, err := os.ReadDir(mgr.storageDir)
			if err != nil || len(entries) != 0 {
				t.Errorf("invalid date created files: %v, %v", entries, err)
			}
		})
	}
}

func TestGetByDateRejectsMismatchedStoredDate(t *testing.T) {
	mgr := setupTestManager(t)
	path := filepath.Join(mgr.storageDir, "2024-01-01.json")
	for _, date := range []string{"", "2024-01-02", "../outside"} {
		data, err := json.Marshal(Scratchpad{Date: date, Content: "keep"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := mgr.GetByDate("2024-01-01"); err == nil {
			t.Errorf("GetByDate accepted stored date %q for another filename", date)
		}
	}
}

func TestListDatesIgnoresInvalidNamesAndDirectories(t *testing.T) {
	mgr := setupTestManager(t)
	for _, name := range []string{
		"2024-02-29.json", "2024-01-01.json", "2023-02-29.json",
		"2024-1-01.json", "2024-01-01.new.json", "config.json", ".sp-abcd.tmp",
	} {
		if err := os.WriteFile(filepath.Join(mgr.storageDir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(mgr.storageDir, "2024-03-01.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := mgr.ListDates()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "2024-01-01,2024-02-29" {
		t.Errorf("ListDates = %v, want only valid note filenames", got)
	}
}
