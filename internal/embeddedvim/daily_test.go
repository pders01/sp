package embeddedvim

import (
	"context"
	"strings"
	"testing"
)

func TestWasmDailyLineBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, text, keys, expected string }{
		{"join-space", "one \n  two", "J", "one two"},
		{"join-paren", "(one\n)", "J", "(one)"},
		{"join-crlf", "one\r\n  two\r\nlast", "J", "one two\r\nlast"},
		{"put-crlf", "one\r\ntwo", "yyp", "one\r\none\r\ntwo"},
		{"delete-final-crlf", "one\r\ntwo", "Gdd", "one"},
		{"delete-final-lf", "one\ntwo", "Gdd", "one"},
		{"delete-sole", "one", "dd", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := openSyntheticUI(t, tc.text)
			uiType(t, u, tc.keys)
			uiText(t, u, tc.expected)
			uiType(t, u, "u")
			uiText(t, u, tc.text)
			uiKey(t, u, "Redo")
			uiText(t, u, tc.expected)
		})
	}
}

func TestWasmDailyOpenAboveAndVisualLineChange(t *testing.T) {
	u := openSyntheticUI(t, "zero\n  one\r\nlast")
	uiType(t, u, "jOX")
	uiKey(t, u, "Escape")
	uiText(t, u, "zero\n  X\n  one\r\nlast")
	if u.guestSnapshot.Cursor != (Cursor{Line: 1, Ch: 2}) {
		t.Fatal("open-above caret/indent mismatch")
	}
	uiType(t, u, "u")
	uiText(t, u, "zero\n  one\r\nlast")
	v := openSyntheticUI(t, "one\r\ntwo\r\nlast")
	uiType(t, v, "jVcX")
	uiKey(t, v, "Escape")
	uiText(t, v, "one\r\nX\r\nlast")
	uiType(t, v, "u")
	uiText(t, v, "one\r\ntwo\r\nlast")
}

func TestEncryptedDailyLinewisePutAndUndo(t *testing.T) {
	manager, _, root := encryptedFixture(t)
	owner := encryptedEditor(t, manager)
	canary := addEncryptedDraft(t, owner)
	ctx := context.Background()
	if _, err := owner.Type(ctx, "2Gyyp"); err != nil {
		t.Fatal("synthetic linewise put failed")
	}
	expected := canary + strings.SplitAfter(syntheticOriginal, "\n")[0] + syntheticOriginal
	if owner.Retained().Editor.Mirror.Text != expected {
		t.Fatal("linewise put changed source boundaries")
	}
	typeEncryptedCommand(t, owner, "w")
	r, err := owner.Checkpoint(ctx)
	if err != nil || r.Closed || !r.Published {
		t.Fatal("linewise checkpoint failed")
	}
	note, err := manager.GetByDate(syntheticDate)
	if err != nil || note.Content != expected {
		t.Fatal("linewise checkpoint bytes differ")
	}
	if _, err = owner.Type(ctx, "u"); err != nil {
		t.Fatal("linewise undo after checkpoint failed")
	}
	if owner.Retained().Editor.Mirror.Text != canary+syntheticOriginal {
		t.Fatal("linewise undo failed exact restoration")
	}
	typeEncryptedCommand(t, owner, "wq")
	r, err = owner.Finish(ctx)
	if err != nil || !r.Closed {
		t.Fatal("linewise restored finish failed")
	}
	note, err = manager.GetByDate(syntheticDate)
	if err != nil || note.Content != canary+syntheticOriginal {
		t.Fatal("restored linewise save bytes differ")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}
