package scratchpad

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
)

var fixturePassphrase = []byte("synthetic test passphrase")

// Use a deliberately cheap scrypt wrapper only in synthetic test fixtures.
// Production initialization is separately tested with age's default work factor.
func privateTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManagerAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := age.NewScryptRecipient(string(fixturePassphrase))
	if err != nil {
		t.Fatal(err)
	}
	recipient.SetWorkFactor(1)
	wrapped, err := encryptAge([]byte(identity.String()), recipient)
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(m.storageDir, "privacy.json"), privacyMetadata{Version: 1, Recipient: identity.Recipient().String(), WrappedIdentity: base64.StdEncoding.EncodeToString(wrapped)})
	if err := m.Unlock(fixturePassphrase); err != nil {
		t.Fatal(err)
	}
	return m
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPrivacyOptInAndMigration(t *testing.T) {
	m, err := NewManagerAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if enabled, privacyErr := m.PrivacyEnabled(); privacyErr != nil || enabled {
		t.Fatalf("new store privacy: %v, %v", enabled, privacyErr)
	}
	oldTime := time.Now().Add(-time.Hour).Round(0)
	note := &Scratchpad{Date: "2024-01-01", Content: "private synthetic secret", Created: oldTime, AppliedTemplates: []string{"template-private"}}
	if saveErr := m.Save(note); saveErr != nil {
		t.Fatal(saveErr)
	}
	modified := note.Modified
	path := filepath.Join(m.storageDir, note.Date+".json")
	before := readTestFile(t, path)
	if !bytes.Contains(before, []byte(note.Content)) {
		t.Fatal("opt-out changed legacy storage")
	}
	reopened, err := NewManagerAt(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, readTestFile(t, path)) {
		t.Fatal("constructor migrated notes")
	}
	if enableErr := reopened.EnablePrivacy(fixturePassphrase); enableErr != nil {
		t.Fatal(enableErr)
	}
	if enabled, privacyErr := reopened.PrivacyEnabled(); privacyErr != nil || !enabled {
		t.Fatalf("enabled: %v %v", enabled, privacyErr)
	}
	got, err := reopened.GetByDate(note.Date)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != note.Content || !got.Created.Equal(oldTime) || !got.Modified.Equal(modified) || strings.Join(got.AppliedTemplates, ",") != "template-private" {
		t.Fatalf("migration lost private metadata: %+v", got)
	}
	identity, err := reopened.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"privacy.json", note.Date + ".json"} {
		data := readTestFile(t, filepath.Join(m.storageDir, name))
		for _, secret := range []string{note.Content, "template-private", identity.String(), string(fixturePassphrase)} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatalf("%s discloses private value", name)
			}
		}
		info, statErr := os.Stat(filepath.Join(m.storageDir, name))
		if statErr != nil {
			t.Fatal(statErr)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("%s is not a regular file", name)
		}
		// Windows FileMode reports read/write attributes, not NTFS ACLs.
		// Keep migration/non-disclosure checks above on every platform, but
		// only assert POSIX group/other permission bits where meaningful.
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s permissions %o", name, info.Mode().Perm())
		}
	}
	metadata := readTestFile(t, filepath.Join(m.storageDir, "privacy.json"))
	// Existing metadata must be unlocked, never replaced, even on a failed attempt.
	if enableErr := reopened.EnablePrivacy([]byte("wrong")); enableErr == nil {
		t.Fatal("wrong passphrase accepted")
	}
	if reopened.IsUnlocked() {
		t.Fatal("failed unlock retained identity")
	}
	if !bytes.Equal(metadata, readTestFile(t, filepath.Join(m.storageDir, "privacy.json"))) {
		t.Fatal("metadata overwritten")
	}
	if unlockErr := reopened.Unlock(fixturePassphrase); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	status, err := reopened.PrivacyStatus()
	if err != nil || !status.Enabled || strings.Join(status.EncryptedDates, ",") != note.Date || len(status.PlaintextDates) != 0 {
		t.Fatalf("status: %+v %v", status, err)
	}
}

