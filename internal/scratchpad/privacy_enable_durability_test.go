package scratchpad

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
)

func TestEnablePrivacyConfirmsUnconfirmedPublications(t *testing.T) {
	for _, scenario := range []string{"activation", "reenable-empty", "reenable-shared-only", "last-note"} {
		t.Run(scenario, func(t *testing.T) {
			var m *Manager
			if scenario == "activation" {
				m = setupTestManager(t)
			} else {
				m = privateTestManager(t)
			}
			defer m.Lock()
			const date = "2024-01-01"
			if scenario == "reenable-shared-only" {
				if err := m.Append(date, "Updates", "synthetic-helper", []string{"public update"}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "reenable-empty" || scenario == "reenable-shared-only" {
				if err := m.DisablePrivacy(fixturePassphrase); err != nil {
					t.Fatal(err)
				}
			}
			failAt := 1 // metadata activation/re-enable
			if scenario == "last-note" {
				writeJSON(t, filepath.Join(m.storageDir, date+".json"), legacyDocument{Date: date, Content: "synthetic first note"})
				writeJSON(t, filepath.Join(m.storageDir, "2024-01-02.json"), legacyDocument{Date: "2024-01-02", Content: "synthetic last note"})
				failAt = 3 // confirm existing metadata, then two note publications
			}
			injected := errors.New("synthetic directory sync failure")
			calls := 0
			var events []PrivacyProgress
			err := m.enablePrivacyWithProgress(fixturePassphrase, func(dir string) error {
				calls++
				if calls == failAt {
					return injected
				}
				return syncDirectory(dir)
			}, privacyProgressCollector(&events))
			if !errors.Is(err, ErrDurabilityUnconfirmed) || !errors.Is(err, injected) || calls != failAt {
				t.Fatalf("publication error = %v, sync calls = %d, want %d", err, calls, failAt)
			}
			want := []PrivacyProgress{{"inspecting", 0, 0}}
			if scenario == "last-note" {
				want = append(want, PrivacyProgress{"migrating", 0, 2}, PrivacyProgress{"migrating", 1, 2})
			}
			assertPrivacyProgress(t, events, want)
			if enabled, err := m.PrivacyEnabled(); err != nil || !enabled {
				t.Fatalf("unconfirmed publication must still be active: %v, %v", enabled, err)
			}

			// Retry as after process exit. Already-published files must not be
			// rewritten, but both metadata and final directory durability must
			// be confirmed, even with no private notes left to migrate.
			fresh, err := NewManagerAt(m.storageDir)
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.Lock()
			dates, err := fresh.privacyDates()
			if err != nil {
				t.Fatal(err)
			}
			before := map[string][]byte{}
			for _, name := range append([]string{"privacy"}, dates...) {
				before[name] = readTestFile(t, filepath.Join(m.storageDir, name+".json"))
			}
			for _, retryFailAt := range []int{1, 2, 0} {
				calls, events = 0, nil
				err := fresh.enablePrivacyWithProgress(fixturePassphrase, func(dir string) error {
					calls++
					if dir != m.storageDir {
						t.Fatalf("sync directory = %q, want %q", dir, m.storageDir)
					}
					if calls == retryFailAt {
						return injected
					}
					return syncDirectory(dir)
				}, privacyProgressCollector(&events))
				want := []PrivacyProgress{{"inspecting", 0, 0}}
				if retryFailAt != 1 {
					want = append(want, PrivacyProgress{"migrating", 0, len(dates)})
					for i := range dates {
						want = append(want, PrivacyProgress{"migrating", i + 1, len(dates)})
					}
				}
				if retryFailAt == 0 {
					if err != nil || calls != 2 {
						t.Fatalf("successful retry = %v, sync calls = %d, want 2", err, calls)
					}
					want = append(want, PrivacyProgress{"complete", len(dates), len(dates)})
				} else if !errors.Is(err, ErrDurabilityUnconfirmed) || !errors.Is(err, injected) || calls != retryFailAt {
					t.Fatalf("retry error = %v, sync calls = %d, want %d", err, calls, retryFailAt)
				}
				assertPrivacyProgress(t, events, want)
				for name, data := range before {
					if !bytes.Equal(data, readTestFile(t, filepath.Join(m.storageDir, name+".json"))) {
						t.Fatalf("retry rewrote %s", name)
					}
				}
			}
			assertNoSaveTemps(t, fresh)
		})
	}
}

func TestEnablePrivacyRetryConfirmsMetadataBeforeMigration(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	const date = "2024-01-01"
	path := filepath.Join(m.storageDir, date+".json")
	writeJSON(t, path, legacyDocument{Date: date, Content: "synthetic pending migration"})
	before := readTestFile(t, path)
	var events []PrivacyProgress
	err := m.enablePrivacyWithProgress(fixturePassphrase, func(string) error {
		return errors.New("synthetic metadata durability failure")
	}, privacyProgressCollector(&events))
	if !errors.Is(err, ErrDurabilityUnconfirmed) {
		t.Fatalf("retry error = %v", err)
	}
	assertPrivacyProgress(t, events, []PrivacyProgress{{"inspecting", 0, 0}})
	if !bytes.Equal(before, readTestFile(t, path)) {
		t.Fatal("retry migrated a note before confirming metadata durability")
	}
	calls := 0
	err = m.enablePrivacyWithProgress(fixturePassphrase, func(dir string) error {
		calls++
		r, err := decodeRecord(readTestFile(t, path), date)
		if err != nil {
			t.Fatal(err)
		}
		if calls == 1 && r.legacy == nil || calls > 1 && r.Private == nil {
			t.Fatalf("incorrect metadata/migration ordering at sync %d", calls)
		}
		return syncDirectory(dir)
	}, nil)
	if err != nil || calls != 3 {
		t.Fatalf("retry = %v, sync calls = %d, want 3", err, calls)
	}
}

func TestEnablePrivacyCompletionChecksIntendedPolicy(t *testing.T) {
	for _, transition := range []string{"disabled", "decrypting", "replacement"} {
		t.Run(transition, func(t *testing.T) {
			m := privateTestManager(t)
			defer m.Lock()
			if err := m.Append("2024-01-01", "Updates", "synthetic-helper", []string{"public update"}); err != nil {
				t.Fatal(err)
			}
			other, err := NewManagerAt(m.storageDir)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Lock()
			var replacement []byte
			if transition == "replacement" {
				alternate := privateTestManager(t)
				defer alternate.Lock()
				replacement = readTestFile(t, filepath.Join(alternate.storageDir, "privacy.json"))
			}
			start := make(chan struct{})
			cancel := make(chan struct{})
			defer close(cancel)
			done := make(chan error, 1)
			go func() {
				select {
				case <-start:
				case <-cancel:
					return
				}
				switch transition {
				case "disabled":
					done <- other.DisablePrivacy(fixturePassphrase)
				case "decrypting":
					err := other.disablePrivacy(fixturePassphrase, func(string) error { return errors.New("synthetic interruption") })
					if errors.Is(err, ErrDurabilityUnconfirmed) {
						err = nil
					}
					done <- err
				case "replacement":
					// Model a cooperating metadata replacement with another valid
					// active policy. Merely checking Enabled would accept it.
					lock, err := lockNote(m.storageDir, "privacy")
					if err == nil {
						_, err = publish(filepath.Join(m.storageDir, "privacy.json"), replacement, false, syncDirectory)
						_ = lock.Close()
					}
					done <- err
				}
			}()
			var events []PrivacyProgress
			calls := 0
			err = m.enablePrivacyWithProgress(fixturePassphrase, func(dir string) error {
				calls++
				return syncDirectory(dir)
			}, func(p PrivacyProgress) {
				events = append(events, p)
				if p.Stage == "migrating" && p.Completed == 1 {
					// A test-only scheduling barrier: another writer changes policy
					// between the last note and completion. No manager reentry.
					close(start)
					if err := <-done; err != nil {
						t.Fatal(err)
					}
				}
			})
			wantErr := ErrPrivacyPolicyChanged
			if transition == "decrypting" {
				wantErr = ErrPrivacyDecrypting
			}
			if !errors.Is(err, wantErr) || calls != 1 {
				t.Fatalf("changed policy = %v, sync calls = %d; want %v, 1", err, calls, wantErr)
			}
			assertPrivacyProgress(t, events, []PrivacyProgress{{"inspecting", 0, 0}, {"migrating", 0, 1}, {"migrating", 1, 1}})
		})
	}
}
