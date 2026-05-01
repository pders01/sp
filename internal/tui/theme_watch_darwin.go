//go:build darwin

package tui

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// watchSystemTheme installs an fsnotify watcher on the macOS global
// preferences plist. Toggling Dark/Light Appearance rewrites this file
// (the "AppleInterfaceStyle" key appears or disappears), so a Write
// event is a reliable proxy for "system appearance changed".
//
// We intentionally watch the parent directory rather than the file
// itself: cfprefsd rewrites preferences via atomic rename, which would
// otherwise leave fsnotify watching a deleted inode. Filtering by
// basename avoids false positives from the dozens of other plists
// macOS rewrites under the same directory.
func watchSystemTheme(ctx context.Context, wg *sync.WaitGroup, notify func()) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	prefsDir := filepath.Join(home, "Library", "Preferences")
	target := filepath.Join(prefsDir, ".GlobalPreferences.plist")

	if _, statErr := os.Stat(target); statErr != nil {
		return fmt.Errorf("stat %s: %w", target, statErr)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	if err := watcher.Add(prefsDir); err != nil {
		_ = watcher.Close()
		return fmt.Errorf("watch %s: %w", prefsDir, err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer watcher.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Base(ev.Name) != ".GlobalPreferences.plist" {
					continue
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}
				notify()
			case werr, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("sp: theme watcher error: %v", werr)
			}
		}
	}()
	return nil
}
