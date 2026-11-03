package templates

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderAlreadyCancelledPreservesCause(t *testing.T) {
	cause := errors.New("synthetic pre-cancellation")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	_, err := RenderContext(ctx, Definition{Name: "Synthetic", Body: "not rendered"}, "2026-01-15")
	if !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
		t.Fatalf("pre-cancellation cause lost: %v", err)
	}
}

func TestCommandStartFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	for _, tc := range []struct{ name, command string }{
		{"absolute", filepath.Join(dir, "missing-synthetic-command")},
		{"path_lookup", "missing-synthetic-command"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := runCommand(context.Background(), Definition{Name: "Missing", Command: []string{tc.command}}, "2026-01-15", time.Second, 1024)
			// Unix can reach StartProcess for an absolute missing path, while
			// Windows resolves executable extensions before starting. PATH
			// lookup also returns exec.ErrNotFound on Unix. Keep both typed
			// not-found causes without accepting unrelated launch failures.
			if (!errors.Is(err, os.ErrNotExist) && !errors.Is(err, exec.ErrNotFound)) || output != "" {
				t.Fatalf("start failure = %q, %v", output, err)
			}
			if !strings.Contains(err.Error(), `run template "Missing"`) {
				t.Fatalf("start failure lost template context: %v", err)
			}
			var pathErr *os.PathError
			var lookupErr *exec.Error
			switch {
			case errors.As(err, &pathErr):
				if pathErr.Path != tc.command {
					t.Fatalf("start failure path = %q, want %q", pathErr.Path, tc.command)
				}
			case errors.As(err, &lookupErr):
				if lookupErr.Name != tc.command {
					t.Fatalf("lookup failure name = %q, want %q", lookupErr.Name, tc.command)
				}
			default:
				t.Fatalf("start failure lost its typed cause: %v", err)
			}
		})
	}
}
