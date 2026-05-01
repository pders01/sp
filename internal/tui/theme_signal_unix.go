//go:build !windows

package tui

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// watchThemeSignal blocks on SIGUSR1 until ctx is cancelled and calls
// notify on each delivery. SIGUSR1 is the cross-platform "the user
// changed terminal/system theme — please re-detect" hook; bind it from
// a wrapper script (e.g. `pkill -USR1 sp`) or a window manager rule.
func watchThemeSignal(ctx context.Context, notify func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	defer signal.Stop(ch)
	defer close(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			notify()
		}
	}
}
