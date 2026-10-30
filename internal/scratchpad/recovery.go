package scratchpad

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Recovery is authenticated private text, not an automatically merged note.
type Recovery struct {
	Date    string
	Content string
}

// ReadRecovery opens only a regular SP recovery file inside this store, after
// unlock. The authenticated inner date must match its recovery filename.
func (m *Manager) ReadRecovery(path string) (*Recovery, error) {
	identity, err := m.unlockedIdentity()
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	rest := strings.TrimPrefix(name, ".sp-recovery-")
	if rest == name || len(rest) != 47 || rest[10] != '-' || rest[43:] != ".age" {
		return nil, errors.New("expected an SP recovery filename")
	}
	date := rest[:10]
	if dateErr := validateDate(date); dateErr != nil {
		return nil, dateErr
	}
	if _, decodeErr := hex.DecodeString(rest[11:43]); decodeErr != nil {
		return nil, errors.New("invalid recovery identifier")
	}
	dir, err := filepath.Abs(m.storageDir)
	if err != nil {
		return nil, err
	}
	filename := filepath.Join(dir, name)
	if path != name {
		absolute, pathErr := filepath.Abs(path)
		if pathErr != nil || absolute != filename {
			return nil, errors.New("recovery file must be inside this note store")
		}
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("recovery must be a regular file, not a symlink")
	}
	ciphertext, err := readBounded(filename, maxDocumentSize)
	if err != nil {
		return nil, err
	}
	if envelopeErr := validateAgeEnvelope(ciphertext, "X25519"); envelopeErr != nil {
		return nil, envelopeErr
	}
	plaintext, err := decryptAge(ciphertext, identity, maxPrivateSize)
	if err != nil {
		return nil, fmt.Errorf("decrypt recovery: %w", err)
	}
	document, err := decodePrivateDocument(plaintext, date)
	if err != nil {
		return nil, fmt.Errorf("invalid recovery document: %w", err)
	}
	return &Recovery{Date: date, Content: document.Content}, nil
}
