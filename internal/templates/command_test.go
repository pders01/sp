package templates

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	missing := filepath.Join(t.TempDir(), "missing-synthetic-command")
	output, err := runCommand(context.Background(), Definition{Name: "Missing", Command: []string{missing}}, "2026-01-15", time.Second, 1024)
	if !errors.Is(err, os.ErrNotExist) || output != "" {
		t.Fatalf("start failure = %q, %v", output, err)
	}
}
