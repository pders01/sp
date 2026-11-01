package scratchpad

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pause after the original date snapshot has been visited, then let another
// writer finish before enable takes its final store lock. The callback is only
// a scheduling barrier; the worker never reenters the enabling manager.
func enableWithConcurrentRecordChange(t *testing.T, m *Manager, change func() error) ([]PrivacyProgress, int, error) {
	t.Helper()
	start, cancel := make(chan struct{}), make(chan struct{})
	defer close(cancel)
	done := make(chan error, 1)
	go func() {
		select {
		case <-start:
			done <- change()
		case <-cancel:
		}
	}()
	var events []PrivacyProgress
	before := map[string][]byte{}
	calls := 0
	err := m.enablePrivacyWithProgress(fixturePassphrase, func(dir string) error {
		calls++
		return syncDirectory(dir)
	}, func(p PrivacyProgress) {
		events = append(events, p)
		if p.Stage != "migrating" || p.Completed != p.Total {
			return
		}
		close(start)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(m.storageDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
				before[entry.Name()] = readTestFile(t, filepath.Join(m.storageDir, entry.Name()))
			}
		}
	})
	for name, data := range before {
		if !bytes.Equal(data, readTestFile(t, filepath.Join(m.storageDir, name))) {
			t.Fatalf("completion check rewrote %s", name)
		}
	}
	return events, calls, err
}

func TestEnablePrivacyCompletionRejectsRetainedKeyABA(t *testing.T) {
	for _, scenario := range []string{"visited-legacy", "new-legacy", "new-legacy-empty-store"} {
		t.Run(scenario, func(t *testing.T) {
			m := privateTestManager(t)
			defer m.Lock()
			// The fixture uses compact JSON; use production serialization so a
			// retained-key disable/re-enable restores the exact same fingerprint.
			lock, err := lockNote(m.storageDir, "privacy")
			if err != nil {
				t.Fatal(err)
			}
			metadata, _, err := m.readMetadata()
			if err == nil {
				err = m.setPrivacyState(metadata, "", syncDirectory)
			}
			_ = lock.Close()
			if err != nil {
				t.Fatal(err)
			}
			_, fingerprint, err := m.readMetadata()
			if err != nil {
				t.Fatal(err)
			}
			const visited = "2026-02-14"
			const added = "2026-02-15"
			const content = "synthetic private content"
			date := added
			count := 1
			switch scenario {
			case "visited-legacy":
				date = visited
				err = m.Save(&Scratchpad{Date: visited, Content: content})
			case "new-legacy":
				err = m.Append(visited, "Updates", "synthetic-helper", []string{"shared update"})
			case "new-legacy-empty-store":
				count = 0
			}
			if err != nil {
				t.Fatal(err)
			}
			other, err := NewManagerAt(m.storageDir)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Lock()
			events, calls, err := enableWithConcurrentRecordChange(t, m, func() error {
				if err := other.DisablePrivacy(fixturePassphrase); err != nil {
					return err
				}
				if scenario != "visited-legacy" {
					if err := other.Save(&Scratchpad{Date: added, Content: content}); err != nil {
						return err
					}
				}
				injected := errors.New("synthetic metadata directory sync failure")
				err := other.enablePrivacyWithProgress(fixturePassphrase, func(string) error { return injected }, nil)
				if !errors.Is(err, ErrDurabilityUnconfirmed) || !errors.Is(err, injected) {
					return errors.New("reactivation did not stop at the intended durability boundary")
				}
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "legacy") || calls != 1 {
				t.Fatalf("completion = %v, sync calls = %d; want incomplete migration before final sync", err, calls)
			}
			want := []PrivacyProgress{{"inspecting", 0, 0}, {"migrating", 0, count}}
			if count != 0 {
				want = append(want, PrivacyProgress{"migrating", 1, 1})
			}
			assertPrivacyProgress(t, events, want)
			_, current, err := m.readMetadata()
			if err != nil || current != fingerprint {
				t.Fatalf("test requires identical final active metadata: %v", err)
			}
			status, err := m.PrivacyStatus()
			if err != nil || !status.Enabled || len(status.PlaintextDates) != 1 || status.PlaintextDates[0] != date {
				t.Fatalf("unexpected incomplete state: %+v, %v", status, err)
			}
			// Only an explicit retry may migrate the plaintext left by the
			// intervening policy reversal; preserve its content exactly.
			if err := m.EnablePrivacy(fixturePassphrase); err != nil {
				t.Fatal(err)
			}
			note, err := m.GetByDate(date)
			if err != nil || note.Content != content {
				t.Fatalf("retry lost synthetic content: %v", err)
			}
			status, err = m.PrivacyStatus()
			if err != nil || len(status.PlaintextDates) != 0 {
				t.Fatalf("retry left legacy records: %+v, %v", status, err)
			}
		})
	}
}

func TestEnablePrivacyCompletionRescansCurrentPaths(t *testing.T) {
	for _, scenario := range []string{"legacy", "empty-legacy", "uppercase", "directory", "symlink", "malformed", "date-mismatch", "shared-only"} {
		t.Run(scenario, func(t *testing.T) {
			m := privateTestManager(t)
			defer m.Lock()
			const visited = "2026-02-14"
			const added = "2026-02-15"
			if err := m.Append(visited, "Updates", "synthetic-helper", []string{"shared update"}); err != nil {
				t.Fatal(err)
			}
			events, calls, err := enableWithConcurrentRecordChange(t, m, func() error {
				lock, err := lockNote(m.storageDir, "privacy")
				if err != nil {
					return err
				}
				defer lock.Close()
				path := filepath.Join(m.storageDir, added+".json")
				r := diskRecord{Version: 2, Date: added, Agent: AgentArea{Content: "synthetic shared update"}}
				switch scenario {
				case "directory":
					return os.Mkdir(path, 0o700)
				case "symlink":
					return os.Symlink(visited+".json", path)
				case "malformed":
					return os.WriteFile(path, []byte("{synthetic invalid JSON"), 0o600)
				case "uppercase":
					path = filepath.Join(m.storageDir, added+".JSON")
				case "date-mismatch":
					r.Date = visited
				}
				var value any = r
				if scenario == "legacy" || scenario == "empty-legacy" {
					content := "synthetic legacy content"
					if scenario == "empty-legacy" {
						content = ""
					}
					value = legacyDocument{Date: added, Content: content}
				}
				data, err := json.Marshal(value)
				if err != nil {
					return err
				}
				return os.WriteFile(path, data, 0o600)
			})
			want := []PrivacyProgress{{"inspecting", 0, 0}, {"migrating", 0, 1}, {"migrating", 1, 1}}
			if scenario == "shared-only" {
				if err != nil || calls != 2 {
					t.Fatalf("safe shared-only addition = %v, sync calls = %d", err, calls)
				}
				want = append(want, PrivacyProgress{"complete", 1, 1})
			} else if err == nil || calls != 1 {
				t.Fatalf("unsafe addition = %v, sync calls = %d; want failure before final sync", err, calls)
			}
			assertPrivacyProgress(t, events, want)
		})
	}
}
