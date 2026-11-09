package privateedit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/scratchpad"
)

const fixtureDate = "2026-01-15"
const fixtureSecret = "synthetic private lifecycle passphrase"

// Cheap wrapping is confined to disposable test metadata, never production KDFs.
func privateManager(t *testing.T) (mgr *scratchpad.Manager, directory string) {
	t.Helper()
	root := t.TempDir()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := age.NewScryptRecipient(fixtureSecret)
	if err != nil {
		t.Fatal(err)
	}
	recipient.SetWorkFactor(1)
	var wrapped bytes.Buffer
	writer, err := age.Encrypt(&wrapped, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, writeErr := writer.Write([]byte(identity.String())); writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	metadata, err := json.Marshal(map[string]any{"version": 1, "recipient": identity.Recipient().String(), "wrapped_identity": base64.StdEncoding.EncodeToString(wrapped.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(filepath.Join(root, "privacy.json"), metadata, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	manager, err := scratchpad.NewManagerAt(root)
	if err != nil {
		t.Fatal(err)
	}
	if unlockErr := manager.Unlock([]byte(fixtureSecret)); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	t.Cleanup(manager.Lock)
	note, err := manager.GetByDate(fixtureDate)
	if err != nil {
		t.Fatal(err)
	}
	note.Content = "synthetic original\r\n"
	note.AppliedTemplates = []string{"synthetic-private-template"}
	if saveErr := manager.SaveWithPrivacyPolicy(note, true); saveErr != nil {
		t.Fatal(saveErr)
	}
	return manager, root
}

type workflowFactory func(*testing.T, *scratchpad.Manager, ...string) *Workflow

func storeWorkflow(t *testing.T, manager *scratchpad.Manager, dates ...string) *Workflow {
	t.Helper()
	date := fixtureDate
	if len(dates) > 0 {
		date = dates[0]
	}
	prepared, err := manager.PreparePrivateEdit(date)
	if err != nil {
		t.Fatal("prepared fixture failed")
	}
	content := prepared.Content()
	mirror := snapshot(content)
	mirror.DocumentID = date
	e := &modelEditor{r: editorcore.Retention{Mirror: mirror}}
	return newWorkflow(e, managerStore{prepared: prepared}, content)
}

func TestWorkflowSyntheticStoreLifecycle(t *testing.T) { testStoreLifecycle(t, storeWorkflow) }

func testStoreLifecycle(t *testing.T, open workflowFactory) {
	t.Helper()
	for _, action := range []string{"finish-shared-preserved", "cancel-after-lock", "private-conflict", "policy-disabled"} {
		t.Run(action, func(t *testing.T) {
			manager, root := privateManager(t)
			w := open(t, manager)
			if w.Retained().Editor.Mirror.Text != "synthetic original\r\n" {
				t.Fatal("child received anything other than selected private content")
			}
			if _, err := w.Set(context.Background(), w.Retained().Editor.Mirror, privateCanary); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "finish-shared-preserved":
				if err := manager.Append(fixtureDate, "Updates", "synthetic-agent", []string{"public update after editor opened"}); err != nil {
					t.Fatal(err)
				}
				result, err := w.Finish(context.Background())
				if err != nil || !result.Published || !result.Closed || result.DurabilityUnconfirmed {
					t.Fatal("native/model private finish failed", err)
				}
				note, err := manager.GetByDate(fixtureDate)
				if err != nil || note.Content != privateCanary || !strings.Contains(note.Agent.Content, "public update after editor opened") || !reflect.DeepEqual(note.AppliedTemplates, []string{"synthetic-private-template"}) {
					t.Fatal("finish changed exact content or private/shared metadata", err)
				}
			case "cancel-after-lock", "private-conflict", "policy-disabled":
				if action == "private-conflict" {
					note, err := manager.GetByDate(fixtureDate)
					if err != nil {
						t.Fatal(err)
					}
					note.Content = "synthetic competing private replacement"
					if saveErr := manager.SaveWithPrivacyPolicy(note, true); saveErr != nil {
						t.Fatal(saveErr)
					}
				}
				if action == "policy-disabled" {
					// Intentional synthetic policy transition, not a plaintext
					// editor fallback. Only the preexisting baseline decrypts.
					if err := manager.DisablePrivacy([]byte(fixtureSecret)); err != nil {
						t.Fatal(err)
					}
				}
				before := readFixture(t, filepath.Join(root, fixtureDate+".json"))
				if action == "cancel-after-lock" {
					manager.Lock()
				} else {
					want := scratchpad.ErrConflict
					if action == "policy-disabled" {
						want = scratchpad.ErrPrivacyPolicyChanged
					}
					result, err := w.Finish(context.Background())
					if !errors.Is(err, want) || result.Published || result.Closed || w.Retained().Editor.Mirror.Text != privateCanary {
						t.Fatal("conflict/policy change overwrote, closed or lost draft", err)
					}
				}
				result, err := w.Cancel(context.Background(), true)
				if err != nil || result.Published || !result.Closed || len(result.Recoveries) != 1 || !result.Recoveries[0].Durable {
					t.Fatal("cancel did not seal before closing", err)
				}
				if !bytes.Equal(before, readFixture(t, filepath.Join(root, fixtureDate+".json"))) {
					t.Fatal("cancel changed the daily note instead of recovery only")
				}
				if !manager.IsUnlocked() {
					if unlockErr := manager.Unlock([]byte(fixtureSecret)); unlockErr != nil {
						t.Fatal(unlockErr)
					}
				}
				recovery, err := manager.ReadRecovery(result.Recoveries[0].Path)
				if err != nil || recovery.Content != privateCanary || recovery.Date != fixtureDate {
					t.Fatal("encrypted recovery lost exact text/date", err)
				}
			}
			assertNoPlaintextCanary(t, root)
		})
	}
}

func TestFactoryOpenRequiresReviewedEnabledUnlockedStoreAndRetainsStartupOwner(t *testing.T) {
	plain, err := scratchpad.NewManagerAt(t.TempDir())
	if err != nil {
		t.Fatal("owned plain fixture failed")
	}
	called := false
	open := func(_ context.Context, text string) (editorcore.Document, error) {
		called = true
		mirror := snapshot(text)
		mirror.DocumentID = fixtureDate
		return &modelEditor{r: editorcore.Retention{Mirror: mirror, Closed: true}}, editorcore.ErrUnsupported
	}
	if w, openErr := OpenDocumentExperiment(context.Background(), plain, fixtureDate, open); w != nil || !errors.Is(openErr, scratchpad.ErrPrivacyPolicyChanged) || called {
		t.Fatal("plaintext store reached generic editor startup")
	}
	manager, root := privateManager(t)
	manager.Lock()
	if w, openErr := OpenDocumentExperiment(context.Background(), manager, fixtureDate, open); w != nil || !errors.Is(openErr, scratchpad.ErrLocked) || called {
		t.Fatal("locked store reached generic editor startup")
	}
	if operationErr := manager.Unlock([]byte(fixtureSecret)); operationErr != nil {
		t.Fatal("owned reauthentication failed")
	}
	w, err := OpenDocumentExperiment(context.Background(), manager, fixtureDate, open)
	if w == nil || !called || !errors.Is(err, editorcore.ErrUnsupported) || !w.Retained().Editor.Closed || w.Retained().Baseline != "synthetic original\r\n" {
		t.Fatal("generic startup error lost its owner/original input")
	}
	assertNoPlaintextCanary(t, root)
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertNoPlaintextCanary(t *testing.T, root string) {
	t.Helper()
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && bytes.Contains(readFixture(t, path), []byte(privateCanary)) {
			t.Error("synthetic private draft persisted in plaintext")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
