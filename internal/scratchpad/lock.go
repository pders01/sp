package scratchpad

import (
	"fmt"
	"os"
	"path/filepath"
)

// lockNote holds an OS advisory lock only across revision checking and saving.
// Closing the handle (or process exit) releases the lock. Keep the lock file:
// removing it would let another writer lock a different inode for the same note.
func lockNote(dir, date string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Join(dir, ".sp-"+date+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open scratchpad lock: %w", err)
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to lock scratchpad: %w", err)
	}
	return file, nil
}
