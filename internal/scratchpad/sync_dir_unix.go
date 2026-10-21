//go:build unix

package scratchpad

import "os"

// syncDirectory persists the directory entry changed by the file rename.
func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return err
	}
	return dir.Close()
}
