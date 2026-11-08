package embeddedvim

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/pders01/sp/internal/editorcore"
)

var _ editorcore.UI = (*UI)(nil)

func openSyntheticUI(t *testing.T, text string) *UI {
	t.Helper()
	wasm, bundle := proofAssets(t)
	u, err := OpenUI(context.Background(), wasm, bundle, editorcore.Options{DocumentID: "synthetic-document", Width: 32, Height: 8}, text)
	if err != nil || u == nil {
		t.Fatal("ui: synthetic open failed")
	}
	t.Cleanup(func() {
		if _, err := u.Discard(context.Background(), true); err != nil {
			t.Error("ui: synthetic cleanup failed")
		}
	})
	return u
}
func uiType(t *testing.T, u *UI, text string) {
	t.Helper()
	n, err := u.Type(context.Background(), text)
	if err != nil || n != len(text) {
		t.Fatal("ui: physical input failed")
	}
	r := u.InputReceipt()
	if !r.Known || r.RequestedBytes != len(text) || r.AcceptedBytes != len(text) {
		t.Fatal("ui: completed receipt mismatch")
	}
}
func uiKey(t *testing.T, u *UI, name string) {
	t.Helper()
	if _, err := u.Key(context.Background(), name); err != nil {
		t.Fatal("ui: named key failed")
	}
}
func uiText(t *testing.T, u *UI, expected string) {
	t.Helper()
	if u.Retained().Observed.Text != expected {
		t.Fatal("ui: retained synthetic text mismatch")
	}
}
func uiAction(t *testing.T, u *UI, kind string) editorcore.Action {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a, err := u.WaitAction(ctx)
	r := u.Retained()
	if err != nil || a.Kind != kind || !a.Snapshot.Frozen || a.Snapshot != r.Observed || r.Action == nil || *r.Action != a || r.LatestUnknown || r.Closed || r.Fault != nil {
		t.Fatal("ui: exact healthy guest action required")
	}
	return a
}

func TestUIPasteEditsPromptsWithoutFaultOrLifecycleExecution(t *testing.T) {
	u := openSyntheticUI(t, "head\nneedle")
	uiType(t, u, "/")
	if _, err := u.InsertText(context.Background(), "needle"); err != nil {
		t.Fatal("ui: search paste faulted")
	}
	uiText(t, u, "head\nneedle")
	if u.Retained().Fault != nil || u.Retained().Action != nil {
		t.Fatal("ui: prompt paste changed ownership")
	}
	uiKey(t, u, "Enter")
	if u.session.Snapshot().Cursor.Line != 1 {
		t.Fatal("ui: pasted search did not execute on submission")
	}
	uiType(t, u, ":")
	if _, err := u.InsertText(context.Background(), "wq"); err != nil || u.Retained().Action != nil {
		t.Fatal("ui: Ex paste executed or faulted")
	}
	uiKey(t, u, "Escape")
	uiType(t, u, "iX")
	uiText(t, u, "head\nXneedle")
}

func TestUIStartupFailureRetainsOwner(t *testing.T) {
	for _, options := range []editorcore.Options{{DocumentID: "synthetic"}, {DocumentID: "synthetic", Width: -1}} {
		u, err := OpenUI(context.Background(), nil, []byte("void 0"), options, "original synthetic copy")
		if err == nil || u == nil {
			t.Fatal("ui: startup failure abandoned owner")
		}
		r := u.Retained()
		if r.Closed || r.Observed.Text != "original synthetic copy" || r.Observed.SessionID == "" || r.Observed.Revision == 0 || r.Fault == nil {
			t.Fatal("ui: startup failure lost retained baseline")
		}
		if _, err := u.Discard(context.Background(), false); !errors.Is(err, editorcore.ErrLossConsent) {
			t.Fatal("ui: startup discard lacked consent")
		}
		if _, err := u.Discard(context.Background(), true); err != nil || !u.Retained().Closed {
			t.Fatal("ui: startup owner could not be explicitly discarded")
		}
	}
}

func TestUIPhysicalScalarsAndLiteralTokenText(t *testing.T) {
	u := openSyntheticUI(t, "")
	uiType(t, u, "iA😀界<CR><F2>")
	uiText(t, u, "A😀界<CR><F2>")
	if u.Retained().Action != nil {
		t.Fatal("ui: physical token-like text requested an action")
	}
	for _, text := range []string{"\n", "\r", "\t", "\x1b", "\x00", "\x7f", string([]byte{0xff})} {
		before := u.Retained().Observed
		if _, err := u.Type(context.Background(), text); !errors.Is(err, editorcore.ErrUnsupported) || u.Retained().Observed != before {
			t.Fatal("ui: physical control or invalid UTF-8 accepted")
		}
		r := u.InputReceipt()
		if !r.Known || r.AcceptedBytes != 0 {
			t.Fatal("ui: synchronous rejection receipt mismatch")
		}
	}
	uiKey(t, u, "Escape")
}

