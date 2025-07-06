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
