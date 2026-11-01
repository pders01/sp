//go:build darwin || linux

package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/templates"
)

// All subprocesses here are synthetic and self-terminating, even if template
// cancellation regresses. The holder's connection stays open until process exit.
func TestTemplateShutdownProcessHelper(t *testing.T) {
	args := os.Args
	if len(args) < 4 || args[len(args)-3] != "template-shutdown-helper" {
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
	child := exec.Command(args[0], "-test.run=^TestTemplateShutdownProcessHelper$", "--", "template-shutdown-helper", "holder", address)
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	time.AfterFunc(5*time.Second, func() { _ = child.Process.Kill(); os.Exit(3) })
	_ = child.Wait()
	os.Exit(0)
}

func TestTemplateWorkflowInheritedPipesCancelAndShutdown(t *testing.T) {
	for _, action := range []string{"escape", "quit", "close-without-completion"} {
		t.Run(action, func(t *testing.T) {
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
			def := templates.Definition{Name: "Synthetic shutdown", Command: []string{
				executable, "-test.run=^TestTemplateShutdownProcessHelper$", "--", "template-shutdown-helper", "parent", listener.Addr().String(),
			}}
			app := newTestApp(ModeNotebook)
			safeToClose := true
			t.Cleanup(func() {
				if safeToClose {
					app.Close()
				}
			})
			app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			app.SetTemplates([]DayTemplate{{ID: "one", Name: "Synthetic"}}, nil, func(ctx context.Context, date string, _ []TemplateSelection) (TemplateApplyResult, error) {
				_, err := templates.RenderContext(ctx, def, date)
				// No store or publication is involved in this synthetic applier.
				return TemplateApplyResult{}, err
			})
			app.Update(privacyKey("a"))
			locked := false
			app.SetPrivacy(PrivacyHooks{Lock: func() { locked = true }})
			app.Update(privacyKey(" "))
			completion := templateWorkflowStart(t, app)
			attempt := app.templateChooser.attempt
			date := app.templateChooser.date
			conn, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			defer func() {
				_ = conn.SetReadDeadline(time.Now().Add(6 * time.Second))
				if _, err := io.Copy(io.Discard, conn); err != nil {
					t.Errorf("holder did not self-terminate: %v", err)
				}
			}()
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			var ready [1]byte
			if _, err := io.ReadFull(conn, ready[:]); err != nil || ready[0] != 'R' {
				t.Fatalf("holder readiness = %q, %v", ready, err)
			}

			switch action {
			case "escape":
				app.Update(tea.KeyMsg{Type: tea.KeyEsc})
			case "quit":
				if _, cmd := app.Update(privacyKey("q")); cmd == nil || !app.IsQuitting() {
					t.Fatal("quit did not request shutdown")
				}
			}
			// Only this goroutine owns the App until it joins; the test's
			// deadline detects stalls without racing App/Privacy state access.
			closed := make(chan struct{})
			safeToClose = false
			go func() {
				defer close(closed)
				if action == "escape" {
					app.Update(completion())
				}
				app.Close()
			}()
			select {
			case <-closed:
			case <-time.After(1500 * time.Millisecond):
				t.Error("template work stalled shutdown/key release on inherited pipes")
				select {
				case <-closed:
				case <-time.After(6 * time.Second):
					// Avoid concurrent App cleanup/state inspection on failure.
					t.Error("shutdown failed even after bounded fixtures exited")
					return
				}
			}
			safeToClose = true
			if !locked {
				t.Error("shutdown did not reach privacy key release")
			}
			select {
			case <-attempt.done:
			default:
				t.Fatal("shutdown returned with active template work")
			}
			if !errors.Is(attempt.result.err, context.Canceled) || attempt.result.result.Published || app.templateApplied(date, "one") {
				t.Errorf("cancellation/publication outcome misreported: %+v", attempt.result)
			}
			var report bytes.Buffer
			app.ReportRecoveries(&report)
			if strings.Contains(report.String(), "published before shutdown") {
				t.Errorf("unpublished cancellation claimed a commit: %q", report.String())
			}
			_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			if _, err := io.Copy(io.Discard, conn); err != nil {
				t.Errorf("owned descendant survived shutdown: %v", err)
			}
		})
	}
}