func TestUIKeyNames(t *testing.T) {
	cases := map[string]string{
		"Escape": "<Esc>", "Enter": "<CR>", "Backspace": "<BS>", "Delete": "<Del>", "Tab": "<Tab>",
		"Left": "<Left>", "Right": "<Right>", "Up": "<Up>", "Down": "<Down>",
		"Home": "<Home>", "End": "<End>", "PageUp": "<PageUp>", "PageDown": "<PageDown>", "Redo": "<C-r>", "Insert": "<Ins>",
	}
	for name, want := range cases {
		token, action := keyToken(name)
		if token != want || action != "" {
			t.Fatal("ui: named key mapping mismatch")
		}
	}
	for r := 'A'; r <= 'Z'; r++ {
		token, action := keyToken("Ctrl" + string(r))
		if token != "<C-"+strings.ToLower(string(r))+">" || action != "" {
			t.Fatal("ui: control key mapping mismatch")
		}
	}
	for _, name := range []string{"Finish", "F2", "Cancel", "F3"} {
		token, action := keyToken(name)
		if token != "" || (action != "finish" && action != "cancel") {
			t.Fatal("ui: action key mapping mismatch")
		}
	}
	for _, name := range []string{"<CR>", "<F2>", "CtrlAA", "ctrlr", "Unknown", ""} {
		token, action := keyToken(name)
		if token != "" || action != "" {
			t.Fatal("ui: arbitrary key token accepted")
		}
	}
}

func TestUITypedExActions(t *testing.T) {
	for _, tc := range []struct{ command, kind string }{{":wq", "finish"}, {":q", "cancel"}} {
		u := openSyntheticUI(t, "synthetic baseline")
		uiType(t, u, tc.command)
		if u.Retained().Action != nil {
			t.Fatal("ui: unsubmitted prompt authorized close")
		}
		uiKey(t, u, "Enter")
		a := uiAction(t, u, tc.kind)
		if a.Snapshot.Text != "synthetic baseline" || a.Snapshot.DocumentID != "synthetic-document" {
			t.Fatal("ui: guest action changed document identity or bytes")
		}
		if err := u.Close(context.Background(), a.Snapshot); err != nil || !u.Retained().Closed {
			t.Fatal("ui: exact healthy frozen close failed")
		}
	}
}

func TestUIActionKeysFreezeAllModes(t *testing.T) {
	for _, modeKeys := range []string{"", "i", "v", ":pending", "/pending"} {
		u := openSyntheticUI(t, "synthetic baseline")
		for _, name := range []string{"F2", "F3", "Finish", "Cancel"} {
			if modeKeys != "" {
				uiType(t, u, modeKeys)
			}
			uiKey(t, u, name)
			kind := "finish"
			if name == "F3" || name == "Cancel" {
				kind = "cancel"
			}
			a := uiAction(t, u, kind)
			if a.Snapshot.Text != "synthetic baseline" {
				t.Fatal("ui: freeze changed document text")
			}
			if _, err := u.Type(context.Background(), "iTAIL"); !errors.Is(err, editorcore.ErrFrozen) {
				t.Fatal("ui: frozen guest accepted normal input")
			}
			if _, err := u.InsertText(context.Background(), "TAIL"); !errors.Is(err, editorcore.ErrFrozen) {
				t.Fatal("ui: frozen guest accepted paste")
			}
			if _, err := u.Resume(context.Background(), a.Snapshot); err != nil || u.Retained().Action != nil || u.Retained().Observed.Frozen {
				t.Fatal("ui: resume did not revoke exact action")
			}
			if err := u.Close(context.Background(), a.Snapshot); err == nil {
				t.Fatal("ui: stale action authorized close after resume")
			}
		}
	}
}

func TestUIGuestBatchDropsTailAndResumeKeepsHistory(t *testing.T) {
	u := openSyntheticUI(t, "base")
	uiType(t, u, "iNEW")
	uiKey(t, u, "Escape")
	uiText(t, u, "NEWbase")
	// Exercise the same completed-input path with a single guest batch whose
	// physical Enter is inside the batch. The UI never parses Ex itself.
	u.mu.Lock()
	u.beginInputLocked(12)
	s, err := u.session.Keys(context.Background(), ":", "w", "q", "<CR>", "i", "T", "A", "I", "L")
	_, err = u.completeInputLocked(s, err, "")
	u.mu.Unlock()
	if err != nil {
		t.Fatal("ui: guest batch failed")
	}
	a := uiAction(t, u, "finish")
	uiText(t, u, "NEWbase")
	if r := u.InputReceipt(); !r.Known || r.AcceptedBytes != r.RequestedBytes {
		t.Fatal("ui: dropped batch tail remained replayable")
	}
	if _, err := u.Resume(context.Background(), a.Snapshot); err != nil {
		t.Fatal("ui: batch action resume failed")
	}
	uiType(t, u, "u")
	uiText(t, u, "base")
	uiKey(t, u, "Redo")
	uiText(t, u, "NEWbase")
	uiKey(t, u, "Finish")
	newAction := uiAction(t, u, "finish")
	if newAction.Snapshot.Revision <= a.Snapshot.Revision {
		t.Fatal("ui: new action reused revoked revision")
	}
	if err := u.Close(context.Background(), a.Snapshot); err == nil || u.Retained().Closed {
		t.Fatal("ui: old equal-text action closed a later generation")
	}
}

