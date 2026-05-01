package scratchpad

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestManager(t *testing.T) *Manager {
	dir := t.TempDir()
	mgr := &Manager{storageDir: dir}
	return mgr
}

func TestNewManager(t *testing.T) {
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr.storageDir == "" {
		t.Error("storageDir should not be empty")
	}
}

func TestSaveAndGetByDate(t *testing.T) {
	mgr := setupTestManager(t)
	date := "2024-01-01"
	sp := &Scratchpad{
		Date:     date,
		Content:  "Hello, world!",
		Created:  time.Now(),
		Modified: time.Now(),
	}
	if err := mgr.Save(sp); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := mgr.GetByDate(date)
	if err != nil {
		t.Fatalf("GetByDate failed: %v", err)
	}
	if loaded.Content != sp.Content {
		t.Errorf("expected content %q, got %q", sp.Content, loaded.Content)
	}
}

func TestGetToday(t *testing.T) {
	mgr := setupTestManager(t)
	sp, err := mgr.GetToday()
	if err != nil {
		t.Fatalf("GetToday failed: %v", err)
	}
	if sp.Date != time.Now().Format("2006-01-02") {
		t.Errorf("expected today's date, got %q", sp.Date)
	}
}

func TestListDates(t *testing.T) {
	mgr := setupTestManager(t)
	dates := []string{"2024-01-01", "2024-01-02"}
	for _, d := range dates {
		sp := &Scratchpad{Date: d, Content: d, Created: time.Now(), Modified: time.Now()}
		if err := mgr.Save(sp); err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	}
	found, err := mgr.ListDates()
	if err != nil {
		t.Fatalf("ListDates failed: %v", err)
	}
	if len(found) != len(dates) {
		t.Errorf("expected %d dates, got %d", len(dates), len(found))
	}
}

func TestGetByDateMissingReturnsBlank(t *testing.T) {
	mgr := setupTestManager(t)
	sp, err := mgr.GetByDate("2099-12-31")
	if err != nil {
		t.Fatalf("GetByDate on missing date returned err: %v", err)
	}
	if sp.Date != "2099-12-31" {
		t.Errorf("date = %q, want %q", sp.Date, "2099-12-31")
	}
	if sp.Content != "" {
		t.Errorf("content = %q, want empty", sp.Content)
	}
}

func TestGetByDateRejectsCorruptJSON(t *testing.T) {
	mgr := setupTestManager(t)
	path := filepath.Join(mgr.storageDir, "2024-02-29.json")
	if err := os.WriteFile(path, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if _, err := mgr.GetByDate("2024-02-29"); err == nil {
		t.Error("expected parse error on corrupt JSON, got nil")
	}
}

func TestListDatesIgnoresNonJSON(t *testing.T) {
	mgr := setupTestManager(t)
	if err := os.WriteFile(filepath.Join(mgr.storageDir, "2024-03-01.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mgr.storageDir, "stray.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}
	dates, err := mgr.ListDates()
	if err != nil {
		t.Fatalf("ListDates: %v", err)
	}
	if len(dates) != 1 || dates[0] != "2024-03-01" {
		t.Errorf("dates = %v, want [2024-03-01]", dates)
	}
}

func TestListDatesMissingDirReturnsError(t *testing.T) {
	mgr := &Manager{storageDir: filepath.Join(t.TempDir(), "does-not-exist")}
	if _, err := mgr.ListDates(); err == nil {
		t.Error("expected error reading missing dir, got nil")
	}
}

func TestSaveUpdatesModified(t *testing.T) {
	mgr := setupTestManager(t)
	old := time.Now().Add(-time.Hour)
	sp := &Scratchpad{Date: "2024-04-04", Content: "x", Created: old, Modified: old}
	if err := mgr.Save(sp); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !sp.Modified.After(old) {
		t.Errorf("Modified = %v, want > %v", sp.Modified, old)
	}
}

func TestDelete(t *testing.T) {
	mgr := setupTestManager(t)
	date := "2024-01-01"
	sp := &Scratchpad{Date: date, Content: "test", Created: time.Now(), Modified: time.Now()}
	if err := mgr.Save(sp); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := mgr.Delete(date); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	filename := filepath.Join(mgr.storageDir, date+".json")
	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Errorf("file should be deleted, but exists")
	}
}
