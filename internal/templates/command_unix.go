//go:build darwin || linux

package templates

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// runTemplateProcess owns a fresh process group until its output is drained or
// cancelled. Keep its leader unreaped throughout that interval: late negative-
// PID signals must never address an unrelated, reused process-group ID.
func runTemplateProcess(ctx context.Context, cmd *exec.Cmd) error {
	return runTemplateProcessObserved(ctx, cmd, waitTemplateExit)
}

// The operation-local observer permits deterministic exit/cancellation tests.
func runTemplateProcessObserved(ctx context.Context, cmd *exec.Cmd, observeExit func(int) error) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, stderr := cmd.Stdout, cmd.Stderr
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	defer outRead.Close()
	defer outWrite.Close()
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	defer errRead.Close()
	defer errWrite.Close()
	// Real file outputs stop exec.Cmd from reaping before its internal pipe
	// goroutines finish. We drain explicitly while the group ID remains owned.
	cmd.Stdout, cmd.Stderr = outWrite, errWrite
	var mu sync.Mutex
	owned := true
	killGroup := func() error {
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
	// Close the parent's writer copies; only the child/descendants hold them.
	_ = outWrite.Close()
	_ = errWrite.Close()
	drained := make(chan error, 2)
	copyOutput := func(dst io.Writer, src *os.File) {
		if dst == nil {
			dst = io.Discard
		}
		_, err := io.Copy(dst, src)
		drained <- err
	}
	go copyOutput(stdout, outRead)
	go copyOutput(stderr, errRead)
	observeErr := observeExit(cmd.Process.Pid)
	// The leader is now a zombie, not yet reaped; cancellation can still safely
	// address its group even when a descendant retains pipes after leader exit.
	delay := cmd.WaitDelay
	if delay <= 0 {
		delay = commandWaitDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	remaining := 2
	var drainErr error
	timedOut := false
	cancelled := ctx.Err() != nil || observeErr != nil
	for remaining > 0 && !cancelled && !timedOut {
		select {
		case err := <-drained:
			remaining--
			drainErr = errors.Join(drainErr, err)
		case <-ctx.Done():
			cancelled = true
		case <-timer.C:
			timedOut = true
		}
	}
	mu.Lock()
	var cancelErr error
	if cancelled || timedOut || ctx.Err() != nil {
		cancelErr = killGroup()
		if errors.Is(cancelErr, os.ErrProcessDone) {
			cancelErr = nil
		}
	}
	// No further group signal is safe after cmd.Wait reaps the leader.
	owned = false
	mu.Unlock()
	// Escaped descendants are not contained; closing our readers still bounds
	// their inherited-pipe wait. Join the copies before inspecting their buffers.
	_ = outRead.Close()
	_ = errRead.Close()
	for remaining > 0 {
		drainErr = errors.Join(drainErr, <-drained)
		remaining--
	}
	if cancelled || timedOut {
		// Closing a pipe is expected on cancellation/timeout, not an extra failure.
		drainErr = nil
	}
	if timedOut {
		drainErr = errors.Join(drainErr, exec.ErrWaitDelay)
	}
	return errors.Join(cmd.Wait(), observeErr, cancelErr, drainErr)
}
