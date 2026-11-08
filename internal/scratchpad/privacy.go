package scratchpad

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"filippo.io/age"
)

// ErrLocked means private editing requires a successfully unlocked identity.
var ErrLocked = errors.New("private scratchpad is locked")

// ErrPrivacyDecrypting blocks normal access until an explicit reversal finishes.
var ErrPrivacyDecrypting = errors.New("privacy decryption is incomplete; run sp privacy disable to resume")

const (
	maxMetadataSize   = 64 << 10
	maxPassphraseSize = 4096
	// age's default: at most approximately 256 MiB of scrypt memory. Never
	// accept an attacker-selected, larger work factor from a metadata header.
	scryptWorkFactor = 18
)

type privacyMetadata struct {
	Version         int    `json:"version"`
	Recipient       string `json:"recipient"`
	WrappedIdentity string `json:"wrapped_identity"`
	State           string `json:"state,omitempty"` // v2: decrypting or disabled; key retained for recovery.
}

// PrivacyStatus describes the on-disk migration state; no configuration boolean
// or cached flag determines whether privacy is enabled.
type PrivacyStatus struct {
	Enabled         bool
	Decrypting      bool
	PlaintextDates  []string
	EncryptedDates  []string
	SharedOnlyDates []string
}

// PrivacyProgress contains only a stage and date-file counts, never note data,
// dates, credentials, or error details. Stages are inspecting, migrating,
// verifying, decrypting, and complete, as applicable to the operation. Counts
// are stage-local and include unchanged files; Total is zero when unknown or
// when no date-file pass is needed. Complete is emitted only on success.
// Migration verifies new ciphertext inline. Final completion also inspects all
// current records under the store lock, without decrypting the archive.
//
// Callbacks run synchronously on the caller's goroutine, possibly with storage
// locks held. They must return promptly and must not reenter or mutate the
// manager. They cannot cancel the operation; errors are returned by the method.
// Callback panics propagate, so callbacks must not panic.
type PrivacyProgress struct {
	Stage     string
	Completed int
	Total     int
}

func reportPrivacyProgress(progress func(PrivacyProgress), stage string, completed, total int) {
	if progress != nil {
		progress(PrivacyProgress{Stage: stage, Completed: completed, Total: total})
	}
}

func (m *Manager) readMetadata() (*privacyMetadata, [sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	data, err := readBounded(filepath.Join(m.storageDir, "privacy.json"), maxMetadataSize)
	if os.IsNotExist(err) {
		m.keyMu.RLock()
		observed := m.observedPrivacy != zero
		m.keyMu.RUnlock()
		if observed {
			return nil, zero, errors.New("previously observed privacy metadata is missing")
		}
		return nil, zero, nil
	}
	if err != nil {
		return nil, zero, fmt.Errorf("read privacy metadata: %w", err)
	}
	var metadata privacyMetadata
	if decodeErr := strictJSON(data, &metadata); decodeErr != nil {
		return nil, zero, fmt.Errorf("invalid privacy metadata: %w", decodeErr)
	}
	switch {
	case metadata.Version == 1 && metadata.State == "":
	case metadata.Version == 2 && (metadata.State == "decrypting" || metadata.State == "disabled"):
	default:
		return nil, zero, errors.New("unsupported privacy metadata version or state")
	}
	if _, parseErr := age.ParseX25519Recipient(metadata.Recipient); parseErr != nil {
		return nil, zero, errors.New("invalid privacy recipient")
	}
	wrapped, err := decodePayload(metadata.WrappedIdentity, maxMetadataSize)
	if err != nil {
		return nil, zero, err
	}
	if err := validateAgeEnvelope(wrapped, "scrypt"); err != nil {
		return nil, zero, fmt.Errorf("invalid wrapped identity: %w", err)
	}
	fingerprint := sha256.Sum256(data)
	m.keyMu.Lock()
	m.observedPrivacy = fingerprint
	m.keyMu.Unlock()
	return &metadata, fingerprint, nil
}

// privacyDates is stricter than the calendar's ListDates: a date-named symlink
// or special file must not hide ciphertext from policy/migration/status checks.
func (m *Manager) privacyDates() ([]string, error) {
	entries, err := os.ReadDir(m.storageDir)
	if err != nil {
		return nil, err
	}
	var dates []string
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if !strings.EqualFold(ext, ".json") {
			continue
		}
		date := strings.TrimSuffix(entry.Name(), ext)
		if validateDate(date) != nil {
			continue
		}
		if ext != ".json" {
			return nil, fmt.Errorf("inspect %s: date files must use the canonical lowercase .json extension", date)
		}
		if !entry.Type().IsRegular() {
			return nil, fmt.Errorf("inspect %s: date paths must be regular files, not directories, symlinks, or special files", date)
		}
		dates = append(dates, date)
	}
	return dates, nil
}

