//go:build darwin || linux

package templates

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Re-exec only synthetic fixtures. The holder keeps BOTH output pipes and a
// readiness/lifetime connection open until exit. Even an escaped holder exits
// itself after two seconds; tests wait for its connection to close before ending.
func TestTemplateProcessHelper(t *testing.T) {
	args := os.Args
	if len(args) < 4 || args[len(args)-3] != "template-process-helper" {
		return
	}
	mode, address := args[len(args)-2], args[len(args)-1]
	if mode == "holder" {
		conn, err := net.DialTimeout("tcp", address, 2*time.Second)
		if err != nil {
			os.Exit(2)
		}
		if _, err := conn.Write([]byte{'R'}); err != nil {
			os.Exit(2)
		}
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}
	child := exec.Command(args[0], "-test.run=^TestTemplateProcessHelper$", "--", "template-process-helper", "holder", address)
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if mode == "escaped" || mode == "exited" {
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	if mode == "exited" || mode == "leader-exited" {
		os.Exit(0)
	}
	// The fixture parent is also bounded, including if its holder misbehaves.
	time.AfterFunc(5*time.Second, func() { _ = child.Process.Kill(); os.Exit(3) })
	_ = child.Wait()
	os.Exit(0)
}

func TestCommandInheritedPipes(t *testing.T) {
	for _, mode := range []string{"group", "escaped", "exited"} {
		triggers := []string{"cancel", "timeout", "caller-deadline"}
		if mode == "exited" {
			triggers = []string{"drain"}
		}
		for _, trigger := range triggers {
			t.Run(mode+"/"+trigger, func(t *testing.T) {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
					t.Fatal(err)
				}
				executable, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				def := Definition{Name: "Synthetic descendant", Command: []string{
					executable, "-test.run=^TestTemplateProcessHelper$", "--", "template-process-helper", mode, listener.Addr().String(),
				}}
				cause := errors.New("synthetic caller cause")
				ctx, cancel := context.WithCancelCause(context.Background())
				defer cancel(nil)
				var parent context.Context = ctx
				if trigger == "caller-deadline" {
					var stop context.CancelFunc
					parent, stop = context.WithTimeoutCause(ctx, 500*time.Millisecond, cause)
					defer stop()
				}
				timeout := 5 * time.Second
				if trigger == "timeout" {
					timeout = 500 * time.Millisecond
				}
				type outcome struct {
					output string
					err    error
				}
				done := make(chan outcome, 1)
				go func() {
					output, err := runCommand(parent, def, "2026-01-15", timeout, 1024)
					done <- outcome{output, err}
				}()
				conn, err := listener.Accept()
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				// On failure as well as success, wait for the bounded holder to
				// exit, rather than leaving live fixture descendants behind.
				defer func() {
					_ = conn.SetReadDeadline(time.Now().Add(6 * time.Second))
					_, err := io.Copy(io.Discard, conn)
					if err != nil {
						t.Errorf("holder did not self-terminate: %v", err)
					}
				}()
				_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				var ready [1]byte
				if _, err := io.ReadFull(conn, ready[:]); err != nil || ready[0] != 'R' {
					t.Fatalf("holder readiness = %q, %v", ready, err)
				}
				if trigger == "cancel" {
					cancel(cause)
				}
				select {
				case result := <-done:
					if result.output != "" {
						t.Errorf("failed command returned usable output: %q", result.output)
					}
					switch trigger {
					case "cancel":
						if !errors.Is(result.err, context.Canceled) || !errors.Is(result.err, cause) {
							t.Errorf("cancellation cause lost: %v", result.err)
						}
					case "timeout":
						if !errors.Is(result.err, context.DeadlineExceeded) || !strings.Contains(result.err.Error(), "timed out after 500ms") {
							t.Errorf("timeout = %v", result.err)
						}
					case "caller-deadline":
						if !errors.Is(result.err, context.DeadlineExceeded) || !errors.Is(result.err, cause) || strings.Contains(result.err.Error(), "timed out after 5s") {
							t.Errorf("caller deadline/cause misreported: %v", result.err)
						}
					case "drain":
						if !errors.Is(result.err, exec.ErrWaitDelay) {
							t.Errorf("incomplete drain = %v, want ErrWaitDelay", result.err)
						}
					}
				case <-time.After(1500 * time.Millisecond):
					t.Error("command waited for a descendant-held output pipe")
					cancel(cause)
					// Fixture processes are bounded; join the runner even on failure.
					select {
					case <-done:
					case <-time.After(6 * time.Second):
						t.Error("command runner failed to finish after fixture lifetime")
					}
				}
				if mode == "group" {
					_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
					if _, err := io.Copy(io.Discard, conn); err != nil {
						t.Errorf("owned group holder survived cancellation: %v", err)
					}
				}
			})
		}
	}
}
