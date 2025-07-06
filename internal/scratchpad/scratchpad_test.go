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
