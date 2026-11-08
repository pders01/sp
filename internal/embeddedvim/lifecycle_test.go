package embeddedvim

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/privateedit"
	"github.com/pders01/sp/internal/scratchpad"
)

const syntheticDate = "2000-01-01"
const syntheticOriginal = "SYNTHETIC fixture only\r\nsecond e\u0301🙂\n"

func encryptedFixture(t *testing.T) (*scratchpad.Manager, []byte, string) {
	t.Helper()
	root := t.TempDir()
	manager, err := scratchpad.NewManagerAt(root)
	if err != nil {
		t.Fatal("synthetic manager creation failed")
	}
	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		t.Fatal("synthetic entropy failed")
	}
	pass := []byte(hex.EncodeToString(entropy))
	clear(entropy)
	t.Cleanup(func() { manager.Lock(); clear(pass) })
	if manager.EnablePrivacy(pass) != nil {
		t.Fatal("synthetic privacy setup failed")
	}
	note, err := manager.GetByDate(syntheticDate)
	if err != nil {
		t.Fatal("synthetic date creation failed")
	}
	note.Content = syntheticOriginal
	if manager.SaveWithPrivacyPolicy(note, true) != nil {
		t.Fatal("synthetic baseline publication failed")
	}
	return manager, pass, root
}

func encryptedEditor(t *testing.T, manager *scratchpad.Manager) *privateedit.Embedded {
	t.Helper()
	assets, err := BundledAssets()
	if err != nil {
		t.Fatal("rebuild the shared public guest assets")
	}
	owner, err := privateedit.OpenInMemory(context.Background(), manager,
		editorcore.Options{DocumentID: syntheticDate, Width: 80, Height: 20},
		func(ctx context.Context, options editorcore.Options, text string) (editorcore.UI, error) {
			return OpenUI(ctx, assets.Wasm, assets.Guest, options, text)
		})
	if err != nil || owner == nil {
		t.Fatal("synthetic encrypted UI open failed")
	}
	return owner
}

func typeEncryptedCommand(t *testing.T, owner *privateedit.Embedded, command string) editorcore.Action {
	t.Helper()
	if _, err := owner.Type(context.Background(), ":"+command); err != nil {
		t.Fatal("synthetic command typing failed")
	}
	if _, err := owner.Key(context.Background(), "Enter"); err != nil {
		t.Fatal("synthetic command submission failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	action, err := owner.WaitAction(ctx)
	if err != nil {
		t.Fatal("synthetic ordered action missing")
	}
	return action
}

func addEncryptedDraft(t *testing.T, owner *privateedit.Embedded) string {
	t.Helper()
	entropy := make([]byte, 16)
	if _, err := rand.Read(entropy); err != nil {
		t.Fatal("synthetic draft entropy failed")
	}
	canary := "SYNTHETIC-PRIVATE-" + hex.EncodeToString(entropy) + "\r\n"
	clear(entropy)
	if _, err := owner.Type(context.Background(), "i"); err != nil {
		t.Fatal("synthetic insert mode failed")
	}
	if _, err := owner.InsertText(context.Background(), canary); err != nil {
		t.Fatal("synthetic literal insertion failed")
	}
	if _, err := owner.Key(context.Background(), "Escape"); err != nil {
		t.Fatal("synthetic normal mode failed")
	}
	return canary
}

func assertEncryptedFixtureOnly(t *testing.T, root, canary string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), canary) {
			return errors.New("fixture plaintext found")
		}
		return nil
	})
	if err != nil {
		t.Fatal("synthetic fixture contains plaintext or could not be verified")
	}
}

