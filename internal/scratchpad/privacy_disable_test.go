package scratchpad

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestDisablePrivacyRoundTrip(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	created := time.Date(2024, 1, 1, 12, 34, 56, 123, time.UTC)
	note := &Scratchpad{
		Date: "2024-01-01", Content: "private\tα\r\n\nlast line\n\n",
		Created: created, AppliedTemplates: []string{"private-template"},
	}
	if err := m.Save(note); err != nil {
		t.Fatal(err)
	}
	if err := m.Append(note.Date, "Updates", "synthetic-agent", []string{"shared bullet"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(&Scratchpad{Date: "2024-01-02", Created: created, Agent: AgentArea{Contributions: []Contribution{}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Append("2024-01-03", "Updates", "agent", []string{"shared-only"}); err != nil {
		t.Fatal(err)
	}
	// A partly enabled store can also have legacy plaintext still to migrate.
	legacyPath := filepath.Join(m.storageDir, "2024-01-04.json")
	writeJSON(t, legacyPath, legacyDocument{Date: "2024-01-04", Content: "legacy", Created: created})
	legacyBefore := readTestFile(t, legacyPath)
	sharedPath := filepath.Join(m.storageDir, "2024-01-03.json")
	sharedBefore := readTestFile(t, sharedPath)
	want, loadErr := m.GetByDate(note.Date)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	metadata, _, loadErr := m.readMetadata()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	recoveryPath, loadErr := m.SealRecovery(note.Date, "unsaved recovery")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	recoveryBefore := readTestFile(t, recoveryPath)
	m.Lock()
	if err := m.DisablePrivacy(fixturePassphrase); err != nil {
		t.Fatal(err)
	}
	m.Lock()
	fresh, loadErr := NewManagerAt(m.storageDir)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	defer fresh.Lock()
	if enabled, err := fresh.PrivacyEnabled(); err != nil || enabled {
		t.Fatalf("disabled policy = %v, %v", enabled, err)
	}
	got, loadErr := fresh.GetByDate(note.Date)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if got.Content != want.Content || !got.Created.Equal(want.Created) || !got.Modified.Equal(want.Modified) ||
		!reflect.DeepEqual(got.AppliedTemplates, want.AppliedTemplates) || !reflect.DeepEqual(got.Agent, want.Agent) {
		t.Fatalf("decryption changed note data: got %+v, want %+v", got, want)
	}
	empty, loadErr := fresh.GetByDate("2024-01-02")
	if loadErr != nil || empty.Content != "" || !empty.Created.Equal(created) || empty.Agent.Contributions == nil {
		t.Fatalf("empty private note = %+v, %v", empty, loadErr)
	}
	for path, before := range map[string][]byte{legacyPath: legacyBefore, sharedPath: sharedBefore, recoveryPath: recoveryBefore} {
		if !bytes.Equal(before, readTestFile(t, path)) {
			t.Fatalf("disable changed untouched file %s", filepath.Base(path))
		}
	}
	status, loadErr := fresh.PrivacyStatus()
	if loadErr != nil || status.Enabled || status.Decrypting || len(status.EncryptedDates) != 0 || len(status.PlaintextDates) != 3 {
		t.Fatalf("disabled status = %+v, %v", status, loadErr)
	}
	if err := fresh.DisablePrivacy(nil); err != nil {
		t.Fatalf("idempotent disable: %v", err)
	}
	if err := fresh.Save(want); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale encrypted revision after reversal = %v", err)
	}
	if err := fresh.Save(&Scratchpad{Date: "2024-01-05", Content: "new plaintext"}); err != nil {
		t.Fatalf("disabled save required unlock: %v", err)
	}
	if err := fresh.Append(note.Date, "Updates", "another-agent", []string{"another shared bullet"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.ReadRecovery(recoveryPath); !errors.Is(err, ErrLocked) {
		t.Fatalf("recovery without unlock = %v", err)
	}
	if err := fresh.Unlock(fixturePassphrase); err != nil {
		t.Fatal(err)
	}
	recovery, loadErr := fresh.ReadRecovery(recoveryPath)
	if loadErr != nil || recovery.Content != "unsaved recovery" {
		t.Fatalf("retained recovery = %+v, %v", recovery, loadErr)
	}
	retained, _, loadErr := fresh.readMetadata()
	if loadErr != nil || retained.Recipient != metadata.Recipient || retained.WrappedIdentity != metadata.WrappedIdentity {
		t.Fatalf("disable replaced key: %+v, %v", retained, loadErr)
	}
	info, loadErr := os.Stat(filepath.Join(m.storageDir, note.Date+".json"))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("plaintext permissions = %o", info.Mode().Perm())
	}
	if err := fresh.EnablePrivacy([]byte("wrong passphrase")); err == nil {
		t.Fatal("re-enable accepted a replacement passphrase")
	}
	if enabled, err := fresh.PrivacyEnabled(); err != nil || enabled {
		t.Fatalf("failed re-enable changed policy: %v, %v", enabled, err)
	}
	if err := fresh.EnablePrivacy(fixturePassphrase); err != nil {
		t.Fatal(err)
	}
	status, loadErr = fresh.PrivacyStatus()
	if loadErr != nil || !status.Enabled || len(status.EncryptedDates) != 4 || len(status.PlaintextDates) != 0 {
		t.Fatalf("re-enabled status = %+v, %v", status, loadErr)
	}
	if _, err := fresh.ReadRecovery(recoveryPath); err != nil {
		t.Fatalf("re-enable lost old recovery key: %v", err)
	}
	assertNoSaveTemps(t, m)
}

func TestDisablePrivacyPreflightFailureDoesNotPublishPlaintext(t *testing.T) {
	for _, failure := range []string{"wrong passphrase", "corrupt ciphertext", "unknown fields", "missing metadata"} {
		t.Run(failure, func(t *testing.T) {
			m := privateTestManager(t)
			defer m.Lock()
			for _, date := range []string{"2024-01-01", "2024-01-02"} {
				if err := m.Save(&Scratchpad{Date: date, Content: "secret"}); err != nil {
					t.Fatal(err)
				}
			}
			second := filepath.Join(m.storageDir, "2024-01-02.json")
			switch failure {
			case "corrupt ciphertext":
				data := readTestFile(t, second)
				r, err := decodeRecord(data, "2024-01-02")
				if err != nil {
					t.Fatal(err)
				}
				// A valid outer record with unauthentic ciphertext.
				r.Private.Payload = r.Private.Payload[:len(r.Private.Payload)-4] + "AAAA"
				writeJSON(t, second, r)
			case "unknown fields":
				if err := os.WriteFile(second, []byte(`{"date":"2024-01-02","content":"legacy","future":true}`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing metadata":
				if err := os.Remove(filepath.Join(m.storageDir, "privacy.json")); err != nil {
					t.Fatal(err)
				}
			}
			first := filepath.Join(m.storageDir, "2024-01-01.json")
			beforeFirst, beforeSecond := readTestFile(t, first), readTestFile(t, second)
			passphrase := fixturePassphrase
			if failure == "wrong passphrase" {
				passphrase = []byte("wrong")
			}
			if err := m.DisablePrivacy(passphrase); err == nil {
				t.Fatal("disable accepted invalid store or passphrase")
			}
			if !bytes.Equal(beforeFirst, readTestFile(t, first)) || !bytes.Equal(beforeSecond, readTestFile(t, second)) {
				t.Fatal("failed preflight changed a daily file")
			}
			assertNoSaveTemps(t, m)
		})
	}
}

func TestDisablePrivacyResumesEveryDurabilityBoundary(t *testing.T) {
	for failAt := 1; failAt <= 4; failAt++ {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			m := privateTestManager(t)
			defer m.Lock()
			for _, date := range []string{"2024-01-01", "2024-01-02"} {
				if err := m.Save(&Scratchpad{Date: date, Content: "secret " + date}); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			operationErr := m.disablePrivacy(fixturePassphrase, func(dir string) error {
				calls++
				if calls == failAt {
					return errors.New("injected sync failure")
				}
				return syncDirectory(dir)
			})
			if !errors.Is(operationErr, ErrDurabilityUnconfirmed) {
				t.Fatalf("disable = %v", operationErr)
			}
			m.Lock()
			fresh, operationErr := NewManagerAt(m.storageDir)
			if operationErr != nil {
				t.Fatal(operationErr)
			}
			defer fresh.Lock()
			status, operationErr := fresh.PrivacyStatus()
			if operationErr != nil || status.Decrypting != (failAt < 4) || len(status.EncryptedDates) != max(3-failAt, 0) {
				t.Fatalf("partial status = %+v, %v", status, operationErr)
			}
			if failAt < 4 {
				for name, operation := range map[string]func() error{
					"save":         func() error { return fresh.Save(&Scratchpad{Date: "2024-01-03", Content: "new"}) },
					"append":       func() error { return fresh.Append("2024-01-01", "Updates", "agent", []string{"new"}) },
					"delete":       func() error { return fresh.Delete("2024-01-01") },
					"enable":       func() error { return fresh.EnablePrivacy(fixturePassphrase) },
					"read private": func() error { _, err := fresh.GetByDate("2024-01-01"); return err },
				} {
					if err := operation(); !errors.Is(err, ErrPrivacyDecrypting) {
						t.Fatalf("%s during reversal = %v", name, err)
					}
				}
				if _, err := fresh.ReadShared("2024-01-01"); err != nil {
					t.Fatalf("shared read during reversal = %v", err)
				}
			}
			if err := fresh.DisablePrivacy(fixturePassphrase); err != nil {
				t.Fatalf("resume = %v", err)
			}
			status, operationErr = fresh.PrivacyStatus()
			if operationErr != nil || status.Enabled || status.Decrypting || len(status.EncryptedDates) != 0 || len(status.PlaintextDates) != 2 {
				t.Fatalf("resumed status = %+v, %v", status, operationErr)
			}
			for _, date := range []string{"2024-01-01", "2024-01-02"} {
				note, err := fresh.GetByDate(date)
				if err != nil || note.Content != "secret "+date {
					t.Fatalf("resumed note = %+v, %v", note, err)
				}
			}
			assertNoSaveTemps(t, m)
		})
	}
}

func TestPrivacyMetadataRejectsUnknownReversalStates(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	metadata, _, err := m.readMetadata()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		version int
		state   string
	}{
		{1, "disabled"}, {1, "decrypting"}, {2, ""}, {2, "false"}, {3, "disabled"},
	} {
		metadata.Version, metadata.State = tc.version, tc.state
		writeJSON(t, filepath.Join(m.storageDir, "privacy.json"), metadata)
		if _, err := m.PrivacyEnabled(); err == nil {
			t.Fatalf("accepted metadata version=%d, state=%q", tc.version, tc.state)
		}
	}
}

func TestDisablePrivacyEmptyStore(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	if err := m.DisablePrivacy(fixturePassphrase); err != nil {
		t.Fatal(err)
	}
	if enabled, err := m.PrivacyEnabled(); err != nil || enabled {
		t.Fatalf("disabled empty store policy = %v, %v", enabled, err)
	}
	if err := m.EnablePrivacy(fixturePassphrase); err != nil {
		t.Fatal(err)
	}
	if enabled, err := m.PrivacyEnabled(); err != nil || !enabled {
		t.Fatalf("re-enabled empty store policy = %v, %v", enabled, err)
	}
}

func TestDisabledMetadataCannotHideCiphertext(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	if err := m.Save(&Scratchpad{Date: "2024-01-01", Content: "secret"}); err != nil {
		t.Fatal(err)
	}
	metadata, _, err := m.readMetadata()
	if err != nil {
		t.Fatal(err)
	}
	metadata.Version, metadata.State = 2, "disabled"
	writeJSON(t, filepath.Join(m.storageDir, "privacy.json"), metadata)
	fresh, err := NewManagerAt(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	for name, operation := range map[string]func() error{
		"policy":  func() error { _, err := fresh.PrivacyEnabled(); return err },
		"disable": func() error { return fresh.DisablePrivacy(fixturePassphrase) },
		"enable":  func() error { return fresh.EnablePrivacy(fixturePassphrase) },
		"save":    func() error { return fresh.Save(&Scratchpad{Date: "2024-01-02", Content: "new"}) },
	} {
		if err := operation(); err == nil {
			t.Fatalf("%s accepted ciphertext in disabled store", name)
		}
	}
}
