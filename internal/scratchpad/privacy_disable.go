package scratchpad

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
)

// DisablePrivacy explicitly decrypts daily private notes in place. It retains
// the passphrase-wrapped key for encrypted recovery files and old backups.
// Once reversal starts, normal writes fail closed until this operation finishes.
// Interrupted conversions can be resumed with the same passphrase. Existing
// copies/backups are not erased, decrypted, or revoked by this operation.
func (m *Manager) DisablePrivacy(passphrase []byte) error {
	return m.DisablePrivacyWithProgress(passphrase, nil)
}

// DisablePrivacyWithProgress is DisablePrivacy with synchronous, data-free
// notifications. Verification reports the existing preflight pass; decryption
// reports the existing publication pass. See PrivacyProgress for restrictions.
func (m *Manager) DisablePrivacyWithProgress(passphrase []byte, progress func(PrivacyProgress)) error {
	return m.disablePrivacyWithProgress(passphrase, syncDirectory, progress)
}

func (m *Manager) disablePrivacy(passphrase []byte, syncDir func(string) error) error {
	return m.disablePrivacyWithProgress(passphrase, syncDir, nil)
}

func (m *Manager) disablePrivacyWithProgress(passphrase []byte, syncDir func(string) error, progress func(PrivacyProgress)) error {
	reportPrivacyProgress(progress, "inspecting", 0, 0)
	storeLock, lockErr := lockNote(m.storageDir, "privacy")
	if lockErr != nil {
		return lockErr
	}
	defer storeLock.Close()
	metadata, _, lockErr := m.readMetadata()
	if lockErr != nil {
		return lockErr
	}
	if metadata == nil || metadata.State == "disabled" {
		if _, err := m.PrivacyEnabled(); err != nil {
			return err
		}
		// A previous attempt may have published the final state without
		// confirming directory durability. Retrying confirms it explicitly.
		if err := syncDir(m.storageDir); err != nil {
			return fmt.Errorf("%w: %w", ErrDurabilityUnconfirmed, err)
		}
		reportPrivacyProgress(progress, "complete", 0, 0)
		return nil
	}
	if err := m.Unlock(passphrase); err != nil {
		return err
	}
	dates, lockErr := m.privacyDates()
	if lockErr != nil {
		return lockErr
	}
	// Authenticate and verify every conversion before exposing any plaintext.
	// A second pass bounds plaintext memory usage to one daily document.
	reportPrivacyProgress(progress, "verifying", 0, len(dates))
	for i, date := range dates {
		if _, err := m.plaintextRecord(date); err != nil {
			return fmt.Errorf("check decryption of %s: %w", date, err)
		}
		reportPrivacyProgress(progress, "verifying", i+1, len(dates))
	}
	// Durably record the transition before the first plaintext publication.
	// v2 metadata is rejected by older privacy-aware SP versions, rather than
	// allowing them to re-encrypt notes partway through this conversion.
	if err := m.setPrivacyState(metadata, "decrypting", syncDir); err != nil {
		return err
	}
	reportPrivacyProgress(progress, "decrypting", 0, len(dates))
	for i, date := range dates {
		if err := m.decryptDate(date, syncDir); err != nil {
			return fmt.Errorf("decrypt %s: %w; run sp privacy disable to resume", date, err)
		}
		reportPrivacyProgress(progress, "decrypting", i+1, len(dates))
	}
	// All notes have been verified and their publications synced. Only now
	// permit ordinary plaintext saves, while retaining the wrapped key.
	if err := m.setPrivacyState(metadata, "disabled", syncDir); err != nil {
		return err
	}
	reportPrivacyProgress(progress, "complete", len(dates), len(dates))
	return nil
}

// plaintextRecord returns nil for an already plaintext or shared-only note.
func (m *Manager) plaintextRecord(date string) ([]byte, error) {
	data, err := readBounded(filepath.Join(m.storageDir, date+".json"), maxDocumentSize)
	if err != nil {
		return nil, err
	}
	record, err := decodeRecord(data, date)
	if err != nil {
		return nil, err
	}
	if record.Private == nil {
		return nil, nil
	}
	note, err := m.openRecord(record)
	if err != nil {
		return nil, err
	}
	// openRecord deliberately carries no save baseline. The generic Save API
	// still rejects downgrades; only this explicit migration publishes plaintext.
	plain, _, err := m.encodeScratchpad(note, false)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeRecord(plain, date)
	if err != nil {
		return nil, err
	}
	verified, err := m.openRecord(decoded)
	if err != nil {
		return nil, err
	}
	before, err := json.Marshal(note)
	if err != nil {
		return nil, err
	}
	after, err := json.Marshal(verified)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(before, after) {
		return nil, errors.New("plaintext scratchpad verification failed")
	}
	return plain, nil
}

func (m *Manager) decryptDate(date string, syncDir func(string) error) error {
	lock, err := lockNote(m.storageDir, date)
	if err != nil {
		return err
	}
	defer lock.Close()
	data, err := m.plaintextRecord(date)
	if err != nil || data == nil {
		return err
	}
	_, err = publish(filepath.Join(m.storageDir, date+".json"), data, false, syncDir)
	return err
}

// Caller holds the store lock and has unlocked the retained identity. Updating
// the cached fingerprint preserves unlock across our own policy transitions.
func (m *Manager) setPrivacyState(metadata *privacyMetadata, state string, syncDir func(string) error) error {
	if _, err := m.unlockedIdentity(); err != nil {
		return err
	}
	pending := *metadata
	pending.State = state
	pending.Version = 2
	if state == "" {
		pending.Version = 1
	}
	data, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return err
	}
	published, err := publish(filepath.Join(m.storageDir, "privacy.json"), data, false, syncDir)
	if published {
		*metadata = pending
		m.keyMu.Lock()
		m.keyMetadata = sha256.Sum256(data)
		m.observedPrivacy = m.keyMetadata
		m.keyMu.Unlock()
	}
	return err
}
