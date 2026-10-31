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
	"time"
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
	m.keyMu.Lock()
	m.identity, m.keyMetadata = identity, fingerprint
	m.recoveryRecipient = identity.Recipient().String()
	m.keyMu.Unlock()
	return nil
}

// EnablePrivacy durably initializes metadata before migrating any notes. A
// partial migration is still an enabled store: all future private saves encrypt.
// Calling it again unlocks the existing identity and resumes. Re-enabling a
// disabled store reuses the retained key and requires its existing passphrase.
func (m *Manager) EnablePrivacy(passphrase []byte) error {
	if err := validatePassphrase(passphrase); err != nil {
		return err
	}
	if err := m.initializePrivacy(passphrase); err != nil {
		return err
	}
	dates, err := m.privacyDates()
	if err != nil {
		return err
	}
	for _, date := range dates {
		data, err := readBounded(filepath.Join(m.storageDir, date+".json"), maxDocumentSize)
		if err != nil {
			return err
		}
		r, err := decodeRecord(data, date)
		if err != nil {
			return fmt.Errorf("migrate %s: %w", date, err)
		}
		if r.legacy == nil {
			continue
		}
		note, err := m.GetByDate(date)
		if err != nil {
			return err
		}
		if note.disk.legacy == nil {
			continue
		}
		if err := m.saveInternal(note, syncDirectory, true); err != nil {
			return fmt.Errorf("migrate %s: %w", date, err)
		}
	}
	return nil
}

func (m *Manager) initializePrivacy(passphrase []byte) error {
	lock, err := lockNote(m.storageDir, "privacy")
	if err != nil {
		return err
	}
	defer lock.Close()
	metadata, _, err := m.readMetadata()
	if err != nil {
		return err
	}
	if metadata != nil {
		if metadata.State == "decrypting" {
			return ErrPrivacyDecrypting
		}
		if metadata.State == "disabled" {
			if _, err := m.PrivacyEnabled(); err != nil {
				return err
			}
		}
		if err := m.Unlock(passphrase); err != nil {
			return err
		}
		if metadata.State == "disabled" {
			return m.setPrivacyState(metadata, "", syncDirectory)
		}
		return nil
	}
	// An orphan ciphertext must never cause generation of a replacement key.
	if _, privacyErr := m.PrivacyEnabled(); privacyErr != nil {
		return privacyErr
	}
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return err
	}
	recipient, err := age.NewScryptRecipient(string(passphrase))
	if err != nil {
		return err
	}
	recipient.SetWorkFactor(scryptWorkFactor)
	wrapped, err := encryptAge([]byte(identity.String()), recipient)
	if err != nil {
		return err
	}
	metadata = &privacyMetadata{Version: 1, Recipient: identity.Recipient().String(), WrappedIdentity: base64.StdEncoding.EncodeToString(wrapped)}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	published, err := publish(filepath.Join(m.storageDir, "privacy.json"), data, true, syncDirectory)
	if published {
		m.keyMu.Lock()
		m.identity, m.keyMetadata = identity, sha256.Sum256(data)
		m.recoveryRecipient = identity.Recipient().String()
		m.observedPrivacy = m.keyMetadata
		m.keyMu.Unlock()
	}
	return err
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
	if err := validateDate(date); err != nil {
		return "", err
	}
	if len(content) > maxPrivateSize || !utf8.ValidString(content) {
		return "", errors.New("invalid recovery content or size")
	}
	m.keyMu.RLock()
	pinned := m.recoveryRecipient
	m.keyMu.RUnlock()
	if pinned == "" {
		metadata, _, err := m.readMetadata()
		if err != nil {
			return "", err
		}
		if metadata == nil {
			return "", errors.New("recovery encryption requires privacy metadata")
		}
		m.keyMu.Lock()
		if m.recoveryRecipient == "" {
			m.recoveryRecipient = metadata.Recipient
		}
		pinned = m.recoveryRecipient
		m.keyMu.Unlock()
	}
	// Do not redirect a live editor's recovery to a public key substituted on
	// disk after unlock. Keeping this PUBLIC recipient after Lock is safe.
	recipient, err := age.ParseX25519Recipient(pinned)
	if err != nil {
		return "", err
	}
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
	path := filepath.Join(m.storageDir, ".sp-recovery-"+date+"-"+id+".age")
	published, err := publish(path, ciphertext, true, syncDirectory)
	if published {
		return path, err
	}
	return "", err
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
