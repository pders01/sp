package scratchpad

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPreparePrivateEditRejectsIdentityInterleavingsIncludingABA(t *testing.T) {
	const date = "2026-01-15"
	for _, aba := range []bool{false, true} {
		name := "replacement"
		if aba {
			name = "replacement-and-return-to-original-key"
		}
		t.Run(name, func(t *testing.T) {
			original, other := privateTestManager(t), privateTestManager(t)
			defer original.Lock()
			defer other.Lock()
			if err := original.Save(&Scratchpad{Date: date, Content: "synthetic content owned by key A"}); err != nil {
				t.Fatal(err)
			}
			if err := other.Save(&Scratchpad{Date: date, Content: "synthetic content owned by key B"}); err != nil {
				t.Fatal(err)
			}
			metadata := filepath.Join(original.storageDir, "privacy.json")
			daily := filepath.Join(original.storageDir, date+".json")
			metadataA, dailyA := readTestFile(t, metadata), readTestFile(t, daily)
			metadataB := readTestFile(t, filepath.Join(other.storageDir, "privacy.json"))
			dailyB := readTestFile(t, filepath.Join(other.storageDir, date+".json"))
			repin := func(meta, note []byte) {
				t.Helper()
				if err := os.WriteFile(metadata, meta, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(daily, note, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := original.Unlock(fixturePassphrase); err != nil {
					t.Fatal(err)
				}
			}
			// Deliberate synthetic noncooperating substitution inside the
			// preparation boundary; no actual store/configuration is accessed.
			prepared, err := original.preparePrivateEdit(date, func(date string) (*Scratchpad, error) {
				repin(metadataB, dailyB)
				loaded, loadErr := original.GetByDate(date)
				if loadErr != nil {
					return nil, loadErr
				}
				if loaded.Content != "synthetic content owned by key B" {
					t.Fatal("interleaving did not exercise the foreign generation")
				}
				if aba {
					repin(metadataA, dailyA)
				}
				return loaded, nil
			})
			if !errors.Is(err, ErrLocked) || prepared != nil {
				t.Fatal("preparation returned content/sealer from different identity generations")
			}
			if !original.IsUnlocked() {
				t.Fatal("preparation failure unexpectedly relocked another authenticated generation")
			}
		})
	}
}

func TestPreparePrivateEditReturnsMatchedPublicOnlySealer(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	const date, text = "2026-01-15", "synthetic selected private content\r\n\n"
	if err := m.Save(&Scratchpad{Date: date, Content: text, AppliedTemplates: []string{"private metadata"}}); err != nil {
		t.Fatal(err)
	}
	identity, err := m.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	before := readTestFile(t, filepath.Join(m.storageDir, date+".json"))
	prepared, err := m.PreparePrivateEdit(date)
	if err != nil || prepared == nil || prepared.Content() != text {
		t.Fatal("matched private edit preparation failed", err)
	}
	if !bytes.Equal(before, readTestFile(t, filepath.Join(m.storageDir, date+".json"))) {
		t.Fatal("preparation changed the original note")
	}
	m.Lock()
	path, err := prepared.SealRecovery(prepared.Content())
	if err != nil {
		t.Fatal(err)
	}
	assertPinnedRecovery(t, path, identity, date, text)
}

func TestPreparePrivateEditRejectsRelockDuringLoad(t *testing.T) {
	m := privateTestManager(t)
	prepared, err := m.preparePrivateEdit("2026-01-15", func(date string) (*Scratchpad, error) {
		m.Lock()
		return &Scratchpad{Date: date, Content: "synthetic content"}, nil
	})
	if !errors.Is(err, ErrLocked) || prepared != nil {
		t.Fatal("relock during preparation leaked a partial result")
	}
}
