package editor

import (
	"os"
	"strings"
	"testing"
)

func TestParseEditorCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{"simple command", "vim", false},
		{"command with args", "vim -c 'set number'", false},
		{"empty command", "", true},
		{"whitespace only", "   ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseEditorCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseEditorCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsGUIEditor(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected bool
	}{
		{"vim", "vim", false},
		{"nvim", "nvim", false},
		{"nano", "nano", false},
		{"vscode", "code", true},
		{"sublime", "subl", true},
		{"notepad", "notepad", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isGUIEditor(tt.command)
			if result != tt.expected {
				t.Errorf("isGUIEditor(%s) = %v, want %v", tt.command, result, tt.expected)
			}
		})
	}
}

func TestGetFallbackEditors(t *testing.T) {
	fallbacks := getFallbackEditors()
	if len(fallbacks) == 0 {
		t.Error("getFallbackEditors() returned empty list")
	}

	// Check that we have at least one common editor
	commonEditors := []string{"vim", "nano"}
	found := false
	for _, editor := range commonEditors {
		for _, fallback := range fallbacks {
			if strings.Contains(fallback, editor) {
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		t.Error("getFallbackEditors() doesn't contain any common editors")
	}
}

func TestRunPreparedRetainsBuffer(t *testing.T) {
	for _, mode := range []string{"success", "failure"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SP_EDITOR_HELPER", mode)
			ed := &Editor{
				command: os.Args[0],
				args:    []string{"-test.run=^TestEditorHelperProcess$", "--"},
			}
			cmd, path, cleanup, err := ed.Prepare("original")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			got, err := ed.RunPrepared(cmd, path)
			if (err != nil) != (mode == "failure") {
				t.Errorf("RunPrepared error = %v, mode = %s", err, mode)
			}
			if mode == "success" && got != "edited" {
				t.Errorf("edited content = %q", got)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil || string(data) != "edited" {
				t.Errorf("editor buffer not retained: %q, %v", data, readErr)
			}
			cleanup()
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Errorf("explicit cleanup did not remove buffer: %v", statErr)
			}
		})
	}
}

// Re-execute the test binary as a noninteractive editor fixture.
func TestEditorHelperProcess(t *testing.T) {
	mode := os.Getenv("SP_EDITOR_HELPER")
	if mode == "" {
		return
	}
	if err := os.WriteFile(os.Args[len(os.Args)-1], []byte("edited"), 0o600); err != nil {
		os.Exit(2)
	}
	if mode == "failure" {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestDetectEditorWithEnv(t *testing.T) {
	// Test with EDITOR environment variable
	originalEditor := os.Getenv("EDITOR")
	defer os.Setenv("EDITOR", originalEditor)

	// Set a mock editor (this won't actually exist, but we can test the logic)
	os.Setenv("EDITOR", "nonexistent-editor")

	_, err := detectEditor()
	if err == nil {
		t.Error("detectEditor() should fail with nonexistent editor")
	}
}
