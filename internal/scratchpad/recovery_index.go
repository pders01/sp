package scratchpad

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxRecoveryReferenceSize = 16 << 10

// RecoveryReference contains public discovery metadata, never recovered text.
// A reference is not proof of a valid recovery or an existing temporary buffer.
// Listed external paths are never opened, followed, or executed automatically.
type RecoveryReference struct {
	Date      string `json:"date"`
	Path      string `json:"path"`
	Encrypted bool   `json:"encrypted"`
}

func recoveryIndexName(date, path string) string {
	id := sha256.Sum256([]byte(date + "\x00" + path))
	return fmt.Sprintf(".sp-buffer-recovery-%x.json", id)
}

func validBufferReference(ref RecoveryReference) bool {
	return validateDate(ref.Date) == nil && filepath.IsAbs(ref.Path) && utf8.ValidString(ref.Path) &&
		strings.IndexByte(ref.Path, 0) < 0 && len(ref.Path) <= 8192 && !ref.Encrypted
}

// RecordBufferRecovery records only where a legacy plaintext editor buffer was
// retained. It does not copy its contents or turn a temporary file into a backup.
func (m *Manager) RecordBufferRecovery(date, path string) error {
	if err := validateDate(date); err != nil {
		return err
	}
	if !validBufferReference(RecoveryReference{Date: date, Path: path}) {
		return errors.New("invalid retained buffer path")
	}
	data, err := json.Marshal(RecoveryReference{Date: date, Path: path})
	if err != nil {
		return err
	}
	if len(data) > maxRecoveryReferenceSize {
		return errors.New("retained buffer metadata exceeds discovery size limit")
	}
	lock, err := lockNote(m.storageDir, "recovery-index")
	if err != nil {
		return err
	}
	defer lock.Close()
	_, err = publish(filepath.Join(m.storageDir, recoveryIndexName(date, path)), data, false, syncDirectory)
	return err
}

// ForgetBufferRecovery removes discovery metadata, never the referenced file.
func (m *Manager) ForgetBufferRecovery(date, path string) error {
	lock, err := lockNote(m.storageDir, "recovery-index")
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := os.Remove(filepath.Join(m.storageDir, recoveryIndexName(date, path))); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(m.storageDir)
}

func recoveryDate(name string) (string, bool) {
	rest := strings.TrimPrefix(name, ".sp-recovery-")
	if rest == name || len(rest) != 47 || rest[10] != '-' || rest[43:] != ".age" {
		return "", false
	}
	if validateDate(rest[:10]) != nil {
		return "", false
	}
	if _, err := hex.DecodeString(rest[11:43]); err != nil {
		return "", false
	}
	return rest[:10], true
}

// ListRecoveries works while locked. Encrypted candidates are identified by
// filename only; authentication still happens in ReadRecovery after unlock.
// Plaintext references may be stale and are not authority to access their paths.
func (m *Manager) ListRecoveries() ([]RecoveryReference, error) {
	entries, err := os.ReadDir(m.storageDir)
	if err != nil {
		return nil, err
	}
	var refs []RecoveryReference
	var problems []error
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if date, ok := recoveryDate(name); ok {
			refs = append(refs, RecoveryReference{Date: date, Path: filepath.Join(m.storageDir, name), Encrypted: true})
			continue
		}
		if !strings.HasPrefix(name, ".sp-buffer-recovery-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		var ref RecoveryReference
		data, readErr := readBounded(filepath.Join(m.storageDir, name), maxRecoveryReferenceSize)
		if readErr == nil {
			readErr = strictJSON(data, &ref)
		}
		if readErr == nil && (!validBufferReference(ref) || name != recoveryIndexName(ref.Date, ref.Path)) {
			readErr = errors.New("invalid recovery reference")
		}
		if readErr != nil {
			problems = append(problems, fmt.Errorf("recovery reference %q: %w", name, readErr))
			continue
		}
		refs = append(refs, ref)
	}
	return refs, errors.Join(problems...)
}
