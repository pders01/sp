package embeddedvim

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/privateedit"
)

func TestWasmReplaceUnicodeReplayAndUndo(t *testing.T) {
	u := openSyntheticUI(t, "abc🙂XYZ")
	uiType(t, u, "R🙂")
	if u.guestSnapshot.Mode != "replace" || !strings.HasPrefix(u.Frame().Lines[*u.Frame().StatusRow], "REPLACE") {
		t.Fatal("replace state did not reach renderer")
	}
	uiKey(t, u, "Escape")
	uiText(t, u, "🙂bc🙂XYZ")
	// Counted motion and overwrite replay both use source grapheme boundaries.
	uiType(t, u, "2l")
	if u.guestSnapshot.Cursor.Ch != 3 {
		t.Fatal("synthetic replay target motion failed")
	}
	uiType(t, u, ".")
	uiText(t, u, "🙂b🙂🙂XYZ")
	uiType(t, u, "u")
	uiText(t, u, "🙂bc🙂XYZ")
	uiType(t, u, "u")
	uiText(t, u, "abc🙂XYZ")
}

func TestWasmReplaceBackspaceAndInsertToggle(t *testing.T) {
	u := openSyntheticUI(t, "abcdef\r\nnext\x00")
	uiType(t, u, "RXY")
	uiKey(t, u, "Backspace") // Upstream CM6 behavior: move left, no restoration.
	uiText(t, u, "XYcdef\r\nnext\x00")
	uiType(t, u, "Z")
	uiKey(t, u, "Insert")
	uiType(t, u, "!")
	uiKey(t, u, "Insert")
	uiType(t, u, "Q")
	uiKey(t, u, "Escape")
	uiText(t, u, "XZ!Qdef\r\nnext\x00")
	uiType(t, u, "u")
	uiText(t, u, "abcdef\r\nnext\x00")
	uiKey(t, u, "CtrlR")
	uiText(t, u, "XZ!Qdef\r\nnext\x00")
}

func TestWasmReplaceCheckpointAndPinnedCancelKeepExactBytes(t *testing.T) {
	manager, pass, root := encryptedFixture(t)
	owner := encryptedEditor(t, manager)
	canary := addEncryptedDraft(t, owner)
	if _, err := owner.Type(context.Background(), "2G0Rxy"); err != nil {
		t.Fatal("synthetic replace input failed")
	}
	if _, err := owner.Key(context.Background(), "Save"); err != nil {
		t.Fatal("replace checkpoint request failed")
	}
	result, err := owner.Checkpoint(context.Background())
	saved := canary + "xy" + syntheticOriginal[2:]
	if err != nil || result.Closed || !result.Published || owner.Retained().Baseline != saved {
		t.Fatal("replace checkpoint failed or changed bytes")
	}
	if _, operationErr := owner.Type(context.Background(), "iQ"); operationErr != nil {
		t.Fatal("checkpoint left replacement active")
	}
	if _, operationErr := owner.Key(context.Background(), "Escape"); operationErr != nil {
		t.Fatal("resumed insert exit failed")
	}
	latest := canary + "xyQ" + syntheticOriginal[2:]
	if owner.Retained().Editor.Mirror.Text != latest {
		t.Fatal("checkpoint did not leave normal insertion semantics")
	}
	if _, operationErr := owner.Key(context.Background(), "Cancel"); operationErr != nil {
		t.Fatal("replace follow-up cancellation failed")
	}
	if _, operationErr := owner.Cancel(context.Background(), false); !errors.Is(operationErr, privateedit.ErrConfirmationRequired) {
		t.Fatal("replace edits bypassed cancellation consent")
	}
	manager.Lock()
	result, err = owner.Cancel(context.Background(), true)
	if err != nil || !result.Closed || len(result.Recoveries) == 0 {
		t.Fatal("replace recovery after relock failed")
	}
	if manager.Unlock(pass) != nil {
		t.Fatal("synthetic reauthentication failed")
	}
	note, err := manager.GetByDate(syntheticDate)
	if err != nil || note.Content != saved {
		t.Fatal("unsaved replace follow-up changed canonical content")
	}
	recovered, err := manager.ReadRecovery(result.Recoveries[0].Path)
	if err != nil || recovered.Content != latest {
		t.Fatal("replace recovery lost exact latest acknowledged bytes")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}
