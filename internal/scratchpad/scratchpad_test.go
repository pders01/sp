package scratchpad

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pders01/sp/internal/templates"
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

func TestApplyTemplateSectionsAppendsWithoutOverwriting(t *testing.T) {
	mgr := setupTestManager(t)
	original := &Scratchpad{Date: "2026-07-19", Content: "# Existing\n", Created: time.Now()}
	if err := mgr.Save(original); err != nil {
		t.Fatal(err)
	}
	sections := []templates.Section{{ID: "tasks", Title: "Tasks", Body: "- [ ] one"}}

	got, err := mgr.ApplyTemplateSections(original.Date, sections)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Existing\n\n## Tasks\n\n- [ ] one\n"
	if got.Content != want {
		t.Errorf("content = %q, want %q", got.Content, want)
	}
	if len(got.AppliedTemplates) != 1 || got.AppliedTemplates[0] != "tasks" {
		t.Errorf("applied templates = %v", got.AppliedTemplates)
	}

	got, err = mgr.ApplyTemplateSections(original.Date, sections)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != want {
		t.Errorf("reapplying changed content to %q", got.Content)
	}

	// If the user removes the generated section in their editor, an explicit
	// force selection can restore it without duplicating metadata.
	got.Content = "# Existing\n"
	if saveErr := mgr.Save(got); saveErr != nil {
		t.Fatal(saveErr)
	}
	sections[0].Force = true
	got, err = mgr.ApplyTemplateSections(original.Date, sections)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != want {
		t.Errorf("forced reapply content = %q, want %q", got.Content, want)
	}
	if len(got.AppliedTemplates) != 1 {
		t.Errorf("forced reapply duplicated metadata: %v", got.AppliedTemplates)
	}
}

func TestApplyTemplateSectionsPreservesExistingTrailingWhitespace(t *testing.T) {
	mgr := setupTestManager(t)
	original := &Scratchpad{
		Date:    "2026-07-20",
		Content: "existing content  \n\n\n",
		Created: time.Now(),
	}
	if err := mgr.Save(original); err != nil {
		t.Fatal(err)
	}
	got, err := mgr.ApplyTemplateSections(original.Date, []templates.Section{{
		ID: "notes", Title: "Notes", Body: "body",
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := original.Content + "## Notes\n\nbody\n"
	if got.Content != want {
		t.Errorf("content = %q, want byte-preserving append %q", got.Content, want)
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
