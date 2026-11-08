package privateedit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pders01/sp/internal/scratchpad"
)

func TestWorkflowMissingDateGenerationSubstitution(t *testing.T) {
	testMissingDateGeneration(t, storeWorkflow)
}

func testMissingDateGeneration(t *testing.T, open workflowFactory) {
	t.Helper()
	manager, root := privateManager(t)
	_, otherRoot := privateManager(t)
	const date = "2026-01-16"
	w := open(t, manager, date)
	metadataPath := filepath.Join(root, "privacy.json")
	originalMetadata := readFixture(t, metadataPath)
	if _, err := w.Set(context.Background(), w.Retained().Editor.Mirror, privateCanary); err != nil {
		t.Fatal(err)
	}
	manager.Lock()
	if err := os.WriteFile(metadataPath, readFixture(t, filepath.Join(otherRoot, "privacy.json")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Unlock([]byte(fixtureSecret)); err != nil {
		t.Fatal(err)
	}
	r, err := w.Finish(context.Background())
	if !errors.Is(err, scratchpad.ErrLocked) || r.Published || r.Closed || w.Retained().Editor.Mirror.Text != privateCanary {
		t.Fatal("missing date allowed a draft to publish under another authenticated key", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, date+".json")); !os.IsNotExist(statErr) {
		t.Fatal("stale edit created a daily note under the substituted key")
	}
	r, err = w.Cancel(context.Background(), true)
	if err != nil || !r.Closed || r.Published || len(r.Recoveries) != 1 || !r.Recoveries[0].Durable {
		t.Fatal("rejected stale edit could not recover using its original public pin", err)
	}
	// Restore only synthetic fixture metadata to verify the old-key recovery;
	// production cancellation never changes keys or rolls store metadata back.
	if writeErr := os.WriteFile(metadataPath, originalMetadata, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if unlockErr := manager.Unlock([]byte(fixtureSecret)); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	recovery, err := manager.ReadRecovery(r.Recoveries[0].Path)
	if err != nil || recovery.Content != privateCanary || recovery.Date != date {
		t.Fatal("old-generation encrypted recovery was redirected or changed", err)
	}
	assertNoPlaintextCanary(t, root)
}
