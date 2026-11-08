package scratchpad

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"time"
	"unicode/utf8"

	"filippo.io/age"
)

// PinRecovery returns a public-only sealer bound to the current recovery
// recipient and storage directory. Lock, metadata replacement, and subsequent
// Unlock calls cannot redirect it. Pinning publishes nothing and retains no
// private identity or passphrase. The sealer is safe for concurrent use.
// As with SealRecovery, a published path may accompany ErrDurabilityUnconfirmed.
func (m *Manager) PinRecovery() (func(date, content string) (string, error), error) {
	recipient, err := m.pinnedRecoveryRecipient()
	if err != nil {
		return nil, err
	}
	storageDir := m.storageDir
	return func(date, content string) (string, error) {
		if err := validateRecoveryInput(date, content); err != nil {
			return "", err
		}
		return sealValidatedRecovery(storageDir, recipient, date, content, syncDirectory)
	}, nil
}

func (m *Manager) pinnedRecoveryRecipient() (*age.X25519Recipient, error) {
	m.keyMu.RLock()
	pinned := m.recoveryRecipient
	m.keyMu.RUnlock()
	if pinned == "" {
		metadata, _, err := m.readMetadata()
		if err != nil {
			return nil, err
		}
		if metadata == nil {
			return nil, errors.New("recovery encryption requires privacy metadata")
		}
		m.keyMu.Lock()
		if m.recoveryRecipient == "" {
			m.recoveryRecipient = metadata.Recipient
		}
		pinned = m.recoveryRecipient
		m.keyMu.Unlock()
	}
	// Do not redirect recovery to a public key substituted on disk after
	// unlock. Keeping this PUBLIC recipient after Lock is safe.
	return age.ParseX25519Recipient(pinned)
}

func validateRecoveryInput(date, content string) error {
	if err := validateDate(date); err != nil {
		return err
	}
	if len(content) > maxPrivateSize || !utf8.ValidString(content) {
		return errors.New("invalid recovery content or size")
	}
	return nil
}

// sealValidatedRecovery is the single encryption/publication path for both
// manager and session recovery. Callers validate input before encryption;
// SealRecovery also retains validation before recipient lookup. Keep fault injection local to the operation, as with other storage writes.
func sealValidatedRecovery(storageDir string, recipient *age.X25519Recipient, date, content string, syncDir func(string) error) (string, error) {
	now := time.Now()
	plain, err := json.Marshal(privateDocument{Version: 2, Date: date, Content: content, Created: now, Modified: now})
	if err != nil {
		return "", err
	}
	if len(plain) > maxPrivateSize {
		return "", errors.New("recovery content too large")
	}
	ciphertext, err := encryptAge(plain, recipient)
	if err != nil {
		return "", err
	}
	id, err := randomID()
	if err != nil {
		return "", err
	}
	path := filepath.Join(storageDir, ".sp-recovery-"+date+"-"+id+".age")
	published, err := publish(path, ciphertext, true, syncDir)
	if published {
		return path, err
	}
	return "", err
}
