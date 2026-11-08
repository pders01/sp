package embeddedvim

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/privateedit"
)

func TestEncryptedLiveCheckpointPreservesHistoryBaselineAndSharedData(t *testing.T) {
	manager, _, root := encryptedFixture(t)
	owner := encryptedEditor(t, manager)
	canary := addEncryptedDraft(t, owner)
	action := typeEncryptedCommand(t, owner, "w")
	if action.Kind != "save" {
		t.Fatal("write did not request a checkpoint")
	}
	result, err := owner.Checkpoint(context.Background())
	if err != nil || result.Closed || !result.Published || result.Disposition != privateedit.Active {
		t.Fatal("encrypted checkpoint closed or failed")
	}
	r := owner.Retained()
	if r.Editor.Mirror.Frozen || r.Editor.Mirror.Revision <= action.Snapshot.Revision || r.Baseline != canary+syntheticOriginal {
		t.Fatal("checkpoint retained stale authority or baseline")
	}
	if _, err := owner.Finish(context.Background()); !errors.Is(err, editorcore.ErrNotFrozen) {
		t.Fatal("checkpoint allowed old finish authority")
	}
	if _, err := owner.Type(context.Background(), "iX"); err != nil {
		t.Fatal("checkpoint did not reopen input")
	}
	if _, err := owner.Key(context.Background(), "Escape"); err != nil {
		t.Fatal("checkpoint Escape failed")
	}
	if _, err := owner.Type(context.Background(), "u"); err != nil {
		t.Fatal("checkpoint undo failed")
	}
	if owner.Retained().Editor.Mirror.Text != r.Baseline {
		t.Fatal("checkpoint lost prior history")
	}
	if _, err := owner.Type(context.Background(), "iY"); err != nil {
		t.Fatal("second checkpoint edit failed")
	}
	if _, err := owner.Key(context.Background(), "Escape"); err != nil {
		t.Fatal("second checkpoint Escape failed")
	}
	expected := owner.Retained().Editor.Mirror.Text
	if manager.Append(syntheticDate, "Updates", "synthetic-shared", []string{"checkpoint shared fixture"}) != nil {
		t.Fatal("synthetic shared update failed")
	}
	typeEncryptedCommand(t, owner, "w")
	if result, err = owner.Checkpoint(context.Background()); err != nil || result.Closed {
		t.Fatal("second checkpoint failed")
	}
	note, err := manager.GetByDate(syntheticDate)
	if err != nil || note.Content != expected || !strings.Contains(note.Agent.Content, "checkpoint shared fixture") {
		t.Fatal("checkpoint changed bytes or lost shared updates")
	}
	typeEncryptedCommand(t, owner, "x")
	if result, err = owner.Finish(context.Background()); err != nil || !result.Closed || !result.Published {
		t.Fatal("exit alias failed after checkpoint")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}

func TestCheckpointAuthenticationFailureRetainsPinnedRecovery(t *testing.T) {
	manager, pass, root := encryptedFixture(t)
	owner := encryptedEditor(t, manager)
	canary := addEncryptedDraft(t, owner)
	typeEncryptedCommand(t, owner, "w")
	manager.Lock()
	if result, err := owner.Checkpoint(context.Background()); err == nil || result.Closed || result.Published || !owner.Retained().Editor.Mirror.Frozen {
		t.Fatal("unauthenticated checkpoint released owner")
	}
	if _, err := owner.Cancel(context.Background(), false); !errors.Is(err, privateedit.ErrConfirmationRequired) {
		t.Fatal("failed checkpoint bypassed recovery consent")
	}
	result, err := owner.Cancel(context.Background(), true)
	if err != nil || !result.Closed || len(result.Recoveries) == 0 {
		t.Fatal("failed checkpoint lost original recovery pin")
	}
	if manager.Unlock(pass) != nil {
		t.Fatal("synthetic unlock failed")
	}
	recovery, err := manager.ReadRecovery(result.Recoveries[0].Path)
	if err != nil || recovery.Content != canary+syntheticOriginal {
		t.Fatal("checkpoint recovery changed exact bytes")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}

func TestGuestScrollingAndBackwardSearchReachViewport(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = fmt.Sprintf("row%02d", i)
	}
	text := strings.Join(lines, "\n")
	u := openSyntheticUI(t, text)
	uiType(t, u, "25Gzt")
	f := u.Frame()
	if u.guestSnapshot.Viewport == nil || u.guestSnapshot.Viewport.Top != 24 || f.CursorRow != 0 || !strings.Contains(f.Lines[0], "row24") {
		t.Fatal("zt did not move viewport to active source line")
	}
	uiType(t, u, "zz")
	if u.Frame().CursorRow < 2 || u.Frame().CursorRow > 4 {
		t.Fatal("zz did not center source cursor")
	}
	top := u.guestSnapshot.Viewport.Top
	uiKey(t, u, "CtrlE")
	if u.guestSnapshot.Viewport.Top != top+1 {
		t.Fatal("CtrlE did not scroll independently")
	}
	uiKey(t, u, "CtrlY")
	if u.guestSnapshot.Viewport.Top != top {
		t.Fatal("CtrlY did not reverse scroll")
	}
	uiType(t, u, "?row03")
	if u.guestSnapshot.Prompt == nil || u.guestSnapshot.Prompt.Prefix != "?" || !strings.HasPrefix(u.Frame().Lines[7], "?") {
		t.Fatal("backward search prompt shows wrong direction")
	}
	if len(u.Frame().Matches) == 0 {
		t.Fatal("incremental search did not highlight visible match")
	}
	uiKey(t, u, "Enter")
	if u.guestSnapshot.Cursor.Line != 3 || len(u.Frame().Matches) == 0 {
		t.Fatal("accepted backward search lost target/highlight")
	}
	uiText(t, u, text)
	f = u.Frame()
	f.Matches[0].Start++
	if f.Matches[0] == u.Frame().Matches[0] {
		t.Fatal("match metadata aliases UI owner")
	}
}

func TestDisplayMetadataValidationIsBounded(t *testing.T) {
	base := Snapshot{Text: "🙂\nx", Mode: "normal", Matches: []SelectionRange{{Head: Cursor{0, 2}}}, Viewport: &GuestViewport{Top: 1}}
	if !validSelectionSnapshot(base) {
		t.Fatal("valid match/viewport rejected")
	}
	for _, mutate := range []func(*Snapshot){
		func(s *Snapshot) { s.Viewport.Top = -1 }, func(s *Snapshot) { s.Viewport.Top = 2 },
		func(s *Snapshot) { s.Matches[0].Head.Ch = 1 }, func(s *Snapshot) { s.Matches = make([]SelectionRange, MaxSelections+1) },
		func(s *Snapshot) { s.Prompt = &Prompt{Kind: "search", Prefix: "\x1b"} },
	} {
		s := clone(base)
		mutate(&s)
		if validSelectionSnapshot(s) {
			t.Fatal("invalid display metadata accepted")
		}
	}
}
