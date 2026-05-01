//go:build !darwin

package tui

import (
	"context"
	"sync"
)

// watchSystemTheme is a no-op on non-Darwin builds. Linux desktop
// environments expose appearance via dconf/gsettings or
// org.freedesktop.appearance, but coverage is too fragmented for a
// single watcher; users on those platforms can wire the SIGUSR1 hook
// or set a static theme in config.
func watchSystemTheme(_ context.Context, _ *sync.WaitGroup, _ func()) error {
	return nil
}
