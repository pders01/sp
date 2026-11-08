package scratchpad

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPreparedPrivateSaveRejectsGenerationSwitchIncludingMissingDate(t *testing.T) {
	for _, missing := range []bool{false, true} {
		for _, timing := range []string{"before-load", "after-load", "after-load-ABA"} {
			name := timing
			if missing {
				name += "-missing-date"
			}
			t.Run(name, func(t *testing.T) {
				m, other := privateTestManager(t), privateTestManager(t)
				defer m.Lock()
				defer other.Lock()
				const date = "2026-01-15"
				baseline := ""
				if !missing {
					baseline = "synthetic equal baseline under different keys"
					for _, manager := range []*Manager{m, other} {
						if err := manager.Save(&Scratchpad{Date: date, Content: baseline}); err != nil {
							t.Fatal(err)
						}
					}
				}
				originalIdentity, err := m.unlockedIdentity()
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := m.PreparePrivateEdit(date)
				if err != nil {
					t.Fatal(err)
				}
				metaPath, dailyPath := filepath.Join(m.storageDir, "privacy.json"), filepath.Join(m.storageDir, date+".json")
				metaA := readTestFile(t, metaPath)
				metaB := readTestFile(t, filepath.Join(other.storageDir, "privacy.json"))
				var dailyA, dailyB []byte
				if !missing {
					dailyA = readTestFile(t, dailyPath)
					dailyB = readTestFile(t, filepath.Join(other.storageDir, date+".json"))
				}
				switchTo := func(meta, daily []byte) {
					t.Helper()
					if writeErr := os.WriteFile(metaPath, meta, 0o600); writeErr != nil {
						t.Fatal(writeErr)
					}
					if !missing {
						if writeErr := os.WriteFile(dailyPath, daily, 0o600); writeErr != nil {
							t.Fatal(writeErr)
						}
					}
					if unlockErr := m.Unlock(fixturePassphrase); unlockErr != nil {
						t.Fatal(unlockErr)
					}
				}
				const draft = "synthetic key-A draft must not publish under key B"
				if timing == "before-load" {
					switchTo(metaB, dailyB)
					err = prepared.Save(baseline, draft)
				} else {
					err = prepared.save(baseline, draft, func(date string) (*Scratchpad, error) {
						note, loadErr := m.GetByDate(date)
						if loadErr != nil {
							return nil, loadErr
						}
						switchTo(metaB, dailyB)
						if timing == "after-load-ABA" {
							switchTo(metaA, dailyA)
						}
						return note, nil
					})
				}
				if !errors.Is(err, ErrLocked) {
					t.Fatal("generation switch passed the publication critical section", err)
				}
				if missing {
					if _, statErr := os.Stat(dailyPath); !os.IsNotExist(statErr) {
						t.Fatal("generation switch created the absent date")
					}
				} else {
					want := dailyB
					if timing == "after-load-ABA" {
						want = dailyA
					}
					if !bytes.Equal(want, readTestFile(t, dailyPath)) {
						t.Fatal("rejected generation switch overwrote stored bytes")
					}
				}
				path, recoveryErr := prepared.SealRecovery(draft)
				if recoveryErr != nil {
					t.Fatal(recoveryErr)
				}
				assertPinnedRecovery(t, path, originalIdentity, date, draft)
			})
		}
	}
}

func TestPreparedPrivateSaveCannotReuseReauthenticatedSameKey(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	prepared, err := m.PreparePrivateEdit("2026-01-15")
	if err != nil {
		t.Fatal(err)
	}
	if unlockErr := m.Unlock(fixturePassphrase); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	if saveErr := prepared.Save("", "synthetic stale-session draft"); !errors.Is(saveErr, ErrLocked) {
		t.Fatal("reauthentication reused a prepared generation")
	}
	if _, _, encodeErr := m.encodeScratchpadReviewed(&Scratchpad{Date: "2026-01-15", Content: "synthetic"}, true, &prepared.generation); !errors.Is(encodeErr, ErrLocked) {
		t.Fatal("encryption did not validate the exact selected identity generation")
	}
}
