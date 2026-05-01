package editor

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Editor represents an external editor
type Editor struct {
	command string
	args    []string
	isGUI   bool
}

// NewEditor creates a new editor instance
func NewEditor() (*Editor, error) {
	editor, err := detectEditor()
	if err != nil {
		return nil, fmt.Errorf("failed to detect editor: %w", err)
	}
	return editor, nil
}

// Edit opens content in the user's preferred editor. The filename
// argument is reserved for future use (so the temp file can carry a
// meaningful suffix); right now we always use sp-*.md.
func (e *Editor) Edit(content, _ string) (string, error) {
	// Create temporary file
	tmpFile, err := os.CreateTemp("", "sp-*.md")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write content to temp file
	if _, werr := tmpFile.WriteString(content); werr != nil {
		return "", fmt.Errorf("failed to write to temp file: %w", werr)
	}
	tmpFile.Close()

	// Build command. Copy e.args so we don't mutate the editor's slice.
	args := make([]string, 0, len(e.args)+1)
	args = append(args, e.args...)
	args = append(args, tmpFile.Name())
	cmd := exec.Command(e.command, args...)

	// Set up I/O
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// For GUI editors, we need to wait for the file to be modified
	if e.isGUI {
		return e.editGUI(cmd, tmpFile.Name())
	}

	// For terminal editors, run in foreground
	if rerr := cmd.Run(); rerr != nil {
		return "", fmt.Errorf("editor failed: %w", rerr)
	}

	// Read back the content
	data, rerr := os.ReadFile(tmpFile.Name())
	if rerr != nil {
		return "", fmt.Errorf("failed to read edited file: %w", rerr)
	}

	return string(data), nil
}

// editGUI handles GUI editors by waiting for file modification
func (e *Editor) editGUI(cmd *exec.Cmd, filename string) (string, error) {
	// Get initial file info
	initialInfo, err := os.Stat(filename)
	if err != nil {
		return "", err
	}

	// Start editor in background
	if serr := cmd.Start(); serr != nil {
		return "", fmt.Errorf("failed to start editor: %w", serr)
	}

	// Wait for file modification (with timeout)
	modified := false
	timeout := time.After(5 * time.Minute) // 5 minute timeout
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			_ = cmd.Process.Kill()
			return "", fmt.Errorf("editor timeout")
		case <-ticker.C:
			info, statErr := os.Stat(filename)
			if statErr != nil {
				continue
			}
			if info.ModTime().After(initialInfo.ModTime()) {
				modified = true
			}
		}
		if modified {
			break
		}
	}

	// Wait for editor to finish
	if werr := cmd.Wait(); werr != nil {
		return "", fmt.Errorf("editor failed: %w", werr)
	}

	// Read back the content
	data, rerr := os.ReadFile(filename)
	if rerr != nil {
		return "", fmt.Errorf("failed to read edited file: %w", rerr)
	}

	return string(data), nil
}

// detectEditor finds the best available editor
func detectEditor() (*Editor, error) {
	// Check $EDITOR environment variable first
	if editor := os.Getenv("EDITOR"); editor != "" {
		return parseEditorCommand(editor)
	}

	// Check $VISUAL environment variable
	if editor := os.Getenv("VISUAL"); editor != "" {
		return parseEditorCommand(editor)
	}

	// Fallback chain based on platform
	fallbacks := getFallbackEditors()

	for _, fallback := range fallbacks {
		if editor, err := parseEditorCommand(fallback); err == nil {
			return editor, nil
		}
	}

	return nil, fmt.Errorf("no suitable editor found")
}

// parseEditorCommand parses an editor command string
func parseEditorCommand(cmd string) (*Editor, error) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty editor command")
	}

	// Check if command exists
	if _, err := exec.LookPath(parts[0]); err != nil {
		return nil, fmt.Errorf("editor not found: %s", parts[0])
	}

	editor := &Editor{
		command: parts[0],
		args:    parts[1:],
		isGUI:   isGUIEditor(parts[0]),
	}

	return editor, nil
}

// isGUIEditor checks if an editor is GUI-based
func isGUIEditor(command string) bool {
	guiEditors := map[string]bool{
		"code":      true, // VSCode
		"subl":      true, // Sublime Text
		"atom":      true, // Atom
		"gedit":     true, // GNOME Editor
		"kate":      true, // KDE Editor
		"notepad++": true, // Notepad++
		"notepad":   true, // Windows Notepad
		"textedit":  true, // macOS TextEdit
	}

	return guiEditors[command]
}

// getFallbackEditors returns platform-specific fallback editors
func getFallbackEditors() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{
			"notepad",
			"notepad++",
			"code",
			"vim",
			"nano",
		}
	case "darwin":
		return []string{
			"vim",
			"nvim",
			"nano",
			"micro",
			"emacs",
			"code",
			"textedit",
		}
	default: // Linux and others
		return []string{
			"vim",
			"nvim",
			"nano",
			"micro",
			"emacs",
			"gedit",
			"kate",
			"code",
		}
	}
}
