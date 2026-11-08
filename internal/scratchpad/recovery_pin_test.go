package scratchpad

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"filippo.io/age"
)

func TestPinRecoveryPublicOnlyWhileLocked(t *testing.T) {
	original := privateTestManager(t)
	identity, err := original.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	original.Lock()
	m, err := NewManagerAt(original.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(m.storageDir, "privacy.json")
	beforeMetadata := readTestFile(t, metadataPath)
	beforeEntries, err := os.ReadDir(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := m.PinRecovery()
	if err != nil {
		t.Fatal(err)
	}
	afterEntries, err := os.ReadDir(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeEntries, afterEntries) || !bytes.Equal(beforeMetadata, readTestFile(t, metadataPath)) {
		t.Fatal("pinning changed store contents")
	}
	m.keyMu.RLock()
	publicOnly := m.identity == nil && m.recoveryRecipient == identity.Recipient().String()
	m.keyMu.RUnlock()
	if !publicOnly {
		t.Fatal("locked pin did not cache only the public recipient")
	}
	m.Lock()
	if err := os.Remove(metadataPath); err != nil {
		t.Fatal(err)
	}
	// Neither the session nor the manager's existing public cache needs metadata.
	for _, sealer := range []func(string, string) (string, error){seal, m.SealRecovery} {
		path, err := sealer("2024-02-29", "synthetic unsaved α\r\n\tno final newline")
		if err != nil {
			t.Fatal(err)
		}
		assertPinnedRecovery(t, path, identity, "2024-02-29", "synthetic unsaved α\r\n\tno final newline")
	}
	m.keyMu.RLock()
	stillLocked := m.identity == nil
	m.keyMu.RUnlock()
	if !stillLocked {
		t.Fatal("public recovery restored a private identity")
	}
}

func TestPinRecoveryImmutableAcrossReplacementAndRepinning(t *testing.T) {
	m := privateTestManager(t)
	originalDir := m.storageDir
	originalIdentity, err := m.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	sealOriginal, err := m.PinRecovery()
	if err != nil {
		t.Fatal(err)
	}
	other := privateTestManager(t)
	otherIdentity, err := other.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	m.Lock()
	if writeErr := os.WriteFile(filepath.Join(originalDir, "privacy.json"), readTestFile(t, filepath.Join(other.storageDir, "privacy.json")), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// A new session still uses the manager's old pin until a successful unlock.
	sealBeforeUnlock, err := m.PinRecovery()
	if err != nil {
		t.Fatal(err)
	}
	if unlockErr := m.Unlock(fixturePassphrase); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	sealOther, err := m.PinRecovery()
	if err != nil {
		t.Fatal(err)
	}
	m.Lock()
	// Storage binding is also a value snapshot, not a captured manager pointer.
	m.storageDir = t.TempDir()
	const date = "2026-10-01"
	const content = "synthetic immutable recovery recipient"
	for _, tc := range []struct {
		name     string
		seal     func(string, string) (string, error)
		identity *age.X25519Identity
		wrongKey *age.X25519Identity
		dir      string
	}{
		{"original session", sealOriginal, originalIdentity, otherIdentity, originalDir},
		{"cached session before unlock", sealBeforeUnlock, originalIdentity, otherIdentity, originalDir},
		{"repinned session", sealOther, otherIdentity, originalIdentity, originalDir},
		{"manager after repinning", m.SealRecovery, otherIdentity, originalIdentity, m.storageDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, err := tc.seal(date, content)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Dir(path) != tc.dir {
				t.Fatal("recovery was redirected to a different directory")
			}
			assertPinnedRecovery(t, path, tc.identity, date, content)
			if _, err := decryptAge(readTestFile(t, path), tc.wrongKey, maxPrivateSize); err == nil {
				t.Fatal("recovery was decryptable with the wrong session key")
			}
		})
	}
}

func TestPinRecoveryValidationAndFailureOrdering(t *testing.T) {
	m := privateTestManager(t)
	seal, err := m.PinRecovery()
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := NewManagerAt(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	beforeFingerprint := fresh.observedPrivacy
	for _, tc := range []struct {
		name, date, content, want string
	}{
		{"date before content", "../outside", string([]byte{0xff}), ""},
		{"invalid UTF-8", "2024-02-29", string([]byte{0xff}), "invalid recovery content or size"},
		{"content limit", "2024-02-29", strings.Repeat("x", maxPrivateSize+1), "invalid recovery content or size"},
		{"encoded limit", "2024-02-29", strings.Repeat("x", maxPrivateSize), "recovery content too large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.want
			if want == "" {
				want = validateDate(tc.date).Error()
			}
			for _, sealer := range []func(string, string) (string, error){seal, fresh.SealRecovery} {
				path, err := sealer(tc.date, tc.content)
				if path != "" || err == nil || err.Error() != want {
					t.Fatalf("validation = %q, %v; want %q", path, err, want)
				}
			}
			if tc.name != "encoded limit" && (fresh.recoveryRecipient != "" || fresh.observedPrivacy != beforeFingerprint) {
				t.Fatal("invalid input read metadata or populated the manager pin")
			}
		})
	}
	// The serialized-size check remains after recipient acquisition, as before.
	if fresh.recoveryRecipient == "" {
		t.Fatal("encoded-size rejection did not preserve manager pin behavior")
	}
	for _, malformed := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing metadata", true: "malformed metadata"}[malformed], func(t *testing.T) {
			m, err := NewManagerAt(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if malformed {
				if writeErr := os.WriteFile(filepath.Join(m.storageDir, "privacy.json"), []byte("{invalid synthetic metadata"), 0o600); writeErr != nil {
					t.Fatal(writeErr)
				}
			}
			seal, err := m.PinRecovery()
			if seal != nil || err == nil || m.recoveryRecipient != "" {
				t.Fatalf("unpinned manager = nonnil sealer %v, error %v", seal != nil, err)
			}
			path, err := m.SealRecovery("../outside", "synthetic")
			if path != "" || err == nil || err.Error() != validateDate("../outside").Error() {
				t.Fatalf("input validation did not precede metadata failure: %q, %v", path, err)
			}
		})
	}
}