func TestUILiteralPasteCannotRequestAction(t *testing.T) {
	const pasted = ":wq\n:q\n<F2><F3>\x1b\x00😀"
	u := openSyntheticUI(t, "")
	s, err := u.InsertText(context.Background(), pasted)
	if err != nil || s.Text != pasted || s.Frozen || u.Retained().Action != nil {
		t.Fatal("ui: literal paste was interpreted or changed")
	}
	r := u.InputReceipt()
	if !r.Known || r.AcceptedBytes != len(pasted) {
		t.Fatal("ui: literal paste receipt mismatch")
	}
	if err := u.Close(context.Background(), s); !errors.Is(err, editorcore.ErrNotFrozen) {
		t.Fatal("ui: paste authorized close")
	}
	f := u.Frame()
	if f.ANSI() == "" {
		t.Fatal("ui: pasted controls produced unsafe frame")
	}
	for _, line := range f.Lines {
		if !editorcore.SafeDisplayText(line) {
			t.Fatal("ui: raw paste controls reached frame")
		}
	}
}

// Only synthetic test code can corrupt the guest namespace. This does not
// expose EvaluateForTest through the editor interface or accept raw errors.
func corruptUIGuest(t *testing.T, u *UI) {
	t.Helper()
	if _, err := u.session.EvaluateForTest(context.Background(), "(globalThis.proof = null, true)"); err != nil {
		t.Fatal("ui: synthetic fault setup failed")
	}
}
func TestUIFaultRetainsLastAcknowledgedCopy(t *testing.T) {
	u := openSyntheticUI(t, "base")
	uiType(t, u, "iACK")
	before := u.Retained().Observed
	corruptUIGuest(t, u)
	if _, err := u.Type(context.Background(), "UNKNOWN"); !errors.Is(err, editorcore.ErrRemote) {
		t.Fatal("ui: failed guest was not classified")
	}
	r, receipt := u.Retained(), u.InputReceipt()
	if r.Observed.Text != before.Text || r.Closed || !r.LatestUnknown || r.Fault == nil || r.Action != nil || receipt.Known || receipt.AcceptedBytes != 0 {
		t.Fatal("ui: failed input lost ownership or claimed acknowledgement")
	}
	if err := u.Resolve(context.Background()); !errors.Is(err, editorcore.ErrRemote) {
		t.Fatal("ui: destroyed guest resolved as healthy")
	}
	if _, err := u.Key(context.Background(), "Finish"); err == nil {
		t.Fatal("ui: unknown latest copy authorized finish")
	}
	if u.InputReceipt() != receipt {
		t.Fatal("ui: rejected input erased the uncertain receipt")
	}
	if _, err := u.WaitAction(context.Background()); err == nil {
		t.Fatal("ui: faulted guest returned an action")
	}
	if err := u.Close(context.Background(), before); err == nil || u.Retained().Closed {
		t.Fatal("ui: faulted guest dropped sole owner")
	}
	if _, err := u.Discard(context.Background(), false); !errors.Is(err, editorcore.ErrLossConsent) {
		t.Fatal("ui: failed guest discarded without consent")
	}
	if _, err := u.Discard(context.Background(), true); err != nil || !u.Retained().Closed || u.Retained().Observed.Text != before.Text {
		t.Fatal("ui: explicit known-copy discard failed")
	}
}

func TestUIFailedResumeCannotResurrectOldAction(t *testing.T) {
	u := openSyntheticUI(t, "retained baseline")
	uiKey(t, u, "Finish")
	a := uiAction(t, u, "finish")
	corruptUIGuest(t, u)
	if _, err := u.Resume(context.Background(), a.Snapshot); !errors.Is(err, editorcore.ErrRemote) {
		t.Fatal("ui: failed resume was not classified")
	}
	r := u.Retained()
	if r.Action != nil || r.Observed.Frozen || !r.LatestUnknown || r.Closed || r.Observed.Text != a.Snapshot.Text || r.Observed.Revision <= a.Snapshot.Revision {
		t.Fatal("ui: failed resume retained stale close authority")
	}
	if err := u.Close(context.Background(), a.Snapshot); err == nil || u.Retained().Closed {
		t.Fatal("ui: failed resume allowed stale close")
	}
}

