package scratchpad

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitialEnableIsDurableBeforePartialMigration(t *testing.T) {
	m := setupTestManager(t)
	if err := m.Save(&Scratchpad{Date: "2024-01-01", Content: "first private"}); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(m.storageDir, "2024-01-02.json")
	// A syntactically valid legacy page with unsupported fields cannot be
	// migrated lossily. It interrupts migration, but cannot opt future saves out.
	unsupported := []byte(`{"date":"2024-01-02","content":"second private","future":"keep"}`)
	if err := os.WriteFile(second, unsupported, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.EnablePrivacy(fixturePassphrase); err == nil {
		t.Fatal("unsupported legacy field did not interrupt migration")
	}
	if enabled, err := m.PrivacyEnabled(); err != nil || !enabled {
		t.Fatalf("explicit init not active: %v %v", enabled, err)
	}
	if bytes.Contains(readTestFile(t, filepath.Join(m.storageDir, "2024-01-01.json")), []byte("first private")) {
		t.Fatal("earlier note not migrated")
	}
	if !bytes.Equal(unsupported, readTestFile(t, second)) {
		t.Fatal("unsupported fields were discarded")
	}
	metadata := readTestFile(t, filepath.Join(m.storageDir, "privacy.json"))
	if err := m.Save(&Scratchpad{Date: "2024-01-03", Content: "future private"}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(readTestFile(t, filepath.Join(m.storageDir, "2024-01-03.json")), []byte("future private")) {
		t.Fatal("partial migration wrote plaintext")
	}
	writeJSON(t, second, legacyDocument{Date: "2024-01-02", Content: "second private"})
	m.Lock()
	if err := m.EnablePrivacy(fixturePassphrase); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(metadata, readTestFile(t, filepath.Join(m.storageDir, "privacy.json"))) {
		t.Fatal("resumption rotated key")
	}
	status, err := m.PrivacyStatus()
	if err != nil || len(status.EncryptedDates) != 3 || len(status.PlaintextDates) != 0 {
		t.Fatalf("resumed status: %+v %v", status, err)
	}
}

func TestMissingMetadataFreshManagerAndEmptyStore(t *testing.T) {
	m := privateTestManager(t)
	if err := m.Save(&Scratchpad{Date: "2024-01-01", Content: "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(m.storageDir, "privacy.json")); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewManagerAt(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.PrivacyEnabled(); err == nil {
		t.Fatal("new manager missed orphan ciphertext")
	}
	if err := fresh.Save(&Scratchpad{Date: "2024-01-02", Content: "secret"}); err == nil {
		t.Fatal("new page wrote without metadata")
	}
	if err := fresh.EnablePrivacy(fixturePassphrase); err == nil {
		t.Fatal("generated replacement identity for orphan ciphertext")
	}
	if _, err := os.Stat(filepath.Join(m.storageDir, "privacy.json")); !os.IsNotExist(err) {
		t.Fatal("orphan key metadata was regenerated")
	}

	// The live manager also remembers observing metadata after Lock, even when
	// no daily ciphertext remains from which a new manager could infer privacy.
	if err := os.Remove(filepath.Join(m.storageDir, "2024-01-01.json")); err != nil {
		t.Fatal(err)
	}
	m.Lock()
	if err := m.Save(&Scratchpad{Date: "2024-01-03", Content: "secret"}); err == nil {
		t.Fatal("known-enabled empty store fell back to plaintext")
	}
}

func TestSharedOnlyDoesNotRequireMetadataOrBecomePrivateOnInit(t *testing.T) {
	m := privateTestManager(t)
	m.Lock()
	if err := m.Append("2024-01-01", "Updates", "agent", []string{"shared"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(m.storageDir, "privacy.json")); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewManagerAt(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	if enabled, err := fresh.PrivacyEnabled(); err != nil || enabled {
		t.Fatalf("shared-only requires private key: %v %v", enabled, err)
	}
	before := readTestFile(t, filepath.Join(m.storageDir, "2024-01-01.json"))
	if err := fresh.EnablePrivacy(fixturePassphrase); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, readTestFile(t, filepath.Join(m.storageDir, "2024-01-01.json"))) {
		t.Fatal("privacy init created private area in shared-only page")
	}
}

func TestPrivateMarshalFailureAndConflictNeverPublishPlaintext(t *testing.T) {
	m := privateTestManager(t)
	note := &Scratchpad{Date: "2024-01-01", Content: "secret"}
	if err := m.Save(note); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.storageDir, note.Date+".json")
	before := readTestFile(t, path)
	note.Content = string([]byte{0xff})
	if err := m.Save(note); err == nil {
		t.Fatal("invalid UTF-8 private content saved")
	}
	if !bytes.Equal(before, readTestFile(t, path)) {
		t.Fatal("failed save changed original")
	}
	assertNoSaveTemps(t, m)
}

func TestEnvelopeHeaderLimitsAndPrivateFormat(t *testing.T) {
	m := privateTestManager(t)
	date := "2024-01-01"
	path := filepath.Join(m.storageDir, date+".json")
	for _, payload := range [][]byte{
		[]byte("age-encryption.org/v1\n-> X25519 " + strings.Repeat("x", 5000) + "\n--- x\n" + strings.Repeat("x", 32)),
		[]byte("age-encryption.org/v1\n-> X25519 invalid\n--- x\n" + strings.Repeat("x", 32)),
		[]byte("age-encryption.org/v1\n-> scrypt invalid 30\n--- x\n" + strings.Repeat("x", 32)),
	} {
		writeJSON(t, path, diskRecord{Version: 2, Date: date, Private: &privateEnvelope{Format: privateFormat, Payload: base64.StdEncoding.EncodeToString(payload)}})
		if _, err := m.ReadShared(date); err == nil {
			t.Fatal("malformed private header accepted")
		}
		if err := m.Append(date, "Updates", "agent", []string{"one"}); err == nil {
			t.Fatal("malformed private header rewritten")
		}
	}
	if err := m.Unlock([]byte("wrong")); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	if _, err := m.GetByDate(date); !errors.Is(err, ErrLocked) {
		t.Fatalf("failed unlock left private access: %v", err)
	}
}
