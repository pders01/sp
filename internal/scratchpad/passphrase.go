package scratchpad

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"filippo.io/age"
)

// ChangePassphrase rewraps the existing identity in a completely migrated,
// enabled store. It does not rotate the key, rewrite notes or recovery, or
// revoke old metadata copies. Both passphrases are validated as exact bytes.
//
// published is false when no replacement occurred. If true, the replacement is
// visible even when err is ErrDurabilityUnconfirmed; it must not be rolled back.
// A retry must authenticate whichever metadata is actually present. The manager
// is locked after every attempt (including failure), and its pinned public
// recovery recipient is retained. Callers must drop their own decrypted views
// and unlock afresh; Lock cannot erase copies held elsewhere in memory.
func (m *Manager) ChangePassphrase(current, replacement []byte) (published bool, err error) {
	return m.changePassphrase(current, replacement, syncDirectory)
}

// Keep durability fault injection local to this operation, as with save.
func (m *Manager) changePassphrase(current, replacement []byte, syncDir func(string) error) (bool, error) {
	m.Lock()
	// Manager.Lock drops key references; it is not a mutex acquisition.
	defer func() { m.Lock() }()
	data, expected, err := m.preparePassphraseChange(current, replacement)
	if err != nil {
		return false, err
	}
	return m.publishPassphraseChange(data, expected, syncDir)
}

// Preparation intentionally takes no store or date lock: all scrypt operations
// and verification finish before the publication lock is acquired. Splitting
// these phases also permits deterministic tests of intervening policy changes.
// Neither phase caches a passphrase or decrypted identity on the manager.
func (m *Manager) preparePassphraseChange(current, replacement []byte) (encoded []byte, fingerprint [sha256.Size]byte, operationErr error) {
	var zero [sha256.Size]byte
	if err := validatePassphrase(current); err != nil {
		return nil, zero, err
	}
	if err := validatePassphrase(replacement); err != nil {
		return nil, zero, err
	}
	metadata, expected, err := m.readMetadata()
	if err != nil {
		return nil, zero, err
	}
	if policyErr := m.checkPassphrasePolicy(metadata); policyErr != nil {
		return nil, zero, policyErr
	}
	// Do not use Unlock or an already cached identity: authenticate the supplied
	// current passphrase afresh, without altering the public recovery pin.
	plain, err := unwrapPassphraseIdentity(metadata.WrappedIdentity, current)
	defer clear(plain) // Best effort only; parsed identity/runtime copies may remain.
	if err != nil {
		return nil, zero, fmt.Errorf("authenticate current passphrase: %w", err)
	}
	identity, err := age.ParseX25519Identity(string(plain))
	if err != nil {
		return nil, zero, errors.New("invalid unwrapped privacy identity")
	}
	if identity.Recipient().String() != metadata.Recipient {
		return nil, zero, errors.New("privacy recipient does not match identity")
	}
	recipient, err := age.NewScryptRecipient(string(replacement))
	if err != nil {
		return nil, zero, err
	}
	recipient.SetWorkFactor(scryptWorkFactor)
	wrapped, err := encryptAge(plain, recipient)
	if err != nil {
		return nil, zero, err
	}
	pending := *metadata
	pending.WrappedIdentity = base64.StdEncoding.EncodeToString(wrapped)
	verified, err := unwrapPassphraseIdentity(pending.WrappedIdentity, replacement)
	defer clear(verified)
	if err != nil {
		return nil, zero, fmt.Errorf("verify replacement wrapping: %w", err)
	}
	if !bytes.Equal(plain, verified) {
		return nil, zero, errors.New("replacement identity verification failed")
	}
	data, err := json.MarshalIndent(pending, "", "  ")
	return data, expected, err
}

func unwrapPassphraseIdentity(wrapped string, passphrase []byte) ([]byte, error) {
	identity, err := age.NewScryptIdentity(string(passphrase))
	if err != nil {
		return nil, err
	}
	identity.SetMaxWorkFactor(scryptWorkFactor)
	payload, err := decodePayload(wrapped, maxMetadataSize)
	if err != nil {
		return nil, err
	}
	return decryptAge(payload, identity, 1024)
}

// The caller holds the store lock at publication, but not at preparation.
// Inspect outer records only: this is not a decrypt-all integrity audit.
func (m *Manager) checkPassphrasePolicy(metadata *privacyMetadata) error {
	if metadata != nil && metadata.State == "decrypting" {
		return ErrPrivacyDecrypting
	}
	if metadata == nil || metadata.Version != 1 || metadata.State != "" {
		return errors.New("passphrase change requires an enabled privacy policy")
	}
	dates, err := m.privacyDates()
	if err != nil {
		return err
	}
	for _, date := range dates {
		data, err := readBounded(filepath.Join(m.storageDir, date+".json"), maxDocumentSize)
		if err != nil {
			return fmt.Errorf("inspect migration %s: %w", date, err)
		}
		r, err := decodeRecord(data, date)
		if err != nil {
			return fmt.Errorf("inspect migration %s: %w", date, err)
		}
		if r.legacy != nil {
			return errors.New("privacy migration is incomplete; run sp privacy enable to resume")
		}
	}
	return nil
}

func (m *Manager) publishPassphraseChange(data []byte, expected [sha256.Size]byte, syncDir func(string) error) (bool, error) {
	storeLock, err := lockNote(m.storageDir, "privacy")
	if err != nil {
		return false, err
	}
	defer storeLock.Close()
	metadata, fingerprint, err := m.readMetadata()
	if err != nil {
		return false, err
	}
	if fingerprint != expected {
		return false, ErrPrivacyPolicyChanged
	}
	// Fingerprints alone cannot detect a retained-key disable/re-enable ABA
	// that restored metadata but left new or previously visited legacy dates.
	if policyErr := m.checkPassphrasePolicy(metadata); policyErr != nil {
		return false, policyErr
	}
	published, err := publish(filepath.Join(m.storageDir, "privacy.json"), data, false, syncDir)
	if published {
		m.keyMu.Lock()
		m.observedPrivacy = sha256.Sum256(data)
		m.keyMu.Unlock()
	}
	return published, err
}
