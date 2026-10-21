package scratchpad

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pders01/sp/internal/templates"
)

// Scratchpad represents a daily scratchpad entry
type Scratchpad struct {
	Date             string    `json:"date"`
	Content          string    `json:"content"`
	AppliedTemplates []string  `json:"applied_templates,omitempty"`
	Created          time.Time `json:"created"`
	Modified         time.Time `json:"modified"`

	// Snapshot of the on-disk version this value was loaded from. New values
	// expect an absent file; successful saves advance the snapshot.
	revision [sha256.Size]byte
	exists   bool
}

// ErrDurabilityUnconfirmed means the replacement succeeded, but syncing its
// directory failed. The new note is visible; it has not been rolled back.
var ErrDurabilityUnconfirmed = errors.New("scratchpad replaced, but crash durability could not be confirmed")

// ErrConflict means a note changed or disappeared since it was loaded.
// Callers must retain unsaved edits and reload rather than overwrite it.
var ErrConflict = errors.New("scratchpad changed since it was loaded")

// Manager handles scratchpad operations
type Manager struct {
	storageDir string
}

// NewManager creates a new scratchpad manager
func NewManager() (*Manager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	storageDir := filepath.Join(homeDir, ".sp")
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	return &Manager{storageDir: storageDir}, nil
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
	filename := filepath.Join(m.storageDir, date+".json")

	data, err := os.ReadFile(filename)
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

	var scratchpad Scratchpad
	if err := json.Unmarshal(data, &scratchpad); err != nil {
		return nil, fmt.Errorf("failed to parse scratchpad file: %w", err)
	}

	if scratchpad.Date != date {
		return nil, fmt.Errorf("scratchpad date %q does not match requested date %q", scratchpad.Date, date)
	}
	scratchpad.revision = sha256.Sum256(data)
	scratchpad.exists = true
	return &scratchpad, nil
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
	if scratchpad == nil {
		return errors.New("cannot save a nil scratchpad")
	}
	if err := validateDate(scratchpad.Date); err != nil {
		return err
	}
	lock, err := lockNote(m.storageDir, scratchpad.Date)
	if err != nil {
		return err
	}
	defer lock.Close()

	filename := filepath.Join(m.storageDir, scratchpad.Date+".json")
	current, err := os.ReadFile(filename)
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
	pending := *scratchpad
	pending.Modified = time.Now()

	data, err := json.MarshalIndent(&pending, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal scratchpad: %w", err)
	}

	tmp, err := os.CreateTemp(m.storageDir, ".sp-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary scratchpad: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close() // Ensures closure on early returns.

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("failed to write temporary scratchpad: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("failed to sync temporary scratchpad: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temporary scratchpad: %w", err)
	}

	if err := os.Rename(tmp.Name(), filename); err != nil {
		return fmt.Errorf("failed to replace scratchpad file: %w", err)
	}

	// Publication has happened even if the durability check below fails.
	scratchpad.Modified = pending.Modified
	scratchpad.revision = sha256.Sum256(data)
	scratchpad.exists = true
	if err := syncDir(m.storageDir); err != nil {
		return fmt.Errorf("%w: sync storage directory: %w", ErrDurabilityUnconfirmed, err)
	}
	return nil
}

// ApplyTemplateSections appends previously unused template sections to date.
// Applied IDs live in JSON metadata, keeping implementation markers out of the
// user's Markdown. Existing content is never replaced.
func (m *Manager) ApplyTemplateSections(date string, sections []templates.Section) (*Scratchpad, error) {
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
	if err := m.Save(scratchpad); err != nil {
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
