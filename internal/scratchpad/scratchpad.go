package scratchpad

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"filippo.io/age"

	"github.com/pders01/sp/internal/templates"
)

// Scratchpad represents a daily scratchpad entry
type Scratchpad struct {
	Date             string    `json:"date"`
	Content          string    `json:"content"`
	AppliedTemplates []string  `json:"applied_templates,omitempty"`
	Created          time.Time `json:"created"`
	Modified         time.Time `json:"modified"`
	Agent            AgentArea `json:"agent,omitempty"`

	// Snapshot of the on-disk version this value was loaded from. New values
	// expect an absent file; successful saves advance the snapshot.
	revision [sha256.Size]byte
	exists   bool
	disk     *diskRecord
}

// ErrDurabilityUnconfirmed means the replacement succeeded, but syncing its
// directory failed. The new note is visible; it has not been rolled back.
var ErrDurabilityUnconfirmed = errors.New("scratchpad replaced, but crash durability could not be confirmed")

// ErrConflict means a note changed or disappeared since it was loaded.
// Callers must retain unsaved edits and reload rather than overwrite it.
var ErrConflict = errors.New("scratchpad changed since it was loaded")

// Manager handles scratchpad operations
type Manager struct {
	storageDir        string
	keyMu             sync.RWMutex
	identity          *age.X25519Identity
	keyMetadata       [sha256.Size]byte
	recoveryRecipient string // Public key pinned by unlock; survives Lock for safe recovery.
	// Remember observation across Lock so disappearing metadata cannot silently
	// opt this manager out, even if no encrypted daily page exists yet.
	observedPrivacy [sha256.Size]byte
}

// NewManager creates a new scratchpad manager
func NewManager() (*Manager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	return NewManagerAt(filepath.Join(homeDir, ".sp"))
}

// NewManagerAt opens a store without enabling privacy or migrating any notes.
func NewManagerAt(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}
	return &Manager{storageDir: dir}, nil
}

// GetToday returns today's scratchpad, or a new unsaved page if none exists.
func (m *Manager) GetToday() (*Scratchpad, error) {
	today := time.Now().Format("2006-01-02")
	return m.GetByDate(today)
}

// GetByDate returns a scratchpad for a specific date
func (m *Manager) GetByDate(date string) (*Scratchpad, error) {
	if err := validateDate(date); err != nil {
		return nil, err
	}
	// Reads inspect this page rather than rescanning the whole legacy archive.
	// Saves still perform the full orphan-ciphertext check before plaintext writes.
	metadata, _, err := m.readMetadata()
	if err != nil {
		return nil, err
	}
	if metadata != nil {
		if metadata.State == "decrypting" {
			return nil, ErrPrivacyDecrypting
		}
		if metadata.State != "disabled" && !m.IsUnlocked() {
			return nil, ErrLocked
		}
	}
	filename := filepath.Join(m.storageDir, date+".json")

	data, err := readBounded(filename, maxDocumentSize)
	if os.IsNotExist(err) {
		// Create new scratchpad for this date
		scratchpad := &Scratchpad{
			Date:     date,
			Content:  "",
			Created:  time.Now(),
			Modified: time.Now(),
		}
		return scratchpad, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read scratchpad file: %w", err)
	}

	record, err := decodeRecord(data, date)
	if err != nil {
		return nil, err
	}
	scratchpad, err := m.openRecord(record)
	if err != nil {
		return nil, err
	}
	scratchpad.revision = sha256.Sum256(data)
	scratchpad.exists = true
	scratchpad.disk = record
	return scratchpad, nil
}

// Save writes a replacement file before renaming it over the note. On Unix the
// rename is atomic and the directory is synced afterward. Other platforms use
// os.Rename without a directory-sync or atomic-replacement guarantee.
// Modified changes only once replacement succeeds, including when the result is
// ErrDurabilityUnconfirmed. Cooperating SP writers check the loaded revision
// under a short per-note lock; stale writes return ErrConflict.
func (m *Manager) Save(scratchpad *Scratchpad) error {
	return m.save(scratchpad, syncDirectory)
}

// The sync callback lets tests exercise a failure after publication without
// requiring a failing filesystem or changing global state.
func (m *Manager) save(scratchpad *Scratchpad, syncDir func(string) error) error {
	return m.saveInternal(scratchpad, syncDir, false)
}