// PrivacyEnabled reads metadata afresh and fails closed on corruption. Without
// metadata, encrypted pages are an error, not permission to write plaintext.
func (m *Manager) PrivacyEnabled() (bool, error) {
	metadata, _, err := m.readMetadata()
	if err != nil {
		return false, err
	}
	if metadata != nil {
		switch metadata.State {
		case "decrypting":
			return true, ErrPrivacyDecrypting
		case "":
			return true, nil
		}
	}
	dates, err := m.privacyDates()
	if err != nil {
		return false, err
	}
	for _, date := range dates {
		data, err := readBounded(filepath.Join(m.storageDir, date+".json"), maxDocumentSize)
		if err != nil {
			return false, err
		}
		// Inspect only the outer version/private marker here. Unknown legacy
		// fields are validated by the requested note operation, not discarded,
		// and must not prevent initializing a resumable migration.
		var fields map[string]json.RawMessage
		if err := strictJSON(data, &fields); err != nil {
			return false, err
		}
		var version int
		if raw, ok := fields["version"]; ok {
			if string(raw) == "null" {
				return false, fmt.Errorf("inspect %s: invalid scratchpad version", date)
			}
			if err := json.Unmarshal(raw, &version); err != nil {
				return false, fmt.Errorf("inspect %s: %w", date, err)
			}
		}
		if version < 0 || version > 2 {
			return false, fmt.Errorf("inspect %s: unsupported scratchpad version %d", date, version)
		}
		if _, private := fields["private"]; private || version == 2 {
			// Shared-only v2 pages are safe without metadata, but still need
			// full validation to distinguish them from an orphan ciphertext.
			r, err := decodeRecord(data, date)
			if err != nil {
				return false, err
			}
			if r.Private != nil {
				return false, errors.New("encrypted scratchpad exists without an active privacy policy; restore metadata or resume sp privacy disable")
			}
		}
	}
	return false, nil
}

// Lock drops the manager's reference to the identity. Go cannot promise secure
// erasure of previously allocated memory or of content held by callers.
func (m *Manager) Lock() {
	m.keyMu.Lock()
	defer m.keyMu.Unlock()
	m.identity = nil
	m.keyMetadata = [sha256.Size]byte{}
	m.keyGeneration = [32]byte{}
}

func (m *Manager) unlockedIdentity() (*age.X25519Identity, error) {
	metadata, fingerprint, err := m.readMetadata()
	if err != nil {
		return nil, err
	}
	m.keyMu.RLock()
	defer m.keyMu.RUnlock()
	if metadata == nil || m.identity == nil || fingerprint != m.keyMetadata || metadata.Recipient != m.identity.Recipient().String() {
		return nil, ErrLocked
	}
	return m.identity, nil
}

func (m *Manager) IsUnlocked() bool {
	_, err := m.unlockedIdentity()
	return err == nil
}

// ValidatePassphrase validates bytes without transforming or retaining them.
func ValidatePassphrase(passphrase []byte) error { return validatePassphrase(passphrase) }

func validatePassphrase(passphrase []byte) error {
	if len(passphrase) == 0 || len(passphrase) > maxPassphraseSize {
		return errors.New("passphrase must contain 1 to 4096 bytes")
	}
	if !utf8.Valid(passphrase) {
		return errors.New("passphrase must be valid UTF-8 text")
	}
	for _, c := range string(passphrase) {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' {
			return errors.New("passphrase must be single-line text without control characters")
		}
	}
	return nil
}

