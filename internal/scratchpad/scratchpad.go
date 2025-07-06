package scratchpad

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Scratchpad represents a daily scratchpad entry
type Scratchpad struct {
	Date     string    `json:"date"`
	Content  string    `json:"content"`
	Created  time.Time `json:"created"`
	Modified time.Time `json:"modified"`
}

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
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	return &Manager{storageDir: storageDir}, nil
}

// GetToday returns today's scratchpad, creating it if it doesn't exist
func (m *Manager) GetToday() (*Scratchpad, error) {
	today := time.Now().Format("2006-01-02")
	return m.GetByDate(today)
}

// GetByDate returns a scratchpad for a specific date
func (m *Manager) GetByDate(date string) (*Scratchpad, error) {
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

	return &scratchpad, nil
}

// Save saves a scratchpad to disk
func (m *Manager) Save(scratchpad *Scratchpad) error {
	scratchpad.Modified = time.Now()

	data, err := json.MarshalIndent(scratchpad, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal scratchpad: %w", err)
	}

	filename := filepath.Join(m.storageDir, scratchpad.Date+".json")
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write scratchpad file: %w", err)
	}

	return nil
}

// ListDates returns all available scratchpad dates
func (m *Manager) ListDates() ([]string, error) {
	files, err := os.ReadDir(m.storageDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read storage directory: %w", err)
	}

	var dates []string
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".json" {
			date := file.Name()[:len(file.Name())-5] // Remove .json extension
			dates = append(dates, date)
		}
	}

	return dates, nil
}

// Delete removes a scratchpad for a specific date
func (m *Manager) Delete(date string) error {
	filename := filepath.Join(m.storageDir, date+".json")
	return os.Remove(filename)
}
