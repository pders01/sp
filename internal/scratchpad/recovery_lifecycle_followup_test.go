package scratchpad

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRecoveryFollowupUnpublishedFailureRetainsPinnedRecipient(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	const date = "2024-02-29"
	if err := m.Save(&Scratchpad{Date: date, Content: "synthetic current note"}); err != nil {
		t.Fatal(err)
	}
	identity, err := m.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	originalRecovery, err := m.SealRecovery(date, "synthetic previous draft")
	if err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(m.storageDir, date+".json")
	noteBefore := readTestFile(t, notePath)
	recoveryBefore := readTestFile(t, originalRecovery)
	metadataPath := filepath.Join(m.storageDir, "privacy.json")
	metadataBefore := readTestFile(t, metadataPath)
	m.Lock()

	// A missing disposable directory deterministically fails before publication,
	// without permission assumptions or global filesystem hooks.
	store := m.storageDir
	m.storageDir = filepath.Join(t.TempDir(), "not-created")
	path, err := m.SealRecovery(date, "synthetic retry α\r\n\tno final newline")
	m.storageDir = store
	if err == nil || path != "" || errors.Is(err, ErrDurabilityUnconfirmed) {
		t.Fatalf("unpublished recovery = %q, %v", path, err)
	}
	if !bytes.Equal(noteBefore, readTestFile(t, notePath)) || !bytes.Equal(recoveryBefore, readTestFile(t, originalRecovery)) {
		t.Fatal("failed recovery publication changed existing data")
	}
	assertNoSaveTemps(t, m)

	// Losing metadata must not redirect a live editor's pinned public key,
	// even after an unsuccessful publication. A fresh manager has no such pin.
	if removeErr := os.Remove(metadataPath); removeErr != nil {
		t.Fatal(removeErr)
	}
	fresh, err := NewManagerAt(store)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Lock()
	if unpinnedPath, unpinnedErr := fresh.SealRecovery(date, "synthetic unpinned draft"); unpinnedErr == nil || unpinnedPath != "" {
		t.Fatalf("unpinned recovery without metadata = %q, %v", unpinnedPath, unpinnedErr)
	}
	const retryContent = "synthetic retry α\r\n\tno final newline"
	path, err = m.SealRecovery(date, retryContent)
	if err != nil || path == "" || path == originalRecovery {
		t.Fatalf("pinned retry = %q, %v", path, err)
	}
	ciphertext := readTestFile(t, path)
	plain, err := decryptAge(ciphertext, identity, maxPrivateSize)
	if err != nil {
		t.Fatal(err)
	}
	document, err := decodePrivateDocument(plain, date)
	if err != nil || document.Content != retryContent || bytes.Contains(ciphertext, []byte(retryContent)) {
		t.Fatalf("pinned retry did not preserve encrypted content: %v", err)
	}
	if writeErr := os.WriteFile(metadataPath, metadataBefore, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if unlockErr := fresh.Unlock(fixturePassphrase); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	got, err := fresh.ReadRecovery(path)
	if err != nil || got == nil || got.Content != retryContent {
		t.Fatalf("reopened retry = %+v, %v", got, err)
	}
	if !bytes.Equal(noteBefore, readTestFile(t, notePath)) || !bytes.Equal(recoveryBefore, readTestFile(t, originalRecovery)) || !bytes.Equal(ciphertext, readTestFile(t, path)) {
		t.Fatal("recovery lifecycle rewrote current note or recovery ciphertext")
	}
	m.keyMu.RLock()
	locked := m.identity == nil
	m.keyMu.RUnlock()
	if !locked {
		t.Fatal("sealing recovery restored a private identity")
	}
	assertNoSaveTemps(t, m)
}

func TestRecoveryFollowupIndexPublicationAndRemovalFailures(t *testing.T) {
	m := setupTestManager(t)
	const date = "2024-02-29"
	buffer := filepath.Join(t.TempDir(), "synthetic-buffer.md")
	bufferBytes := []byte("synthetic retained α\r\n\tno final newline")
	if err := os.WriteFile(buffer, bufferBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(m.storageDir, recoveryIndexName(date, buffer))
	if err := os.Mkdir(index, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(index, "synthetic-sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Use a nonempty directory to block both replacement and removal, without
	// depending on chmod, root behavior, symlink privileges, or native ACLs.
	if err := m.RecordBufferRecovery(date, buffer); err == nil || errors.Is(err, ErrDurabilityUnconfirmed) {
		t.Fatalf("blocked index publication = %v", err)
	}
	if err := m.ForgetBufferRecovery(date, buffer); err == nil {
		t.Fatal("forgot a nonempty directory as if it were an index file")
	}
	if string(readTestFile(t, sentinel)) != "keep" || !bytes.Equal(bufferBytes, readTestFile(t, buffer)) {
		t.Fatal("failed index operation changed the target or retained buffer")
	}
	assertNoSaveTemps(t, m)
	if err := os.Remove(sentinel); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordBufferRecovery(date, buffer); err != nil {
		t.Fatal(err)
	}
	refs, err := m.ListRecoveries()
	want := []RecoveryReference{{Date: date, Path: buffer}}
	if err != nil || !reflect.DeepEqual(refs, want) {
		t.Fatalf("index retry = %+v, %v", refs, err)
	}
	for range 2 {
		if err := m.ForgetBufferRecovery(date, buffer); err != nil {
			t.Fatalf("forget retry = %v", err)
		}
	}
	if refs, err := m.ListRecoveries(); err != nil || len(refs) != 0 {
		t.Fatalf("forgotten index = %+v, %v", refs, err)
	}
	if !bytes.Equal(bufferBytes, readTestFile(t, buffer)) {
		t.Fatal("successful index retries changed retained buffer bytes")
	}
	assertNoSaveTemps(t, m)
}

func TestRecoveryFollowupIndexVisibleAfterDurabilityFailure(t *testing.T) {
	m := setupTestManager(t)
	ref := RecoveryReference{Date: "2024-02-29", Path: filepath.Join(t.TempDir(), "synthetic-never-created-buffer.md")}
	index := filepath.Join(m.storageDir, recoveryIndexName(ref.Date, ref.Path))
	data, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("synthetic discovery directory sync failure")
	calls := 0
	// Exercise the existing publish seam, not an injected RecordBufferRecovery
	// call: publication succeeds, then directory durability cannot be confirmed.
	published, err := publish(index, data, false, func(dir string) error {
		calls++
		if dir != m.storageDir || !bytes.Equal(data, readTestFile(t, index)) {
			t.Fatal("directory sync ran before index publication")
		}
		return injected
	})
	if !published || calls != 1 || !errors.Is(err, ErrDurabilityUnconfirmed) || !errors.Is(err, injected) {
		t.Fatalf("index publication = %v, %v (sync calls %d)", published, err, calls)
	}
	fresh, err := NewManagerAt(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := fresh.ListRecoveries()
	if err != nil || !reflect.DeepEqual(refs, []RecoveryReference{ref}) {
		t.Fatalf("published discovery = %+v, %v", refs, err)
	}
	if err := fresh.ForgetBufferRecovery(ref.Date, ref.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(index); !os.IsNotExist(err) {
		t.Fatalf("index still present after forget: %v", err)
	}
	if _, err := os.Lstat(ref.Path); !os.IsNotExist(err) {
		t.Fatalf("discovery created an external buffer: %v", err)
	}
	assertNoSaveTemps(t, fresh)
}
