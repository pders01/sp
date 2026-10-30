package scratchpad

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

func TestAppendRejectsCaseAliasesWithoutDiscardingCiphertext(t *testing.T) {
	m := privateTestManager(t)
	const date = "2026-10-01"
	if err := m.Save(&Scratchpad{Date: date, Content: "synthetic private content"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.storageDir, date+".json")
	original := readTestFile(t, path)
	m.Lock()
	for _, alias := range []string{
		`"Private":null`,
		`"Version":99`,
		`"DATE":"2026-10-01"`,
		`"Agent":{"content":"replace"}`,
	} {
		t.Run(alias, func(t *testing.T) {
			// Valid JSON with a case alias after the original canonical fields.
			modified := append(bytes.TrimSpace(original[:len(bytes.TrimSpace(original))-1]), []byte(","+alias+"}")...)
			if err := os.WriteFile(path, modified, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := m.Append(date, "Updates", "agent", []string{"new shared bullet"}); err == nil {
				t.Fatal("case alias was accepted")
			}
			if !bytes.Equal(modified, readTestFile(t, path)) {
				t.Fatal("rejected alias rewrote the note/ciphertext")
			}
		})
	}
}

func TestStrictJSONRejectsUnicodeCaseAliases(t *testing.T) {
	var contribution Contribution
	if err := strictJSON([]byte(`{"ſource":"spoofed"}`), &contribution); err == nil {
		t.Fatal("Unicode case-fold alias was accepted")
	}
}

func TestPassphraseCanRoundTripThroughInteractiveTextInput(t *testing.T) {
	for _, passphrase := range [][]byte{[]byte("tab\tseparated"), []byte("line\nbreak"), {0xff}} {
		if err := validatePassphrase(passphrase); err == nil {
			t.Fatal("accepted passphrase that cannot round-trip through text input")
		}
	}
	if err := validatePassphrase([]byte(strings.Repeat("é", 2048))); err != nil {
		t.Fatalf("maximum supported UTF-8 passphrase rejected: %v", err)
	}
}

func TestUnknownStoreVersionDoesNotPermitNewPlaintextSaves(t *testing.T) {
	m := setupTestManager(t)
	writeJSON(t, filepath.Join(m.storageDir, "2026-10-01.json"), map[string]any{
		"version": 99, "date": "2026-10-01", "future_payload": "opaque",
	})
	if err := m.Save(&Scratchpad{Date: "2026-10-02", Content: "private text"}); err == nil {
		t.Fatal("unsupported store version permitted a new plaintext save")
	}
}

func TestRecoveryRecipientRemainsPinnedAfterMetadataSubstitution(t *testing.T) {
	m := privateTestManager(t)
	identity, err := m.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	attacker, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(m.storageDir, "privacy.json")
	var metadata privacyMetadata
	if decodeErr := json.Unmarshal(readTestFile(t, metadataPath), &metadata); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	metadata.Recipient = attacker.Recipient().String()
	writeJSON(t, metadataPath, metadata)
	if m.IsUnlocked() {
		t.Fatal("substituted metadata should invalidate private saving")
	}
	// Public recipient pinning survives dropping the private identity too.
	m.Lock()
	path, err := m.SealRecovery("2026-10-01", "SYNTHETIC PRIVATE RECOVERY")
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := readTestFile(t, path)
	if _, decryptErr := decryptAge(ciphertext, attacker, maxPrivateSize); decryptErr == nil {
		t.Fatal("recovery was encrypted to substituted attacker key")
	}
	plaintext, err := decryptAge(ciphertext, identity, maxPrivateSize)
	if err != nil || !strings.Contains(string(plaintext), "SYNTHETIC PRIVATE RECOVERY") {
		t.Fatalf("recovery is not decryptable with original identity: %v", err)
	}
	if bytes.Contains(ciphertext, []byte("SYNTHETIC PRIVATE RECOVERY")) || bytes.Contains(ciphertext, []byte(base64.StdEncoding.EncodeToString([]byte("SYNTHETIC PRIVATE RECOVERY")))) {
		t.Fatal("recovery leaked private content")
	}
}