func TestPinRecoverySharedPublicationSemantics(t *testing.T) {
	m := privateTestManager(t)
	identity, err := m.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	m.Lock()
	syncErr := errors.New("synthetic recovery directory sync failure")
	path, err := sealValidatedRecovery(m.storageDir, identity.Recipient(), "2024-02-29", "synthetic durable recovery", func(dir string) error {
		if dir != m.storageDir {
			t.Error("synced unexpected directory")
		}
		return syncErr
	})
	if path == "" || !errors.Is(err, ErrDurabilityUnconfirmed) || !errors.Is(err, syncErr) {
		t.Fatalf("published recovery = %q, %v", path, err)
	}
	assertPinnedRecovery(t, path, identity, "2024-02-29", "synthetic durable recovery")
	path, err = sealValidatedRecovery(filepath.Join(m.storageDir, "missing"), identity.Recipient(), "2024-02-29", "synthetic unpublished recovery", func(string) error {
		t.Error("sync called before publication")
		return nil
	})
	if path != "" || err == nil || errors.Is(err, ErrDurabilityUnconfirmed) {
		t.Fatalf("unpublished recovery = %q, %v", path, err)
	}
	assertNoSaveTemps(t, m)
}

func TestPinRecoveryConcurrentSealingAndLock(t *testing.T) {
	m := privateTestManager(t)
	identity, err := m.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	seal, err := m.PinRecovery()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	paths := make(chan string, 8)
	errs := make(chan error, 9)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := seal("2024-02-29", "synthetic concurrent recovery")
			if err != nil {
				errs <- err
				return
			}
			paths <- path
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.Lock()
		if err := m.Unlock(fixturePassphrase); err != nil {
			errs <- err
		}
		m.Lock()
	}()
	wg.Wait()
	close(paths)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	seen := make(map[string]bool)
	for path := range paths {
		if seen[path] {
			t.Fatal("concurrent publications reused a path")
		}
		seen[path] = true
		assertPinnedRecovery(t, path, identity, "2024-02-29", "synthetic concurrent recovery")
	}
	if len(seen) != 8 {
		t.Fatalf("published %d recoveries, want 8", len(seen))
	}
}

func assertPinnedRecovery(t *testing.T, path string, identity *age.X25519Identity, date, content string) {
	t.Helper()
	ciphertext := readTestFile(t, path)
	if bytes.Contains(ciphertext, []byte(content)) || !bytes.HasPrefix(ciphertext, []byte("age-encryption.org/v1\n")) {
		t.Fatal("recovery is not age ciphertext without plaintext")
	}
	plain, err := decryptAge(ciphertext, identity, maxPrivateSize)
	if err != nil {
		t.Fatal(err)
	}
	var document privateDocument
	if decodeErr := json.Unmarshal(plain, &document); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if document.Version != 2 || document.Date != date || document.Content != content || document.Created.IsZero() || !document.Created.Equal(document.Modified) {
		t.Fatal("recovery format or exact content changed")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("recovery permissions = %v", info.Mode().Perm())
	}
}