func (m *Manager) Unlock(passphrase []byte) error {
	m.Lock()
	if err := validatePassphrase(passphrase); err != nil {
		return err
	}
	metadata, fingerprint, err := m.readMetadata()
	if err != nil {
		return err
	}
	if metadata == nil {
		return errors.New("privacy metadata is missing")
	}
	passwordIdentity, err := age.NewScryptIdentity(string(passphrase))
	if err != nil {
		return err
	}
	passwordIdentity.SetMaxWorkFactor(scryptWorkFactor)
	wrapped, err := decodePayload(metadata.WrappedIdentity, maxMetadataSize)
	if err != nil {
		return err
	}
	plain, err := decryptAge(wrapped, passwordIdentity, 1024)
	if err != nil {
		return fmt.Errorf("unlock privacy identity: %w", err)
	}
	identity, err := age.ParseX25519Identity(string(plain))
	if err != nil {
		return errors.New("invalid unwrapped privacy identity")
	}
	if identity.Recipient().String() != metadata.Recipient {
		return errors.New("privacy recipient does not match identity")
	}
	// Do not cache a key for metadata replaced while scrypt was running.
	_, current, err := m.readMetadata()
	if err != nil {
		return err
	}
	if current != fingerprint {
		return errors.New("privacy metadata changed during unlock")
	}
	generation, err := newAuthGeneration()
	if err != nil {
		return err
	}
	m.keyMu.Lock()
	m.identity, m.keyMetadata = identity, fingerprint
	m.keyGeneration = generation
	m.recoveryRecipient = identity.Recipient().String()
	m.keyMu.Unlock()
	return nil
}

// EnablePrivacy durably initializes metadata before migrating any notes. A
// partial migration is still an enabled store: all future private saves encrypt.
// Calling it again unlocks the existing identity and resumes. Re-enabling a
// disabled store reuses the retained key and requires its existing passphrase.
func (m *Manager) EnablePrivacy(passphrase []byte) error {
	return m.EnablePrivacyWithProgress(passphrase, nil)
}

// EnablePrivacyWithProgress is EnablePrivacy with synchronous, data-free
// notifications. See PrivacyProgress for callback restrictions and count semantics.
func (m *Manager) EnablePrivacyWithProgress(passphrase []byte, progress func(PrivacyProgress)) error {
	return m.enablePrivacyWithProgress(passphrase, syncDirectory, progress)
}

// Keep fault injection local to the operation, as with save and disablePrivacy.
func (m *Manager) enablePrivacyWithProgress(passphrase []byte, syncDir func(string) error, progress func(PrivacyProgress)) error {
	reportPrivacyProgress(progress, "inspecting", 0, 0)
	if err := validatePassphrase(passphrase); err != nil {
		return err
	}
	fingerprint, operationErr := m.initializePrivacy(passphrase, syncDir)
	if operationErr != nil {
		return operationErr
	}
	dates, operationErr := m.privacyDates()
	if operationErr != nil {
		return operationErr
	}
	reportPrivacyProgress(progress, "migrating", 0, len(dates))
	for i, date := range dates {
		data, err := readBounded(filepath.Join(m.storageDir, date+".json"), maxDocumentSize)
		if err != nil {
			return err
		}
		r, err := decodeRecord(data, date)
		if err != nil {
			return fmt.Errorf("migrate %s: %w", date, err)
		}
		if r.legacy != nil {
			note, err := m.GetByDate(date)
			if err != nil {
				return err
			}
			if note.disk.legacy != nil {
				if err := m.saveInternal(note, syncDir, true); err != nil {
					return fmt.Errorf("migrate %s: %w", date, err)
				}
			}
		}
		reportPrivacyProgress(progress, "migrating", i+1, len(dates))
	}
	// A retry can skip every publication, including the last note whose rename
	// previously returned ErrDurabilityUnconfirmed. Confirm the intended active
	// policy, current records, and directory durability before completion. Take
	// only the store lock here: per-note migration locks have been released.
	storeLock, operationErr := lockNote(m.storageDir, "privacy")
	if operationErr != nil {
		return operationErr
	}
	defer storeLock.Close()
	metadata, current, operationErr := m.readMetadata()
	if operationErr != nil {
		return operationErr
	}
	if metadata != nil && metadata.State == "decrypting" {
		return ErrPrivacyDecrypting
	}
	if metadata == nil || metadata.State != "" || current != fingerprint {
		return ErrPrivacyPolicyChanged
	}
	// Matching metadata does not prove uninterrupted policy: disable/re-enable
	// retains the key and can restore the exact fingerprint while leaving an
	// already-visited note plaintext or adding new legacy dates. Rescan under
	// the store lock so cooperating writers cannot change this verified state
	// before durability/completion. This is read-only, not consent to migrate
	// records left by an intervening reversal; require an explicit retry.
	currentDates, operationErr := m.privacyDates()
	if operationErr != nil {
		return operationErr
	}
	for _, date := range currentDates {
		data, err := readBounded(filepath.Join(m.storageDir, date+".json"), maxDocumentSize)
		if err != nil {
			return fmt.Errorf("verify migration %s: %w", date, err)
		}
		r, err := decodeRecord(data, date)
		if err != nil {
			return fmt.Errorf("verify migration %s: %w", date, err)
		}
		if r.legacy != nil {
			return errors.New("privacy migration is incomplete; legacy scratchpads remain; run sp privacy enable to resume")
		}
	}
	if err := syncDir(m.storageDir); err != nil {
		return fmt.Errorf("%w: sync storage directory: %w", ErrDurabilityUnconfirmed, err)
	}
	reportPrivacyProgress(progress, "complete", len(dates), len(dates))
	return nil
}

