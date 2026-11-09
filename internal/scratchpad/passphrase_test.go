package scratchpad

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
)

// These tests are deliberately serial: production wrapping uses factor 18.
// Only synthetic input fixtures use a cheap wrapper; the operation under test
// always derives and verifies at the production work factor.
func passphraseFixture(t *testing.T, current []byte) *Manager {
	t.Helper()
	m := privateTestManager(t)
	identity, err := m.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := age.NewScryptRecipient(string(current))
	if err != nil {
		t.Fatal(err)
	}
	recipient.SetWorkFactor(1)
	wrapped, err := encryptAge([]byte(identity.String()), recipient)
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(m.storageDir, "privacy.json"), privacyMetadata{Version: 1, Recipient: identity.Recipient().String(), WrappedIdentity: base64.StdEncoding.EncodeToString(wrapped)})
	if err := m.Unlock(current); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Lock)
	return m
}

func passphraseFiles(t *testing.T, m *Manager) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".lock") {
			continue
		}
		if entry.Type().IsRegular() {
			files[entry.Name()] = readTestFile(t, filepath.Join(m.storageDir, entry.Name()))
		}
	}
	return files
}

func assertPassphraseFiles(t *testing.T, m *Manager, before map[string][]byte, metadataChanged bool) {
	t.Helper()
	after := passphraseFiles(t, m)
	if len(after) != len(before) {
		t.Fatalf("file count changed: %d to %d", len(before), len(after))
	}
	for name, data := range before {
		if metadataChanged && name == "privacy.json" {
			if bytes.Equal(data, after[name]) {
				t.Fatal("metadata was not rewrapped")
			}
			continue
		}
		if !bytes.Equal(data, after[name]) {
			t.Fatalf("unexpected change to %s", name)
		}
	}
	assertNoSaveTemps(t, m)
}