func TestLockedPrivateAndSharedRecovery(t *testing.T) {
	m := privateTestManager(t)
	note := &Scratchpad{Date: "2024-01-01", Content: "secret body", Created: time.Now()}
	if err := m.Save(note); err != nil {
		t.Fatal(err)
	}
	identity, err := m.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	m.Lock()
	if m.IsUnlocked() {
		t.Fatal("still unlocked")
	}
	if _, readErr := m.GetByDate(note.Date); !errors.Is(readErr, ErrLocked) {
		t.Fatalf("locked read: %v", readErr)
	}
	if _, readErr := m.GetByDate("2024-01-02"); !errors.Is(readErr, ErrLocked) {
		t.Fatalf("new locked page: %v", readErr)
	}
	before := readTestFile(t, filepath.Join(m.storageDir, note.Date+".json"))
	if saveErr := m.Save(note); !errors.Is(saveErr, ErrLocked) {
		t.Fatalf("locked save: %v", saveErr)
	}
	if !bytes.Equal(before, readTestFile(t, filepath.Join(m.storageDir, note.Date+".json"))) {
		t.Fatal("locked save changed file")
	}
	if appendErr := m.Append(note.Date, "Updates", "test-agent", []string{"public bullet"}); appendErr != nil {
		t.Fatal(appendErr)
	}
	shared, err := m.ReadShared(note.Date)
	if err != nil || !shared.HasPrivate || shared.Legacy || !strings.Contains(shared.Content, "public bullet") || strings.Contains(shared.Content, note.Content) {
		t.Fatalf("shared: %+v %v", shared, err)
	}
	path, err := m.SealRecovery(note.Date, "unsaved private recovery")
	if err != nil {
		t.Fatal(err)
	}
	sealed := readTestFile(t, path)
	if bytes.Contains(sealed, []byte("unsaved private recovery")) {
		t.Fatal("plaintext recovery")
	}
	plain, err := decryptAge(sealed, identity, maxPrivateSize)
	if err != nil {
		t.Fatal(err)
	}
	var recovered privateDocument
	if err := strictJSON(plain, &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Date != note.Date || recovered.Content != "unsaved private recovery" {
		t.Fatalf("recovery: %+v", recovered)
	}
	if m.IsUnlocked() {
		t.Fatal("recovery unlocked identity")
	}
	assertNoSaveTemps(t, m)
}

func TestPrivacyMetadataFailClosed(t *testing.T) {
	for _, mode := range []string{"missing", "malformed", "unknown", "version", "recipient", "wrapped", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			m := privateTestManager(t)
			note := &Scratchpad{Date: "2024-01-01", Content: "never fall back"}
			if err := m.Save(note); err != nil {
				t.Fatal(err)
			}
			metadataPath := filepath.Join(m.storageDir, "privacy.json")
			before := readTestFile(t, filepath.Join(m.storageDir, note.Date+".json"))
			var meta map[string]any
			if err := json.Unmarshal(readTestFile(t, metadataPath), &meta); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "missing":
				if err := os.Remove(metadataPath); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				if err := os.WriteFile(metadataPath, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				meta["future"] = true
				writeJSON(t, metadataPath, meta)
			case "version":
				meta["version"] = 99
				writeJSON(t, metadataPath, meta)
			case "recipient":
				meta["recipient"] = "invalid"
				writeJSON(t, metadataPath, meta)
			case "wrapped":
				meta["wrapped_identity"] = "invalid"
				writeJSON(t, metadataPath, meta)
			case "replaced":
				meta["wrapped_identity"] = base64.StdEncoding.EncodeToString([]byte("age-encryption.org/v1\n-> scrypt invalid\n"))
				writeJSON(t, metadataPath, meta)
			}
			if _, err := m.PrivacyEnabled(); err == nil {
				t.Fatal("bad metadata accepted")
			}
			if m.IsUnlocked() {
				t.Fatal("cached identity accepted changed metadata")
			}
			if err := m.Save(note); err == nil {
				t.Fatal("save allowed plaintext fallback")
			}
			if err := m.Save(&Scratchpad{Date: "2024-01-02", Content: "new secret"}); err == nil {
				t.Fatal("new page allowed plaintext fallback")
			}
			if err := m.Unlock(fixturePassphrase); err == nil {
				t.Fatal("bad metadata unlocked")
			}
			if !bytes.Equal(before, readTestFile(t, filepath.Join(m.storageDir, note.Date+".json"))) {
				t.Fatal("failed operation changed ciphertext")
			}
			if _, err := os.Stat(filepath.Join(m.storageDir, "2024-01-02.json")); !os.IsNotExist(err) {
				t.Fatal("failed save created a page")
			}
		})
	}
}

