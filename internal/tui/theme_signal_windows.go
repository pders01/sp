//go:build windows

package tui

import "context"

// Windows has no SIGUSR1 equivalent. The TUI toggle key remains the
// only manual trigger on this platform.
func watchThemeSignal(ctx context.Context, _ func()) {
	<-ctx.Done()
}
