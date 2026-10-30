// privacy-fixture creates a fresh, synthetic locked store for adversarial tests.
// It never prints or persists the randomly generated passphrase or plaintext
// canary. Run from the repository with: go run ./scripts/privacy-fixture -dir PATH
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pders01/sp/internal/scratchpad"
)

func main() {
	dir := flag.String("dir", "", "new directory for disposable synthetic notes (must not exist)")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "-dir is required; never point this tool at your real note store")
		os.Exit(2)
	}
	if err := run(*dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(dir string) error {
	// Refuse existing directories, including real stores, rather than accepting
	// an override/force flag. The caller must supply a fresh disposable path.
	if err := os.Mkdir(dir, 0o700); err != nil {
		return fmt.Errorf("fixture requires a new directory: %w", err)
	}
	m, err := scratchpad.NewManagerAt(dir)
	if err != nil {
		return err
	}
	defer m.Lock()
	var secret, randomContent [32]byte
	if _, randomErr := rand.Read(secret[:]); randomErr != nil {
		return randomErr
	}
	if _, randomErr := rand.Read(randomContent[:]); randomErr != nil {
		return randomErr
	}
	passphrase := []byte(base64.RawURLEncoding.EncodeToString(secret[:]))
	clear(secret[:])
	defer clear(passphrase)
	content := "SYNTHETIC PRIVATE CANARY " + base64.RawURLEncoding.EncodeToString(randomContent[:])
	clear(randomContent[:])
	const date = "2026-10-01"
	note := &scratchpad.Scratchpad{Date: date, Content: content, Created: time.Now()}
	if saveErr := m.Save(note); saveErr != nil {
		return saveErr
	}
	if enableErr := m.EnablePrivacy(passphrase); enableErr != nil {
		return enableErr
	}
	if appendErr := m.Append(date, "Public project", "synthetic-fixture", []string{"- Public synthetic contribution"}); appendErr != nil {
		return appendErr
	}
	loaded, err := m.GetByDate(date)
	if err != nil || loaded.Content != content {
		return fmt.Errorf("fixture round trip failed: %v", err)
	}
	if _, recoveryErr := m.SealRecovery(date, content); recoveryErr != nil {
		return recoveryErr
	}
	if traversalErr := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(data, []byte(content)) || bytes.Contains(data, passphrase) {
			return fmt.Errorf("fixture setup leaked plaintext into %s", entry.Name())
		}
		return nil
	}); traversalErr != nil {
		return traversalErr
	}
	m.Lock()
	digest := sha256.Sum256([]byte(content))
	return json.NewEncoder(os.Stdout).Encode(struct {
		Directory    string `json:"directory"`
		Date         string `json:"date"`
		CanarySHA256 string `json:"canary_sha256"`
		Description  string `json:"description"`
	}{dir, date, hex.EncodeToString(digest[:]), "Synthetic locked store. Random passphrase was never printed or persisted. No real user notes used."})
}