func TestScryptWorkFactorBound(t *testing.T) {
	m := privateTestManager(t)
	path := filepath.Join(m.storageDir, "privacy.json")
	var meta privacyMetadata
	if err := json.Unmarshal(readTestFile(t, path), &meta); err != nil {
		t.Fatal(err)
	}
	wrapped, err := decodePayload(meta.WrappedIdentity, maxMetadataSize)
	if err != nil {
		t.Fatal(err)
	}
	wrapped = bytes.Replace(wrapped, []byte(" 1\n"), []byte(" 30\n"), 1)
	meta.WrappedIdentity = base64.StdEncoding.EncodeToString(wrapped)
	writeJSON(t, path, meta)
	start := time.Now()
	if err := m.Unlock(fixturePassphrase); err == nil || !strings.Contains(err.Error(), "work factor") {
		t.Fatalf("expected bounded work factor error, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("oversized scrypt work was performed")
	}
}

func TestInterruptedMigrationResumesWithoutReplacingIdentity(t *testing.T) {
	m := privateTestManager(t)
	metadata := readTestFile(t, filepath.Join(m.storageDir, "privacy.json"))
	first := legacyDocument{Date: "2024-01-01", Content: "first secret", Created: time.Now(), Modified: time.Now(), AppliedTemplates: []string{"private-id"}}
	second := legacyDocument{Date: "2024-01-02", Content: "second secret", Created: time.Now(), Modified: time.Now()}
	writeJSON(t, filepath.Join(m.storageDir, first.Date+".json"), first)
	if err := os.WriteFile(filepath.Join(m.storageDir, second.Date+".json"), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.EnablePrivacy(fixturePassphrase); err == nil {
		t.Fatal("expected interrupted migration")
	}
	if enabled, err := m.PrivacyEnabled(); err != nil || !enabled {
		t.Fatalf("partial store not enabled: %v %v", enabled, err)
	}
	firstCipher := readTestFile(t, filepath.Join(m.storageDir, first.Date+".json"))
	if bytes.Contains(firstCipher, []byte(first.Content)) {
		t.Fatal("first page was not migrated")
	}
	writeJSON(t, filepath.Join(m.storageDir, second.Date+".json"), second)
	if err := m.Append(second.Date, "Updates", "agent", []string{"public"}); err == nil {
		t.Fatal("append rewrote mixed legacy plaintext")
	}
	if err := m.Save(&Scratchpad{Date: "2024-01-03", Content: "future secret"}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(readTestFile(t, filepath.Join(m.storageDir, "2024-01-03.json")), []byte("future secret")) {
		t.Fatal("partial migration allowed plaintext save")
	}
	status, err := m.PrivacyStatus()
	if err != nil || len(status.PlaintextDates) != 1 || status.PlaintextDates[0] != second.Date {
		t.Fatalf("mixed status: %+v %v", status, err)
	}
	if enableErr := m.EnablePrivacy(fixturePassphrase); enableErr != nil {
		t.Fatal(enableErr)
	}
	if !bytes.Equal(metadata, readTestFile(t, filepath.Join(m.storageDir, "privacy.json"))) {
		t.Fatal("resume replaced identity")
	}
	if !bytes.Equal(firstCipher, readTestFile(t, filepath.Join(m.storageDir, first.Date+".json"))) {
		t.Fatal("resume rewrote already migrated page")
	}
	got, err := m.GetByDate(first.Date)
	if err != nil || got.Content != first.Content || !got.Modified.Equal(first.Modified) || strings.Join(got.AppliedTemplates, ",") != "private-id" {
		t.Fatalf("migration metadata lost: %+v %v", got, err)
	}
}

func TestEncryptedMixedLegacySaveAndDurability(t *testing.T) {
	m := privateTestManager(t)
	writeJSON(t, filepath.Join(m.storageDir, "2024-01-01.json"), legacyDocument{Date: "2024-01-01", Content: "legacy secret"})
	note, err := m.GetByDate("2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	note.Content = "edited secret"
	syncErr := errors.New("synthetic directory sync failure")
	if err := m.save(note, func(string) error {
		got, err := m.GetByDate(note.Date)
		if err != nil || got.Content != note.Content || !got.Modified.Equal(note.Modified) {
			t.Fatalf("publication: %+v %v", got, err)
		}
		return syncErr
	}); !errors.Is(err, ErrDurabilityUnconfirmed) || !errors.Is(err, syncErr) {
		t.Fatalf("durability error: %v", err)
	}
	data := readTestFile(t, filepath.Join(m.storageDir, note.Date+".json"))
	if bytes.Contains(data, []byte(note.Content)) {
		t.Fatal("mixed legacy save wrote plaintext")
	}
	if note.revision != sha256.Sum256(data) {
		t.Fatal("published revision not advanced")
	}
	note.Content = "next edit"
	if err := m.Save(note); err != nil {
		t.Fatal(err)
	}
}

func TestPrivacyRejectsOversizedInputs(t *testing.T) {
	m := privateTestManager(t)
	if err := m.Unlock(bytes.Repeat([]byte("x"), maxPassphraseSize+1)); err == nil {
		t.Fatal("oversized passphrase accepted")
	}
	if _, err := m.SealRecovery("2024-01-01", strings.Repeat("x", maxPrivateSize+1)); err == nil {
		t.Fatal("oversized recovery accepted")
	}
	if _, err := m.SealRecovery("../outside", "secret"); err == nil {
		t.Fatal("invalid recovery date accepted")
	}
	if err := os.WriteFile(filepath.Join(m.storageDir, "privacy.json"), bytes.Repeat([]byte("x"), maxMetadataSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PrivacyEnabled(); err == nil {
		t.Fatal("oversized metadata accepted")
	}
}