func TestChangePassphraseExactBytesAndRetainedKey(t *testing.T) {
	current := []byte("  synthetic café 雪  ")
	replacement := []byte("  synthetic cafe\u0301 雪  ")
	currentCopy, replacementCopy := bytes.Clone(current), bytes.Clone(replacement)
	m := passphraseFixture(t, current)
	identity, identityErr := m.unlockedIdentity()
	if identityErr != nil {
		t.Fatal(identityErr)
	}
	const date = "2026-03-04"
	const text = "  synthetic private\r\n\t雪 e\u0301\n\n"
	if err := m.Save(&Scratchpad{Date: date, Content: text, AppliedTemplates: []string{"synthetic-template"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Append(date, "Updates", "synthetic-helper", []string{"shared synthetic update"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Append("2026-03-05", "Updates", "synthetic-helper", []string{"shared-only day"}); err != nil {
		t.Fatal(err)
	}
	recovery, err := m.SealRecovery(date, text)
	if err != nil {
		t.Fatal(err)
	}
	before := passphraseFiles(t, m)
	stale, err := NewManagerAt(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	defer stale.Lock()
	if unlockErr := stale.Unlock(current); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	staleNote, err := stale.GetByDate(date)
	if err != nil {
		t.Fatal(err)
	}
	// A cached unlocked identity is not authentication of the supplied secret.
	for _, incorrect := range [][]byte{[]byte("wrong synthetic password"), bytes.TrimSpace(current), replacement} {
		if published, changeErr := m.ChangePassphrase(incorrect, replacement); published || changeErr == nil || m.IsUnlocked() {
			t.Fatalf("wrong current passphrase: published=%v err=%v", published, changeErr)
		}
		assertPassphraseFiles(t, m, before, false)
	}
	if unlockErr := m.Unlock(current); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	published, err := m.ChangePassphrase(current, replacement)
	if !published || err != nil || m.IsUnlocked() {
		t.Fatalf("change: published=%v err=%v", published, err)
	}
	if !bytes.Equal(current, currentCopy) || !bytes.Equal(replacement, replacementCopy) {
		t.Fatal("operation modified caller-owned secret bytes")
	}
	assertPassphraseFiles(t, m, before, true)
	metadata, _, err := m.readMetadata()
	if err != nil || metadata.Recipient != identity.Recipient().String() || metadata.Version != 1 || metadata.State != "" {
		t.Fatalf("changed identity or policy: %v", err)
	}
	wrapped, err := decodePayload(metadata.WrappedIdentity, maxMetadataSize)
	if err != nil || !bytes.Contains(wrapped, []byte(" 18\n")) {
		t.Fatalf("replacement did not use the production work factor: %v", err)
	}
	for _, secret := range [][]byte{current, replacement, []byte(identity.String()), []byte(text)} {
		if bytes.Contains(readTestFile(t, filepath.Join(m.storageDir, "privacy.json")), secret) {
			t.Fatal("metadata contains an unwrapped synthetic secret")
		}
	}
	if stale.IsUnlocked() {
		t.Fatal("old manager retained ordinary access across fingerprint change")
	}
	if _, loadErr := stale.GetByDate(date); !errors.Is(loadErr, ErrLocked) {
		t.Fatalf("stale read: %v", loadErr)
	}
	if saveErr := stale.Save(staleNote); !errors.Is(saveErr, ErrLocked) {
		t.Fatalf("stale save: %v", saveErr)
	}
	// Recovery is deliberately not revoked: the stale manager's public pin
	// still seals to the same retained identity, even after Lock.
	stale.Lock()
	staleRecovery, err := stale.SealRecovery(date, text)
	if err != nil {
		t.Fatal(err)
	}
	for _, incorrect := range [][]byte{current, bytes.TrimSpace(replacement), []byte("  synthetic café 雪  ")} {
		if unlockErr := m.Unlock(incorrect); unlockErr == nil {
			t.Fatal("non-exact replacement passphrase unlocked the new metadata")
		}
	}
	if unlockErr := m.Unlock(replacement); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	gotIdentity, err := m.unlockedIdentity()
	if err != nil || gotIdentity.String() != identity.String() {
		t.Fatalf("rewrap changed private key: %v", err)
	}
	got, err := m.GetByDate(date)
	if err != nil || got.Content != text || strings.Join(got.AppliedTemplates, ",") != "synthetic-template" {
		t.Fatalf("private bytes or metadata changed: %v", err)
	}
	for _, path := range []string{recovery, staleRecovery} {
		recovered, recoveryErr := m.ReadRecovery(path)
		if recoveryErr != nil || recovered.Content != text {
			t.Fatalf("retained recovery unreadable: %v", recoveryErr)
		}
	}
	// An explicitly retained old metadata backup continues to unlock the same
	// daily ciphertext and recovery; no backup is created by the operation.
	backup := setupTestManager(t)
	defer backup.Lock()
	for name, data := range before {
		if writeErr := os.WriteFile(filepath.Join(backup.storageDir, name), data, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if unlockErr := backup.Unlock(current); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	old, err := backup.GetByDate(date)
	if err != nil || old.Content != text {
		t.Fatalf("old metadata backup lost access: %v", err)
	}
	if _, recoveryErr := backup.ReadRecovery(filepath.Base(recovery)); recoveryErr != nil {
		t.Fatal(recoveryErr)
	}
	// Rewrapping and failed authentication must not silently adopt a public
	// recipient substituted on disk, even if it can be authenticated afresh.
	alternate := privateTestManager(t)
	defer alternate.Lock()
	if writeErr := os.WriteFile(filepath.Join(m.storageDir, "privacy.json"), readTestFile(t, filepath.Join(alternate.storageDir, "privacy.json")), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if published, changeErr := m.ChangePassphrase([]byte("wrong synthetic password"), replacement); published || changeErr == nil {
		t.Fatalf("substitution authentication: %v, %v", published, changeErr)
	}
	// Even a successful fresh authentication of the metadata now on disk must
	// not change this live manager's previously pinned recovery recipient.
	if published, changeErr := m.ChangePassphrase(fixturePassphrase, replacement); !published || changeErr != nil || m.IsUnlocked() {
		t.Fatalf("authenticated substituted metadata rewrap: %v, %v", published, changeErr)
	}
	pinnedRecovery, err := m.SealRecovery(date, text)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decryptAge(readTestFile(t, pinnedRecovery), identity, maxPrivateSize)
	if err != nil || !bytes.Contains(plain, []byte("synthetic private")) {
		t.Fatalf("recovery pin was redirected: %v", err)
	}
}

func TestChangePassphraseInputRejection(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	identity, err := m.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	before := passphraseFiles(t, m)
	for _, invalid := range [][]byte{nil, {}, bytes.Repeat([]byte("x"), maxPassphraseSize+1), {0xff}, []byte("prefix\nrest"), []byte("prefix\rrest"), []byte("prefix\trest"), []byte("prefix\x00rest"), []byte("prefix\u2028rest"), []byte("prefix\u2029rest")} {
		for _, invalidCurrent := range []bool{true, false} {
			// Seed an unlocked cache without a KDF; validation must still drop it.
			m.keyMu.Lock()
			m.identity = identity
			m.keyMu.Unlock()
			current, replacement := fixturePassphrase, []byte("synthetic replacement")
			if invalidCurrent {
				current = invalid
			} else {
				replacement = invalid
			}
			published, err := m.ChangePassphrase(current, replacement)
			if published || err == nil || m.IsUnlocked() {
				t.Fatalf("invalid input: published=%v err=%v", published, err)
			}
			assertPassphraseFiles(t, m, before, false)
		}
	}
}

func TestChangePassphraseRejectsUnsafePolicy(t *testing.T) {
	for _, scenario := range []string{"missing", "never-enabled", "disabled", "decrypting", "unknown", "unknown-state", "corrupt", "unknown-field", "recipient-mismatch", "corrupt-identity", "excessive-kdf", "legacy", "empty-legacy", "malformed-date", "unknown-date", "uppercase", "date-directory"} {
		t.Run(scenario, func(t *testing.T) {
			m := privateTestManager(t)
			defer m.Lock()
			path := filepath.Join(m.storageDir, "privacy.json")
			metadata, _, metadataErr := m.readMetadata()
			if metadataErr != nil {
				t.Fatal(metadataErr)
			}
			switch scenario {
			case "missing", "never-enabled":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if scenario == "never-enabled" {
					m = &Manager{storageDir: m.storageDir}
				}
			case "disabled", "decrypting":
				metadata.Version, metadata.State = 2, scenario
				writeJSON(t, path, metadata)
			case "unknown":
				metadata.Version = 99
				writeJSON(t, path, metadata)
			case "unknown-state":
				metadata.Version, metadata.State = 2, "rotating"
				writeJSON(t, path, metadata)
			case "corrupt", "unknown-field":
				data := []byte("{synthetic invalid metadata")
				if scenario == "unknown-field" {
					data = append([]byte(`{"future":true,`), readTestFile(t, path)[1:]...)
				}
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			case "recipient-mismatch":
				other, err := age.GenerateX25519Identity()
				if err != nil {
					t.Fatal(err)
				}
				metadata.Recipient = other.Recipient().String()
				writeJSON(t, path, metadata)
			case "corrupt-identity", "excessive-kdf":
				wrapped, err := decodePayload(metadata.WrappedIdentity, maxMetadataSize)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "excessive-kdf" {
					wrapped = bytes.Replace(wrapped, []byte(" 1\n"), []byte(" 19\n"), 1)
				} else {
					wrapped[len(wrapped)-1] ^= 1
				}
				metadata.WrappedIdentity = base64.StdEncoding.EncodeToString(wrapped)
				writeJSON(t, path, metadata)
			default:
				datePath := filepath.Join(m.storageDir, "2026-03-04.json")
				switch scenario {
				case "legacy", "empty-legacy":
					content := "synthetic unmigrated private text"
					if scenario == "empty-legacy" {
						content = ""
					}
					writeJSON(t, datePath, legacyDocument{Date: "2026-03-04", Content: content})
				case "malformed-date", "unknown-date", "uppercase":
					data := []byte("{synthetic invalid date")
					if scenario == "unknown-date" {
						data = []byte(`{"version":99,"date":"2026-03-04"}`)
					}
					if scenario == "uppercase" {
						datePath = strings.TrimSuffix(datePath, ".json") + ".JSON"
					}
					if err := os.WriteFile(datePath, data, 0o600); err != nil {
						t.Fatal(err)
					}
				case "date-directory":
					if err := os.Mkdir(datePath, 0o700); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := passphraseFiles(t, m)
			calls := 0
			published, err := m.changePassphrase(fixturePassphrase, []byte("synthetic replacement"), func(string) error { calls++; return nil })
			if published || err == nil || calls != 0 || m.IsUnlocked() {
				t.Fatalf("unsafe policy: published=%v err=%v syncs=%d", published, err, calls)
			}
			if scenario == "decrypting" && !errors.Is(err, ErrPrivacyDecrypting) {
				t.Fatalf("decrypting error: %v", err)
			}
			assertPassphraseFiles(t, m, before, false)
		})
	}
}

func TestChangePassphrasePublicationAndRetry(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	before := passphraseFiles(t, m)
	// A synthetic directory in place of the lock file deterministically stops
	// publication, without permission assumptions or a broken real filesystem.
	lockPath := filepath.Join(m.storageDir, ".sp-privacy.lock")
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	replacement := []byte("synthetic replacement for durability")
	published, err := m.ChangePassphrase(fixturePassphrase, replacement)
	if published || err == nil || errors.Is(err, ErrDurabilityUnconfirmed) || m.IsUnlocked() {
		t.Fatalf("before publication: published=%v err=%v", published, err)
	}
	assertPassphraseFiles(t, m, before, false)
	if removeErr := os.Remove(lockPath); removeErr != nil {
		t.Fatal(removeErr)
	}
	injected := errors.New("synthetic sync failure")
	calls := 0
	published, err = m.changePassphrase(fixturePassphrase, replacement, func(dir string) error {
		calls++
		if dir != m.storageDir {
			t.Fatalf("unexpected sync directory %q", dir)
		}
		if bytes.Equal(before["privacy.json"], readTestFile(t, filepath.Join(dir, "privacy.json"))) {
			t.Fatal("sync happened before replacement")
		}
		return injected
	})
	if !published || !errors.Is(err, ErrDurabilityUnconfirmed) || !errors.Is(err, injected) || calls != 1 || m.IsUnlocked() {
		t.Fatalf("after publication: published=%v err=%v calls=%d", published, err, calls)
	}
	assertPassphraseFiles(t, m, before, true)
	after := passphraseFiles(t, m)
	fresh, err := NewManagerAt(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Lock()
	if published, err := fresh.ChangePassphrase(fixturePassphrase, replacement); published || err == nil {
		t.Fatalf("old secret retry: published=%v err=%v", published, err)
	}
	assertPassphraseFiles(t, fresh, after, false)
	// Same-secret changes are allowed, but are real randomized publications;
	// retry authenticates the replacement now on disk and never rolls back.
	if published, err := fresh.ChangePassphrase(replacement, replacement); !published || err != nil || fresh.IsUnlocked() {
		t.Fatalf("actual secret retry: published=%v err=%v", published, err)
	}
	assertPassphraseFiles(t, fresh, after, true)
	if err := fresh.Unlock(replacement); err != nil {
		t.Fatal(err)
	}
}

func TestChangePassphraseRevalidatesUnderStoreLock(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	original := readTestFile(t, filepath.Join(m.storageDir, "privacy.json"))
	lock, lockErr := lockNote(m.storageDir, "privacy")
	if lockErr != nil {
		t.Fatal(lockErr)
	}
	// Preparation must finish even while another writer owns the store lock.
	type preparation struct {
		data        []byte
		fingerprint [sha256.Size]byte
		err         error
	}
	ready := make(chan preparation, 1)
	go func() {
		data, fingerprint, err := m.preparePassphraseChange(fixturePassphrase, []byte("synthetic race replacement"))
		ready <- preparation{data, fingerprint, err}
	}()
	var prepared preparation
	select {
	case prepared = <-ready:
	case <-time.After(30 * time.Second):
		_ = lock.Close()
		t.Fatal("preparation blocked behind store lock")
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if prepared.err != nil {
		t.Fatal(prepared.err)
	}
	for _, scenario := range []string{"changed-fingerprint", "disabled", "decrypting", "missing", "corrupt", "retained-key-aba", "new-corrupt-date", "shared-only"} {
		t.Run(scenario, func(t *testing.T) {
			other := setupTestManager(t)
			defer other.Lock()
			if err := os.WriteFile(filepath.Join(other.storageDir, "privacy.json"), original, 0o600); err != nil {
				t.Fatal(err)
			}
			metadata, _, metadataErr := other.readMetadata()
			if metadataErr != nil {
				t.Fatal(metadataErr)
			}
			path := filepath.Join(other.storageDir, "privacy.json")
			switch scenario {
			case "changed-fingerprint":
				// Even semantically identical metadata is a concurrent change.
				if err := os.WriteFile(path, append(bytes.Clone(original), '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
			case "disabled", "decrypting":
				metadata.Version, metadata.State = 2, scenario
				writeJSON(t, path, metadata)
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(path, []byte("{synthetic corruption"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "retained-key-aba":
				// Identical active metadata after reversal/reactivation is not
				// sufficient if any date was left plaintext in the meantime.
				writeJSON(t, filepath.Join(other.storageDir, "2026-03-04.json"), legacyDocument{Date: "2026-03-04"})
			case "new-corrupt-date":
				if err := os.WriteFile(filepath.Join(other.storageDir, "2026-03-04.json"), []byte("{synthetic corruption"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "shared-only":
				if err := other.Append("2026-03-04", "Updates", "synthetic-helper", []string{"concurrent shared addition"}); err != nil {
					t.Fatal(err)
				}
			}
			before := passphraseFiles(t, other)
			// Publication never takes a date lock; it needs only the store
			// lock, avoiding lock-order inversion with ordinary store writers.
			dateLock, err := lockNote(other.storageDir, "2026-03-04")
			if err != nil {
				t.Fatal(err)
			}
			defer dateLock.Close()
			calls := 0
			published, err := other.publishPassphraseChange(prepared.data, prepared.fingerprint, func(dir string) error { calls++; return syncDirectory(dir) })
			if scenario == "shared-only" {
				if !published || err != nil || calls != 1 {
					t.Fatalf("safe concurrent append: published=%v err=%v calls=%d", published, err, calls)
				}
				assertPassphraseFiles(t, other, before, true)
			} else {
				if published || err == nil || calls != 0 {
					t.Fatalf("concurrent change: published=%v err=%v calls=%d", published, err, calls)
				}
				if scenario == "changed-fingerprint" && !errors.Is(err, ErrPrivacyPolicyChanged) {
					t.Fatalf("fingerprint mismatch: %v", err)
				}
				assertPassphraseFiles(t, other, before, false)
			}
		})
	}
	// A cooperating writer can finish a metadata replacement while preparation
	// runs, but publication must wait for its store lock and reread afterwards.
	storeLock, err := lockNote(m.storageDir, "privacy")
	if err != nil {
		t.Fatal(err)
	}
	type publication struct {
		published bool
		err       error
	}
	done := make(chan publication, 1)
	go func() {
		published, err := m.publishPassphraseChange(prepared.data, prepared.fingerprint, syncDirectory)
		done <- publication{published, err}
	}()
	if err := os.WriteFile(filepath.Join(m.storageDir, "privacy.json"), append(bytes.Clone(original), '\n'), 0o600); err != nil {
		_ = storeLock.Close()
		t.Fatal(err)
	}
	if err := storeLock.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.published || !errors.Is(result.err, ErrPrivacyPolicyChanged) {
			t.Fatalf("store lock fence: published=%v err=%v", result.published, result.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("publication did not finish after store lock release")
	}
	// The helper split is not a metadata-format extension.
	var pending privacyMetadata
	if err := json.Unmarshal(prepared.data, &pending); err != nil || pending.Version != 1 || pending.State != "" {
		t.Fatalf("prepared metadata format: %v", err)
	}
}