func (m *Manager) initializePrivacy(passphrase []byte, syncDir func(string) error) ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	lock, lockErr := lockNote(m.storageDir, "privacy")
	if lockErr != nil {
		return zero, lockErr
	}
	defer lock.Close()
	metadata, fingerprint, lockErr := m.readMetadata()
	if lockErr != nil {
		return zero, lockErr
	}
	if metadata != nil {
		if metadata.State == "decrypting" {
			return zero, ErrPrivacyDecrypting
		}
		if metadata.State == "disabled" {
			if _, err := m.PrivacyEnabled(); err != nil {
				return zero, err
			}
		}
		if err := m.Unlock(passphrase); err != nil {
			return zero, err
		}
		if metadata.State == "disabled" {
			if err := m.setPrivacyState(metadata, "", syncDir); err != nil {
				return zero, err
			}
			_, fingerprint, lockErr = m.readMetadata()
			return fingerprint, lockErr
		}
		// Existing metadata may be the visible but unconfirmed publication
		// from an interrupted activation. Make it durable before migrating.
		if err := syncDir(m.storageDir); err != nil {
			return zero, fmt.Errorf("%w: sync storage directory: %w", ErrDurabilityUnconfirmed, err)
		}
		return fingerprint, nil
	}
	// An orphan ciphertext must never cause generation of a replacement key.
	if _, privacyErr := m.PrivacyEnabled(); privacyErr != nil {
		return zero, privacyErr
	}
	identity, lockErr := age.GenerateX25519Identity()
	if lockErr != nil {
		return zero, lockErr
	}
	recipient, lockErr := age.NewScryptRecipient(string(passphrase))
	if lockErr != nil {
		return zero, lockErr
	}
	recipient.SetWorkFactor(scryptWorkFactor)
	wrapped, lockErr := encryptAge([]byte(identity.String()), recipient)
	if lockErr != nil {
		return zero, lockErr
	}
	metadata = &privacyMetadata{Version: 1, Recipient: identity.Recipient().String(), WrappedIdentity: base64.StdEncoding.EncodeToString(wrapped)}
	data, lockErr := json.MarshalIndent(metadata, "", "  ")
	if lockErr != nil {
		return zero, lockErr
	}
	generation, generationErr := newAuthGeneration()
	if generationErr != nil {
		return zero, generationErr
	}
	fingerprint = sha256.Sum256(data)
	published, lockErr := publish(filepath.Join(m.storageDir, "privacy.json"), data, true, syncDir)
	if published {
		m.keyMu.Lock()
		m.identity, m.keyMetadata = identity, sha256.Sum256(data)
		m.keyGeneration = generation
		m.recoveryRecipient = identity.Recipient().String()
		m.observedPrivacy = m.keyMetadata
		m.keyMu.Unlock()
	}
	return fingerprint, lockErr
}

