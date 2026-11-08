package embeddedvim

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/privateedit"
)

func TestWasmZZFreezesExactDocumentAndDropsTrailingKeys(t *testing.T) {
	u := openSyntheticUI(t, "synthetic\r\n🙂")
	uiType(t, u, "iX")
	uiKey(t, u, "Escape")
	uiType(t, u, "Z")
	if u.Retained().Action != nil || u.Retained().Observed.Frozen {
		t.Fatal("single Z requested finish")
	}
	uiType(t, u, "ZiTAIL")
	a := uiAction(t, u, "finish")
	if a.Snapshot.Text != "Xsynthetic\r\n🙂" {
		t.Fatal("ZZ lost exact document or accepted trailing edits")
	}
}

func TestWasmZZPublishesEncryptedWholeDocumentWithSharedUpdates(t *testing.T) {
	manager, _, root := encryptedFixture(t)
	owner := encryptedEditor(t, manager)
	canary := addEncryptedDraft(t, owner)
	if manager.Append(syntheticDate, "Updates", "synthetic-shared", []string{"public concurrent fixture update"}) != nil {
		t.Fatal("synthetic shared update failed")
	}
	if _, err := owner.Type(context.Background(), "ZZ"); err != nil {
		t.Fatal("ZZ request failed")
	}
	result, err := owner.Finish(context.Background())
	if err != nil || !result.Closed || !result.Published {
		t.Fatal("ZZ encrypted save failed")
	}
	note, err := manager.GetByDate(syntheticDate)
	if err != nil || note.Content != canary+syntheticOriginal || !strings.Contains(note.Agent.Content, "public concurrent fixture update") {
		t.Fatal("ZZ changed bytes or lost shared updates")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}

func TestWasmZQCannotBypassRecoveryConsentOrPublishCanonicalNote(t *testing.T) {
	manager, pass, root := encryptedFixture(t)
	owner := encryptedEditor(t, manager)
	canary := addEncryptedDraft(t, owner)
	if _, err := owner.Type(context.Background(), "ZQ"); err != nil {
		t.Fatal("ZQ request failed")
	}
	if _, err := owner.Cancel(context.Background(), false); !errors.Is(err, privateedit.ErrConfirmationRequired) {
		t.Fatal("ZQ bypassed changed-draft recovery consent")
	}
	if err := owner.Resume(context.Background()); err != nil {
		t.Fatal("declining ZQ could not resume")
	}
	if owner.Retained().Editor.Mirror.Frozen {
		t.Fatal("declining ZQ retained stale close authority")
	}
	if _, err := owner.Type(context.Background(), "ZQ"); err != nil {
		t.Fatal("second ZQ request failed")
	}
	if _, err := owner.Cancel(context.Background(), false); !errors.Is(err, privateedit.ErrConfirmationRequired) {
		t.Fatal("second ZQ reused old consent")
	}
	manager.Lock()
	result, err := owner.Cancel(context.Background(), true)
	if err != nil || !result.Closed || result.Published || len(result.Recoveries) == 0 {
		t.Fatal("ZQ pinned encrypted recovery failed")
	}
	if manager.Unlock(pass) != nil {
		t.Fatal("synthetic reauthentication failed")
	}
	recovery, err := manager.ReadRecovery(result.Recoveries[0].Path)
	if err != nil || recovery.Content != canary+syntheticOriginal {
		t.Fatal("ZQ recovery did not preserve exact draft")
	}
	note, err := manager.GetByDate(syntheticDate)
	if err != nil || note.Content != syntheticOriginal {
		t.Fatal("ZQ published to canonical note")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}
