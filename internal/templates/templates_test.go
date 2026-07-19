package templates

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNormalizeFillsID(t *testing.T) {
	defs, err := Normalize([]Definition{{Name: "Meeting Notes", Body: "one"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].ID != "meeting-notes" {
		t.Fatalf("Normalize() = %+v", defs)
	}
}

func TestNormalizeRejectsInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name string
		defs []Definition
	}{
		{"duplicate id", []Definition{{Name: "Meeting Notes", Body: "one"}, {ID: "meeting-notes", Body: "two"}}},
		{"missing source", []Definition{{Name: "Empty"}}},
		{"multiple sources", []Definition{{Name: "Ambiguous", Body: "body", File: "file.md"}}},
		{"missing identity", []Definition{{Body: "body"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Normalize(tt.defs); err == nil {
				t.Errorf("Normalize(%+v) returned no error", tt.defs)
			}
		})
	}
}

func TestRenderMarkdownFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meeting.md")
	if err := os.WriteFile(path, []byte("- agenda\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	section, err := Render(Definition{ID: "meeting", Name: "Meeting", File: path}, "2026-07-19")
	if err != nil {
		t.Fatal(err)
	}
	if section.Body != "- agenda" || section.Title != "Meeting" {
		t.Errorf("section = %+v", section)
	}
}

func TestRenderCommandReceivesDate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	section, err := Render(Definition{
		ID:      "script",
		Name:    "Script",
		Command: []string{"sh", "-c", `printf 'Date: %s' "$SP_DATE"`},
	}, "2026-07-19")
	if err != nil {
		t.Fatal(err)
	}
	if section.Body != "Date: 2026-07-19" {
		t.Errorf("body = %q", section.Body)
	}
}

func TestCommandGetsMinimalEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	t.Setenv("SP_TEMPLATE_TEST_SECRET", "do-not-pass")
	section, err := Render(Definition{
		ID:      "env",
		Name:    "Environment",
		Command: []string{"sh", "-c", `printf '%s' "${SP_TEMPLATE_TEST_SECRET-unset}"`},
	}, "2026-07-19")
	if err != nil {
		t.Fatal(err)
	}
	if section.Body != "unset" {
		t.Errorf("inherited environment leaked to command: %q", section.Body)
	}
}

func TestCommandTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	_, err := runCommand(context.Background(), Definition{Name: "Slow", Command: []string{"sh", "-c", "sleep 1"}}, "2026-07-19", 20*time.Millisecond, 1024)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("runCommand() error = %v, want timeout", err)
	}
}

func TestCommandCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	_, err := RenderContext(ctx, Definition{
		ID: "canceled", Name: "Canceled", Command: []string{"sh", "-c", "sleep 1"},
	}, "2026-07-19")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("RenderContext() error = %v, want context.Canceled", err)
	}
}

func TestCommandOutputLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	_, err := runCommand(context.Background(), Definition{Name: "Large", Command: []string{"sh", "-c", "printf 123456789"}}, "2026-07-19", time.Second, 4)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("runCommand() error = %v, want output limit", err)
	}
}

func TestBuiltinTimebox(t *testing.T) {
	defs := Builtins()
	if len(defs) != 1 || defs[0].ID != "workday-timebox" {
		t.Fatalf("Builtins() = %+v", defs)
	}
	if !strings.Contains(defs[0].Body, "### Schedule") {
		t.Error("embedded timebox Markdown is missing its schedule section")
	}
}