func (m *Manager) saveInternal(scratchpad *Scratchpad, syncDir func(string) error, migrating bool) error {
	if scratchpad == nil {
		return errors.New("cannot save a nil scratchpad")
	}
	if err := validateDate(scratchpad.Date); err != nil {
		return err
	}
	storeLock, err := lockNote(m.storageDir, "privacy")
	if err != nil {
		return err
	}
	defer storeLock.Close()
	enabled, err := m.PrivacyEnabled()
	if err != nil {
		return err
	}
	if migrating && !enabled {
		return errors.New("privacy policy changed during migration; retry sp privacy enable")
	}
	if enabled && !m.IsUnlocked() {
		return ErrLocked
	}
	lock, err := lockNote(m.storageDir, scratchpad.Date)
	if err != nil {
		return err
	}
	defer lock.Close()

	filename := filepath.Join(m.storageDir, scratchpad.Date+".json")
	current, err := readBounded(filename, maxDocumentSize)
	switch {
	case os.IsNotExist(err):
		if scratchpad.exists {
			return fmt.Errorf("%w: %s was deleted", ErrConflict, scratchpad.Date)
		}
	case err != nil:
		return fmt.Errorf("failed to check scratchpad revision: %w", err)
	case !scratchpad.exists || sha256.Sum256(current) != scratchpad.revision:
		return fmt.Errorf("%w: %s", ErrConflict, scratchpad.Date)
	}
	if len(current) != 0 {
		if _, decodeErr := decodeRecord(current, scratchpad.Date); decodeErr != nil {
			return decodeErr
		}
	}
	pending := *scratchpad
	if !migrating {
		pending.Modified = time.Now()
	}
	data, record, err := m.encodeScratchpad(&pending, enabled)
	if err != nil {
		return err
	}
	_, err = publish(filename, data, false, func(dir string) error {
		// Publication advances the revision before the durability check, even
		// when that check fails (callers may inspect the note in the callback).
		scratchpad.Modified = pending.Modified
		scratchpad.revision = sha256.Sum256(data)
		scratchpad.exists = true
		scratchpad.disk = record
		return syncDir(dir)
	})
	return err
}

// ApplyTemplateSections appends previously unused template sections to date.
// Applied IDs live in JSON metadata, keeping implementation markers out of the
// user's Markdown. Existing content is never replaced. A published replacement
// with unconfirmed durability returns the updated note alongside its error.
func (m *Manager) ApplyTemplateSections(date string, sections []templates.Section) (*Scratchpad, error) {
	return m.applyTemplateSections(date, sections, syncDirectory)
}

func (m *Manager) applyTemplateSections(date string, sections []templates.Section, syncDir func(string) error) (*Scratchpad, error) {
	scratchpad, err := m.GetByDate(date)
	if err != nil {
		return nil, err
	}

	applied := make(map[string]bool, len(scratchpad.AppliedTemplates))
	for _, id := range scratchpad.AppliedTemplates {
		applied[id] = true
	}

	content := scratchpad.Content
	changed := false
	for _, section := range sections {
		alreadyApplied := applied[section.ID]
		if section.ID == "" || (alreadyApplied && !section.Force) {
			continue
		}
		if content != "" {
			trailingNewlines := len(content) - len(strings.TrimRight(content, "\n"))
			if trailingNewlines < 2 {
				content += strings.Repeat("\n", 2-trailingNewlines)
			}
		}
		content += "## " + strings.TrimSpace(section.Title) + "\n\n" + strings.TrimSpace(section.Body) + "\n"
		if !alreadyApplied {
			scratchpad.AppliedTemplates = append(scratchpad.AppliedTemplates, section.ID)
			applied[section.ID] = true
		}
		changed = true
	}
	if !changed {
		return scratchpad, nil
	}
	scratchpad.Content = content
	if err := m.save(scratchpad, syncDir); err != nil {
		if errors.Is(err, ErrDurabilityUnconfirmed) {
			return scratchpad, err
		}
		return nil, err
	}
	return scratchpad, nil
}

// ListDates returns all available scratchpad dates
func (m *Manager) ListDates() ([]string, error) {
	files, err := os.ReadDir(m.storageDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read storage directory: %w", err)
	}

	var dates []string
	for _, file := range files {
		if !file.Type().IsRegular() || filepath.Ext(file.Name()) != ".json" {
			continue
		}
		date := strings.TrimSuffix(file.Name(), ".json")
		if validateDate(date) == nil {
			dates = append(dates, date)
		}
	}

	return dates, nil
}

// Delete removes a scratchpad for a specific date
func (m *Manager) Delete(date string) error {
	if err := validateDate(date); err != nil {
		return err
	}
	storeLock, err := lockNote(m.storageDir, "privacy")
	if err != nil {
		return err
	}
	defer storeLock.Close()
	if _, err := m.PrivacyEnabled(); err != nil {
		return err
	}
	lock, err := lockNote(m.storageDir, date)
	if err != nil {
		return err
	}
	defer lock.Close()
	filename := filepath.Join(m.storageDir, date+".json")
	return os.Remove(filename)
}

func validateDate(date string) error {
	parsed, err := time.Parse(time.DateOnly, date)
	if err != nil || parsed.Format(time.DateOnly) != date {
		return fmt.Errorf("invalid scratchpad date %q: expected a valid YYYY-MM-DD date", date)
	}
	return nil
}
