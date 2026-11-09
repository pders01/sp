package embeddedvim

import (
	"context"
	"strings"
	"testing"
)

func TestWasmEverydayPutCaretAndCRLFAppend(t *testing.T) {
	u := openSyntheticUI(t, "abc")
	uiType(t, u, "yl2lP")
	uiText(t, u, "abac")
	if u.guestSnapshot.Cursor != (Cursor{Line: 0, Ch: 2}) {
		t.Fatal("put-before caret missed inserted character")
	}
	uiType(t, u, "u")
	uiText(t, u, "abc")

	v := openSyntheticUI(t, "one\r\ntwo")
	uiType(t, v, "AX")
	uiKey(t, v, "Escape")
	uiText(t, v, "oneX\r\ntwo")
	if v.guestSnapshot.Cursor != (Cursor{Line: 0, Ch: 3}) {
		t.Fatal("CRLF append caret escaped source line")
	}
	uiType(t, v, "u")
	uiText(t, v, "one\r\ntwo")
}

func TestWasmEverydayPlainIndentAndCorrectionRepeat(t *testing.T) {
	u := openSyntheticUI(t, "  leftRIGHT\r\nlast")
	uiType(t, u, "6li")
	uiKey(t, u, "Enter")
	uiType(t, u, "X")
	uiKey(t, u, "Escape")
	uiText(t, u, "  left\n  XRIGHT\r\nlast")
	if u.guestSnapshot.Cursor != (Cursor{Line: 1, Ch: 2}) {
		t.Fatal("newline indent caret mismatch")
	}
	uiType(t, u, "u")
	uiText(t, u, "  leftRIGHT\r\nlast")

	v := openSyntheticUI(t, "seed")
	uiType(t, v, "A red")
	uiKey(t, v, "CtrlW")
	uiType(t, v, "X")
	uiKey(t, v, "Escape")
	uiText(t, v, "seed X")
	uiType(t, v, ".")
	uiText(t, v, "seed X X")
	uiType(t, v, "u")
	uiText(t, v, "seed X")
}

func TestEncryptedEverydayCorrectionRepeatsAcrossCheckpoint(t *testing.T) {
	manager, _, root := encryptedFixture(t)
	owner := encryptedEditor(t, manager)
	canary := addEncryptedDraft(t, owner)
	ctx := context.Background()
	if _, err := owner.Type(ctx, "2GA red"); err != nil {
		t.Fatal("synthetic append preparation failed")
	}
	if _, err := owner.Key(ctx, "CtrlW"); err != nil {
		t.Fatal("synthetic insert correction failed")
	}
	if _, err := owner.Type(ctx, "X"); err != nil {
		t.Fatal("synthetic corrected input failed")
	}
	if _, err := owner.Key(ctx, "Escape"); err != nil {
		t.Fatal("synthetic correction mode exit failed")
	}
	first := canary + strings.Replace(syntheticOriginal, "\r\n", " X\r\n", 1)
	if owner.Retained().Editor.Mirror.Text != first {
		t.Fatal("corrected append changed unrelated bytes")
	}
	typeEncryptedCommand(t, owner, "w")
	r, err := owner.Checkpoint(ctx)
	if err != nil || r.Closed || !r.Published {
		t.Fatal("corrected append checkpoint failed")
	}
	note, err := manager.GetByDate(syntheticDate)
	if err != nil || note.Content != first {
		t.Fatal("corrected checkpoint changed bytes")
	}
	if _, operationErr := owner.Type(ctx, "."); operationErr != nil {
		t.Fatal("correction repeat after checkpoint failed")
	}
	second := canary + strings.Replace(syntheticOriginal, "\r\n", " X X\r\n", 1)
	if owner.Retained().Editor.Mirror.Text != second {
		t.Fatal("checkpoint lost original insert correction record")
	}
	if _, operationErr := owner.Type(ctx, "u"); operationErr != nil || owner.Retained().Editor.Mirror.Text != first {
		t.Fatal("correction repeat undo failed")
	}
	if _, operationErr := owner.Key(ctx, "Redo"); operationErr != nil || owner.Retained().Editor.Mirror.Text != second {
		t.Fatal("correction repeat redo failed")
	}
	typeEncryptedCommand(t, owner, "wq")
	r, err = owner.Finish(ctx)
	if err != nil || !r.Closed {
		t.Fatal("corrected repeat finish failed")
	}
	note, err = manager.GetByDate(syntheticDate)
	if err != nil || note.Content != second {
		t.Fatal("corrected repeat final bytes differ")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}
