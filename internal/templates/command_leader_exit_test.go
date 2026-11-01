//go:build darwin || linux

package templates

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestCommandCancellationOwnsGroupAfterLeaderExit(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestTemplateProcessHelper$", "--", "template-process-helper", "leader-exited", listener.Addr().String())
	cmd.Stdout, cmd.Stderr = &limitedBuffer{limit: 1024}, &limitedBuffer{limit: 1024}
	cmd.WaitDelay = commandWaitDelay
	observed := make(chan struct{})
	proceed := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(proceed)
		}
	}()
	done := make(chan error, 1)
	go func() {
		done <- runTemplateProcessObserved(ctx, cmd, func(pid int) error {
			err := waitTemplateExit(pid)
			close(observed)
			<-proceed
			return err
		})
	}()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer func() {
		_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
		if _, err := io.Copy(io.Discard, conn); err != nil {
			t.Errorf("bounded holder did not terminate: %v", err)
		}
	}()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var ready [1]byte
	if _, err := io.ReadFull(conn, ready[:]); err != nil || ready[0] != 'R' {
		t.Fatalf("holder readiness: %q, %v", ready, err)
	}
	select {
	case <-observed:
	case <-time.After(5 * time.Second):
		t.Fatal("leader exit was not observed")
	}
	cancel()
	close(proceed)
	released = true
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation stalled after leader exit")
	}
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err := io.Copy(io.Discard, conn); err != nil {
		t.Errorf("owned same-group holder survived cancellation after leader exit: %v", err)
	}
	// The group has now been disarmed/reaped. A stale callback must not send
	// another negative-PID signal after ownership is gone.
	if err := cmd.Cancel(); err != os.ErrProcessDone {
		t.Fatalf("late cancellation did not reject stale ownership: %v", err)
	}
}
