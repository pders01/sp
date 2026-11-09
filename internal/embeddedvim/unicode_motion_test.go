package embeddedvim

import (
	"context"
	"testing"
)

func TestWasmCountedHorizontalMotionUsesGraphemes(t *testing.T) {
	text := "a😀e\u0301👩‍💻z\r\nlast"
	u := openSyntheticUI(t, text)
	uiType(t, u, "2l")
	if u.guestSnapshot.Cursor != (Cursor{Line: 0, Ch: 3}) || u.Frame().CursorColumn != 5 {
		t.Fatal("counted right motion confused source units and display cells")
	}
	uiType(t, u, "2h")
	if u.guestSnapshot.Cursor != (Cursor{Line: 0, Ch: 0}) {
		t.Fatal("counted left motion did not reverse graphemes")
	}
	uiType(t, u, "999l")
	if u.guestSnapshot.Cursor != (Cursor{Line: 0, Ch: 10}) {
		t.Fatal("right motion entered last grapheme/CRLF or wrapped")
	}
	uiType(t, u, "999h")
	if u.guestSnapshot.Cursor != (Cursor{Line: 0, Ch: 0}) {
		t.Fatal("left motion escaped source line")
	}
	uiText(t, u, text)
	uiType(t, u, "2x")
	uiText(t, u, "e\u0301👩‍💻z\r\nlast")
	uiType(t, u, "p")
	uiText(t, u, "e\u0301a😀👩‍💻z\r\nlast")
	uiType(t, u, "u")
	uiText(t, u, "e\u0301👩‍💻z\r\nlast")
	uiType(t, u, "u")
	uiText(t, u, text)
	uiType(t, u, "y2lp")
	uiText(t, u, "aa😀😀e\u0301👩‍💻z\r\nlast")
	uiType(t, u, "u")
	uiText(t, u, text)
	uiType(t, u, "v2l")
	if u.guestSnapshot.Cursor.Ch != 3 || len(u.Frame().Selections) == 0 {
		t.Fatal("visual count lost source cursor or highlight")
	}
	uiKey(t, u, "Escape")
	uiText(t, u, text)
}

func TestEncryptedCountedUnicodeDeleteAndUndoCheckpoint(t *testing.T) {
	manager, _, root := encryptedFixture(t)
	owner := encryptedEditor(t, manager)
	canary := addEncryptedDraft(t, owner)
	if _, err := owner.Type(context.Background(), "3G07l2x"); err != nil {
		t.Fatal("counted Unicode delete failed")
	}
	expected := canary + "SYNTHETIC fixture only\r\nsecond \n"
	if owner.Retained().Editor.Mirror.Text != expected {
		t.Fatal("counted deletion split or overconsumed Unicode")
	}
	typeEncryptedCommand(t, owner, "w")
	result, err := owner.Checkpoint(context.Background())
	if err != nil || result.Closed || !result.Published {
		t.Fatal("Unicode deletion checkpoint failed")
	}
	note, err := manager.GetByDate(syntheticDate)
	if err != nil || note.Content != expected {
		t.Fatal("Unicode checkpoint changed bytes")
	}
	if _, operationErr := owner.Type(context.Background(), "u"); operationErr != nil {
		t.Fatal("Unicode delete history lost across checkpoint")
	}
	if owner.Retained().Editor.Mirror.Text != canary+syntheticOriginal {
		t.Fatal("Unicode undo failed exact restoration")
	}
	typeEncryptedCommand(t, owner, "wq")
	if result, err = owner.Finish(context.Background()); err != nil || !result.Closed {
		t.Fatal("Unicode undo finish failed")
	}
	note, err = manager.GetByDate(syntheticDate)
	if err != nil || note.Content != canary+syntheticOriginal {
		t.Fatal("Unicode restored bytes not saved")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}
