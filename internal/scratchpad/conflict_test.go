package scratchpad

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveRejectsStaleRevision(t *testing.T) {
	mgr := setupTestManager(t)
	note := &Scratchpad{Date: "2024-01-01", Content: "original"}
	if err := mgr.Save(note); err != nil {
		t.Fatal(err)
	}
	stale, err := mgr.GetByDate(note.Date)
	if err != nil {
		t.Fatal(err)
	}
	previousModified := stale.Modified
	note.Content = "other writer"
	if saveErr := mgr.Save(note); saveErr != nil {
		t.Fatal(saveErr)
	}
	stale.Content = "my edits"
	if saveErr := mgr.Save(stale); !errors.Is(saveErr, ErrConflict) {
		t.Fatalf("stale Save = %v, want ErrConflict", saveErr)
	}
	if !stale.Modified.Equal(previousModified) || stale.Content != "my edits" {
		t.Error("conflict mutated the unsaved note")
	}
	got, err := mgr.GetByDate(note.Date)
	if err != nil || got.Content != "other writer" {
		t.Fatalf("conflict overwrote newer note: %+v, %v", got, err)
	}
	assertNoSaveTemps(t, mgr)
}

func TestSaveRejectsConcurrentCreation(t *testing.T) {
	mgr := setupTestManager(t)
	first, err := mgr.GetByDate("2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	second, err := mgr.GetByDate(first.Date)
	if err != nil {
		t.Fatal(err)
	}
	first.Content = "created first"
	if err := mgr.Save(first); err != nil {
		t.Fatal(err)
	}
	second.Content = "created second"
	if err := mgr.Save(second); !errors.Is(err, ErrConflict) {
		t.Fatalf("second creation = %v, want ErrConflict", err)
	}
}

func TestSaveDoesNotResurrectDeletedNote(t *testing.T) {
	mgr := setupTestManager(t)
	note := &Scratchpad{Date: "2024-01-01", Content: "original"}
	if err := mgr.Save(note); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Delete(note.Date); err != nil {
		t.Fatal(err)
	}
	note.Content = "stale edits"
	if err := mgr.Save(note); !errors.Is(err, ErrConflict) {
		t.Fatalf("Save after deletion = %v, want ErrConflict", err)
	}
	if _, err := os.Stat(filepath.Join(mgr.storageDir, note.Date+".json")); !os.IsNotExist(err) {
		t.Errorf("deleted note was recreated: %v", err)
	}
}

func TestConcurrentSavesHaveOneWinner(t *testing.T) {
	mgr := setupTestManager(t)
	base := &Scratchpad{Date: "2024-01-01", Content: "original"}
	if err := mgr.Save(base); err != nil {
		t.Fatal(err)
	}
	first, err := mgr.GetByDate(base.Date)
	if err != nil {
		t.Fatal(err)
	}
	second, err := mgr.GetByDate(base.Date)
	if err != nil {
		t.Fatal(err)
	}
	first.Content, second.Content = "first writer", "second writer"
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, note := range []*Scratchpad{first, second} {
		go func() {
			<-start
			results <- mgr.Save(note)
		}()
	}
	close(start)
	var saved, conflicted int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			saved++
		case errors.Is(err, ErrConflict):
			conflicted++
		default:
			t.Errorf("unexpected save error: %v", err)
		}
	}
	if saved != 1 || conflicted != 1 {
		t.Errorf("saved = %d, conflicted = %d; want one of each", saved, conflicted)
	}
	assertNoSaveTemps(t, mgr)
}
