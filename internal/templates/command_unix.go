//go:build darwin || linux

package templates

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

// runTemplateProcess gives the supported Unix targets a fresh process group.
// Only signal that group while its leader is still our unreaped child: a late
// negative-PID kill after Wait could otherwise hit a reused process-group ID.
// Descendants that change groups/sessions are not contained; WaitDelay still
// bounds their inherited output pipes. No process discovery is involved.
func runTemplateProcess(ctx context.Context, cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var mu sync.Mutex
	owned := true
	killGroup := func() error {
		// Setpgid makes the child's positive PID its new group ID.
		if cmd.Process == nil || cmd.Process.Pid <= 0 {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Cancel = func() error {
		mu.Lock()
		defer mu.Unlock()
		if !owned {
			return os.ErrProcessDone
		}
		return killGroup()
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// Observe exit WITHOUT reaping. Serialize the last possible group signal
	// with disarming Cancel, then let exec.Wait reap and drain (bounded by
	// WaitDelay). This also avoids signals sent by delayed cleanup callbacks.
	observeErr := waitTemplateExit(cmd.Process.Pid)
	mu.Lock()
	var cancelErr error
	if observeErr != nil || ctx.Err() != nil {
		cancelErr = killGroup()
		if errors.Is(cancelErr, os.ErrProcessDone) {
			cancelErr = nil
		}
	}
	owned = false
	mu.Unlock()
	return errors.Join(cmd.Wait(), observeErr, cancelErr)
}