func TestWasmEncryptedFinishPreservesSharedAndExactDraft(t *testing.T) {
	manager, _, root := encryptedFixture(t)
	owner := encryptedEditor(t, manager)
	canary := addEncryptedDraft(t, owner)
	if manager.Append(syntheticDate, "Updates", "synthetic-shared", []string{"public concurrent fixture update"}) != nil {
		t.Fatal("synthetic shared update failed")
	}
	action := typeEncryptedCommand(t, owner, "wq")
	if action.Kind != "finish" || !action.Snapshot.Frozen || action.Snapshot.Text != canary+syntheticOriginal {
		t.Fatal("save request did not freeze exact input-ordered text")
	}
	result, err := owner.Finish(context.Background())
	if err != nil || !result.Published || !result.Closed {
		t.Fatal("synthetic encrypted finish failed")
	}
	note, err := manager.GetByDate(syntheticDate)
	if err != nil || note.Content != canary+syntheticOriginal || !strings.Contains(note.Agent.Content, "public concurrent fixture update") {
		t.Fatal("encrypted save changed bytes or lost shared data")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}

func TestWasmEncryptedFinishWhileVisualSavesWholeExactDocument(t *testing.T) {
	manager, _, root := encryptedFixture(t)
	owner := encryptedEditor(t, manager)
	canary := addEncryptedDraft(t, owner)
	if _, err := owner.Type(context.Background(), "V"); err != nil || len(owner.Frame().Selections) == 0 {
		t.Fatal("synthetic visual mode did not highlight")
	}
	if _, err := owner.Key(context.Background(), "Finish"); err != nil {
		t.Fatal("synthetic visual finish request failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	action, err := owner.WaitAction(ctx)
	if err != nil || action.Kind != "finish" || action.Snapshot.Text != canary+syntheticOriginal {
		t.Fatal("visual finish changed the document snapshot")
	}
	result, err := owner.Finish(context.Background())
	if err != nil || !result.Published || !result.Closed {
		t.Fatal("visual encrypted finish failed")
	}
	note, err := manager.GetByDate(syntheticDate)
	if err != nil || note.Content != canary+syntheticOriginal {
		t.Fatal("visual highlight metadata changed encrypted document bytes")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}

func TestWasmEncryptedCancelAfterRelockUsesPinnedRecovery(t *testing.T) {
	manager, pass, root := encryptedFixture(t)
	owner := encryptedEditor(t, manager)
	canary := addEncryptedDraft(t, owner)
	action := typeEncryptedCommand(t, owner, "q")
	if action.Kind != "cancel" {
		t.Fatal("quit command authorized publication")
	}
	if _, err := owner.Cancel(context.Background(), false); !errors.Is(err, privateedit.ErrConfirmationRequired) {
		t.Fatal("changed quit bypassed explicit recovery consent")
	}
	manager.Lock()
	result, err := owner.Cancel(context.Background(), true)
	if err != nil || !result.Closed || result.Published || len(result.Recoveries) != 1 || !result.Recoveries[0].Durable {
		t.Fatal("locked cancellation failed pinned encrypted recovery")
	}
	if manager.Unlock(pass) != nil {
		t.Fatal("synthetic reauthentication failed")
	}
	recovered, err := manager.ReadRecovery(result.Recoveries[0].Path)
	if err != nil || recovered.Content != canary+syntheticOriginal {
		t.Fatal("encrypted recovery changed exact text")
	}
	note, err := manager.GetByDate(syntheticDate)
	if err != nil || note.Content != syntheticOriginal {
		t.Fatal("quit replaced the saved private note")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}

func TestWasmInputFaultCanSealKnownDraftAfterExplicitLossConsent(t *testing.T) {
	manager, pass, root := encryptedFixture(t)
	assets, err := BundledAssets()
	if err != nil {
		t.Fatal("rebuild the shared public guest assets")
	}
	var ui *UI
	owner, err := privateedit.OpenExperimentWithDriver(context.Background(), manager,
		editorcore.Options{DocumentID: syntheticDate, Width: 80, Height: 20},
		func(ctx context.Context, options editorcore.Options, text string) (editorcore.UI, error) {
			var err error
			ui, err = OpenUI(ctx, assets.Wasm, assets.Guest, options, text)
			return ui, err
		})
	if err != nil {
		t.Fatal("synthetic fault owner open failed")
	}
	canary := addEncryptedDraft(t, owner)
	if _, err := ui.session.EvaluateForTest(context.Background(), `(()=>{throw new Error('synthetic input fault')})()`); err == nil {
		t.Fatal("synthetic guest fault did not fail")
	}
	if _, err := owner.Type(context.Background(), "x"); err == nil || !owner.Retained().Editor.LatestUnknown {
		t.Fatal("guest fault did not retain uncertain owner")
	}
	if _, err := owner.RecoverKnown(context.Background(), false); !errors.Is(err, privateedit.ErrConfirmationRequired) {
		t.Fatal("known-copy loss consent bypassed")
	}
	manager.Lock()
	result, err := owner.RecoverKnown(context.Background(), true)
	if err != nil || !result.Closed || !result.MayHaveLostEdits || len(result.Recoveries) == 0 {
		t.Fatal("known-copy encrypted recovery failed")
	}
	if manager.Unlock(pass) != nil {
		t.Fatal("synthetic reauthentication failed")
	}
	recovered, err := manager.ReadRecovery(result.Recoveries[0].Path)
	if err != nil || recovered.Content != canary+syntheticOriginal {
		t.Fatal("known-copy recovery lost acknowledged bytes")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}

func TestWasmResumeRevokesActionAndConflictRetainsEncryptedOwner(t *testing.T) {
	manager, pass, root := encryptedFixture(t)
	owner := encryptedEditor(t, manager)
	canary := addEncryptedDraft(t, owner)
	typeEncryptedCommand(t, owner, "q")
	if owner.Resume(context.Background()) != nil {
		t.Fatal("declined cancellation did not resume")
	}
	if _, err := owner.Finish(context.Background()); !errors.Is(err, editorcore.ErrNotFrozen) {
		t.Fatal("stale cancellation action authorized save")
	}
	note, err := manager.GetByDate(syntheticDate)
	if err != nil {
		t.Fatal("synthetic concurrent note load failed")
	}
	note.Content = "synthetic concurrent private edit"
	if manager.SaveWithPrivacyPolicy(note, true) != nil {
		t.Fatal("synthetic concurrent publication failed")
	}
	typeEncryptedCommand(t, owner, "wq")
	if _, err := owner.Finish(context.Background()); !errors.Is(err, scratchpad.ErrConflict) || owner.Retained().Editor.Closed {
		t.Fatal("conflict overwrote or dropped the private owner")
	}
	manager.Lock()
	result, err := owner.Cancel(context.Background(), true)
	if err != nil || !result.Closed || len(result.Recoveries) != 1 {
		t.Fatal("conflicted owner did not retain pinned recovery")
	}
	if manager.Unlock(pass) != nil {
		t.Fatal("synthetic reauthentication failed")
	}
	recovered, err := manager.ReadRecovery(result.Recoveries[0].Path)
	if err != nil || recovered.Content != canary+syntheticOriginal {
		t.Fatal("conflict recovery lost the draft")
	}
	assertEncryptedFixtureOnly(t, root, canary)
}