func (m *Manager) PrivacyStatus() (PrivacyStatus, error) {
	status := PrivacyStatus{}
	enabled, err := m.PrivacyEnabled()
	if err != nil && !errors.Is(err, ErrPrivacyDecrypting) {
		return status, err
	}
	status.Enabled = enabled
	status.Decrypting = errors.Is(err, ErrPrivacyDecrypting)
	dates, err := m.privacyDates()
	if err != nil {
		return status, err
	}
	for _, date := range dates {
		data, err := readBounded(filepath.Join(m.storageDir, date+".json"), maxDocumentSize)
		if err != nil {
			return status, fmt.Errorf("inspect %s: %w", date, err)
		}
		r, err := decodeRecord(data, date)
		if err != nil {
			return status, fmt.Errorf("inspect %s: %w", date, err)
		}
		switch {
		case r.legacy != nil:
			status.PlaintextDates = append(status.PlaintextDates, date)
		case r.Private != nil:
			if m.IsUnlocked() {
				if _, err := m.openRecord(r); err != nil {
					return status, err
				}
			}
			status.EncryptedDates = append(status.EncryptedDates, date)
		default:
			status.SharedOnlyDates = append(status.SharedOnlyDates, date)
		}
	}
	return status, nil
}

// SealRecovery encrypts unsaved private content using only the public recipient.
// No plaintext is written, even to a temporary file. It works while locked.
func (m *Manager) SealRecovery(date, content string) (string, error) {
	if err := validateRecoveryInput(date, content); err != nil {
		return "", err
	}
	recipient, err := m.pinnedRecoveryRecipient()
	if err != nil {
		return "", err
	}
	return sealValidatedRecovery(m.storageDir, recipient, date, content, syncDirectory)
}

// Authentication generation is an in-memory nonce, NOT a new data key or format.
func newAuthGeneration() ([32]byte, error) {
	var generation [32]byte
	_, err := rand.Read(generation[:])
	return generation, err
}

func randomID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func encryptAge(plain []byte, recipient age.Recipient) ([]byte, error) {
	var out bytes.Buffer
	writer, err := age.Encrypt(&out, recipient)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(plain); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// headerProbe uses age's own parser, without performing cryptography, to reject
// malformed/oversized headers and unsupported recipient types before any KDF.
var errHeaderInspected = errors.New("age header inspected")

type headerProbe string

func (p headerProbe) Unwrap(stanzas []*age.Stanza) ([]byte, error) {
	if len(stanzas) != 1 || stanzas[0].Type != string(p) {
		return nil, errors.New("unsupported age recipient header")
	}
	stanza := stanzas[0]
	if len(stanza.Body) != 32 {
		return nil, errors.New("invalid age recipient body")
	}
	argSize := 32
	if p == "scrypt" {
		argSize = 16
		if len(stanza.Args) != 2 {
			return nil, errors.New("invalid scrypt header")
		}
		factor, err := strconv.Atoi(stanza.Args[1])
		if err != nil || factor < 1 || factor > scryptWorkFactor {
			return nil, errors.New("scrypt work factor exceeds supported bound")
		}
	} else if len(stanza.Args) != 1 {
		return nil, errors.New("invalid X25519 header")
	}
	arg, err := base64.RawStdEncoding.Strict().DecodeString(stanza.Args[0])
	if err != nil || len(arg) != argSize {
		return nil, errors.New("invalid age recipient argument")
	}
	return nil, errHeaderInspected
}

func validateAgeEnvelope(payload []byte, recipientType string) error {
	if !bytes.HasPrefix(payload, []byte("age-encryption.org/v1\n")) {
		return errors.New("invalid age format")
	}
	header := payload
	if len(header) > 4096 {
		header = header[:4096]
	}
	mac := bytes.Index(header, []byte("\n--- "))
	if mac < 0 {
		return errors.New("invalid or oversized age header")
	}
	end := bytes.IndexByte(header[mac+1:], '\n')
	if end < 0 || len(payload)-(mac+1+end+1) < 32 {
		return errors.New("truncated age envelope")
	}
	_, err := age.Decrypt(bytes.NewReader(payload), headerProbe(recipientType))
	if errors.Is(err, errHeaderInspected) {
		return nil
	}
	return err
}

func decryptAge(ciphertext []byte, identity age.Identity, limit int) ([]byte, error) {
	reader, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, err
	}
	plain, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(plain) > limit {
		return nil, errors.New("decrypted content exceeds size limit")
	}
	return plain, nil
}
