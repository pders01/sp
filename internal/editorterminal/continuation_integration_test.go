package editorterminal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/embeddedvim"
	"github.com/pders01/sp/internal/privateedit"
	"github.com/pders01/sp/internal/scratchpad"
)

// Actual linked VM and encrypted lifecycle, but no terminal/native launch. A
// deliberately quarantined synthetic event exercises recovery authority only;
// it is not a claim to have reproduced a real transport outage.
func TestContinuationQuarantineWithBundledVMAndEncryptedStore(t *testing.T) {
	ctx := context.Background()
	store := filepath.Join(t.TempDir(), "store")
	if os.MkdirAll(store, 0o700) != nil {
		t.Fatal("owned recovery store directory failed")
	}
	const secret = "synthetic-recovery-fixture-only"
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal("owned recovery identity failed")
	}
	recipient, err := age.NewScryptRecipient(secret)
	if err != nil {
		t.Fatal("owned recovery recipient failed")
	}
	recipient.SetWorkFactor(1)
	var wrapped bytes.Buffer
	writer, err := age.Encrypt(&wrapped, recipient)
	if err != nil {
		t.Fatal("owned recovery wrap failed")
	}
	if _, err = writer.Write([]byte(identity.String())); err != nil || writer.Close() != nil {
		t.Fatal("owned recovery wrap write failed")
	}
	metadata, err := json.Marshal(map[string]any{
		"version":          1,
		"recipient":        identity.Recipient().String(),
		"wrapped_identity": base64.StdEncoding.EncodeToString(wrapped.Bytes()),
	})
	if err != nil || os.WriteFile(filepath.Join(store, "privacy.json"), metadata, 0o600) != nil {
		t.Fatal("owned recovery metadata write failed")
	}
	manager, err := scratchpad.NewManagerAt(store)
	if err != nil {
		t.Fatal("owned recovery fixture setup failed")
	}
	if manager.Unlock([]byte(secret)) != nil {
		t.Fatal("owned recovery privacy setup failed")
	}
	defer manager.Lock()
	const date = "2000-01-01"
	note, err := manager.GetByDate(date)
	if err != nil {
		t.Fatal("owned recovery date setup failed")
	}
	note.Content = "synthetic canonical"
	if manager.SaveWithPrivacyPolicy(note, true) != nil {
		t.Fatal("owned recovery canonical setup failed")
	}
	owner, err := privateedit.OpenInMemory(ctx, manager, editorcore.Options{DocumentID: date, Width: 50, Height: 10}, embeddedvim.OpenBundledUI)
	if err != nil || owner == nil {
		t.Fatal("owned bundled recovery open failed")
	}
	t.Cleanup(func() {
		if !owner.Retained().Editor.Closed {
			_, _ = owner.Key(ctx, "Cancel")
			_, _ = owner.Cancel(ctx, true)
		}
	})
	// The terminal admits larger pastes than the Vim document budget. A
	// synchronous zero-execution rejection must remain visible and usable.
	initial := fixtureModel()
	initial.editor = owner
	initial.enqueue(Input{Paste: true, Text: strings.Repeat("x", embeddedvim.MaxTextBytes+1)})
	runJob(t, initial, initial.next())
	if initial.phase != "editing" || initial.held != nil || initial.status == "" || owner.Retained().Editor.Mirror.Text != "synthetic canonical" {
		t.Fatal("oversized paste trapped or changed the bundled editor")
	}
	if _, err = owner.Type(ctx, "AX"); err != nil {
		t.Fatal("owned recovery edit failed")
	}
	if _, err = owner.Key(ctx, "Escape"); err != nil {
		t.Fatal("owned recovery mode exit failed")
	}
	// This full receipt is for a NEW cancellation request, not the uncertain key.
	if _, err = owner.Key(ctx, "Cancel"); err != nil {
		t.Fatal("owned recovery freeze failed")
	}
	m := fixtureModel()
	retained := Report{Editor: owner, PendingInput: true, pending: []Input{{Key: "Escape", Partial: true}, {Text: "synthetic unsent"}}}
	restoreContinuation(m, retained)
	m.finished(jobMsg{kind: "open", owner: owner}) // Do not run terminal watchers.
	if apply(m, physical('r')) != nil || m.held == nil || len(m.queue) != 1 {
		t.Fatal("later bundled receipt released quarantined events")
	}
	apply(m, physical('k'))
	if m.phase != "known-confirm" {
		t.Fatal("bundled reentry did not require physical loss consent")
	}
	runJob(t, m, apply(m, physical('y')))
	result := m.jobs.lastResult().outcome
	if !result.Closed || !result.MayHaveLostEdits || m.held != nil || len(m.queue) != 0 || len(result.Recoveries) != 1 || !result.Recoveries[0].Durable {
		t.Fatal("confirmed bundled recovery did not durably settle ownership")
	}
	note, err = manager.GetByDate(date)
	if err != nil || note.Content != "synthetic canonical" {
		t.Fatal("transport recovery overwrote canonical source")
	}
}

type rejectedPasteEditor struct{ *fakeEditor }

func (e rejectedPasteEditor) InsertText(_ context.Context, text string) (editorcore.Snapshot, error) {
	e.receipt = editorcore.InputReceipt{RequestID: e.receipt.RequestID + 1, RequestedBytes: len(text), Known: true}
	return e.r.Editor.Mirror, editorcore.ErrUnsupported
}

func TestKnownPreExecutionRejectionDoesNotTrapHealthyEditor(t *testing.T) {
	m := fixtureModel()
	e := rejectedPasteEditor{fixtureEditor()}
	m.editor = e
	m.enqueue(Input{Paste: true, Text: "synthetic rejected paste"})
	runJob(t, m, m.next())
	if m.phase != "editing" || m.held != nil || m.editor.Retained().Editor.Fault != nil {
		t.Fatal("known pre-execution rejection trapped a healthy editor")
	}
	if m.status == "" || e.r.Editor.Mirror.Text != testPrivate {
		t.Fatal("input rejection was silent or changed source")
	}
}