func TestUIOwnershipAndFrameCopies(t *testing.T) {
	u := openSyntheticUI(t, "synthetic baseline")
	other := openSyntheticUI(t, "synthetic baseline")
	uiKey(t, u, "Finish")
	uiKey(t, other, "Finish")
	a, b := uiAction(t, u, "finish"), uiAction(t, other, "finish")
	if a.Snapshot.SessionID == b.Snapshot.SessionID {
		t.Fatal("ui: sessions reused ownership identity")
	}
	if err := u.Close(context.Background(), b.Snapshot); err == nil {
		t.Fatal("ui: foreign snapshot authorized close")
	}
	r := u.Retained()
	r.Action.Kind = "cancel"
	r.Action.Snapshot.Text = "changed caller copy"
	f := u.Frame()
	f.Lines[0] = "changed caller frame"
	if f.StatusRow == nil {
		t.Fatal("ui: status metadata missing")
	}
	*f.StatusRow = 0
	if u.Frame().StatusRow == nil || *u.Frame().StatusRow == 0 {
		t.Fatal("ui: returned status slot aliases private state")
	}
	if u.Retained().Action.Kind != "finish" || u.Frame().Lines[0] == f.Lines[0] {
		t.Fatal("ui: returned mutable data aliases private state")
	}
	changed := u.Changed()
	if err := u.Resize(context.Background(), 12, 4); err != nil {
		t.Fatal("ui: frozen resize failed")
	}
	select {
	case <-changed:
	default:
		t.Fatal("ui: resize did not broadcast")
	}
	if u.Retained().Observed != a.Snapshot || *u.Retained().Action != a {
		t.Fatal("ui: display resize invalidated exact action")
	}
	if err := u.Resize(context.Background(), 0, 4); !errors.Is(err, editorcore.ErrUnsupported) {
		t.Fatal("ui: invalid geometry reached guest")
	}
	if f := u.Frame(); f.Width != 12 || f.Height != 4 || f.ANSI() == "" {
		t.Fatal("ui: resized frame is unsafe")
	}
}

func TestUIContextCancellationAndNoFrameAuthority(t *testing.T) {
	u := openSyntheticUI(t, "base")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := u.Retained().Observed
	if _, err := u.Type(ctx, "iLOST"); !errors.Is(err, context.Canceled) || u.Retained().Observed != before {
		t.Fatal("ui: pre-cancelled input reached guest")
	}
	if _, err := u.WaitAction(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("ui: cancelled action waiter did not stop")
	}
	if err := u.Resize(ctx, 20, 8); !errors.Is(err, context.Canceled) {
		t.Fatal("ui: cancelled resize reached guest")
	}
	if _, err := u.Discard(ctx, true); !errors.Is(err, context.Canceled) || u.Retained().Closed {
		t.Fatal("ui: cancelled discard lost owner")
	}
	// A plausible display is not an input-ordered action.
	u.mu.Lock()
	u.frame.Lines[0] = "finish"
	u.mu.Unlock()
	if err := u.Close(context.Background(), before); !errors.Is(err, editorcore.ErrNotFrozen) {
		t.Fatal("ui: frame authorized close")
	}
	uiType(t, u, ":q")
	uiKey(t, u, "Enter")
	a := uiAction(t, u, "cancel")
	if _, err := u.Resume(ctx, a.Snapshot); !errors.Is(err, context.Canceled) || u.Retained().Action == nil {
		t.Fatal("ui: pre-cancelled resume changed guest ownership")
	}
	if err := u.Close(ctx, a.Snapshot); !errors.Is(err, context.Canceled) || u.Retained().Closed {
		t.Fatal("ui: pre-cancelled close lost owner")
	}
}

func TestUIWaitersAndSerializedInput(t *testing.T) {
	u := openSyntheticUI(t, "")
	uiType(t, u, "i")
	var writers sync.WaitGroup
	for range 8 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			if _, err := u.Type(context.Background(), "界"); err != nil {
				t.Error("ui: serialized input failed")
			}
		}()
	}
	writers.Wait()
	if got := u.Retained().Observed.Text; !utf8.ValidString(got) || got != strings.Repeat("界", 8) {
		t.Fatal("ui: serialized scalar input lost bytes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var waiters sync.WaitGroup
	for range 4 {
		waiters.Add(1)
		go func() {
			defer waiters.Done()
			a, err := u.WaitAction(ctx)
			if err != nil || a.Kind != "finish" {
				t.Error("ui: action broadcast did not reach waiter")
			}
		}()
	}
	uiKey(t, u, "F2")
	waiters.Wait()
}
