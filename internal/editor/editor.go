package editor

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
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

// Prepare builds the *exec.Cmd that opens content in the user's editor
// alongside the temp-file path that holds the buffered content and a
// cleanup func to remove that file. Successful process exit is the edit-finished
// contract: known GUI launchers get blocking options; unknown executables and
// wrappers must themselves remain running until editing finishes. Callers use
// RunPrepared, or hand the command to tea.ExecProcess and call ReadEdited only
// after successful exit. Invoke cleanup only after persistence succeeds or the
// edit is unchanged, retaining the buffer on error.
func (e *Editor) Prepare(content string) (cmd *exec.Cmd, path string, cleanup func(), err error) {
	args, err := editorCompletionArgs(e.command, e.args)
	if err != nil {
		return nil, "", nil, err
	}
	tmpFile, err := os.CreateTemp("", "sp-*.md")
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	cleanup = func() { _ = os.Remove(tmpFile.Name()) }

	if _, werr := tmpFile.WriteString(content); werr != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("failed to write to temp file: %w", werr)
	}
	if cerr := tmpFile.Close(); cerr != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("failed to close temp file: %w", cerr)
	}

	args = append(args, tmpFile.Name())
	cmd = exec.Command(e.command, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd, tmpFile.Name(), cleanup, nil
}

// IsGUI reports whether the resolved editor is a known GUI launcher.
// It is informational only: Prepare supplies the same process-exit completion
// contract for GUI and terminal editors, including when used with tea.ExecProcess.
func (e *Editor) IsGUI() bool { return e.isGUI }

// ReadEdited reads the content the editor wrote to path.
func ReadEdited(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("failed to inspect edited file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("edited buffer must be a regular file, not a symlink")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read edited file: %w", err)
	}
	return string(data), nil
}

// RunPrepared runs a command returned by Prepare and reads its edited buffer.
// The caller owns cleanup so the buffer can survive a subsequent save failure.
// Like tea.ExecProcess, this waits for process exit, never for a first save.
func (e *Editor) RunPrepared(cmd *exec.Cmd, path string) (string, error) {
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor failed: %w", err)
	}
	return ReadEdited(path)
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

	return nil, fmt.Errorf("no suitable blocking editor found; configure EDITOR (or VISUAL) to an editor such as 'code --wait' or 'vim'")
}

// parseEditorCommand parses arguments without shell execution or expansion.
func parseEditorCommand(cmd string) (*Editor, error) {
	if strings.TrimSpace(cmd) == "" {
		return nil, fmt.Errorf("empty editor command")
	}
	parts, err := splitEditorCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("invalid editor command: %w", err)
	}
	if len(parts) == 0 || parts[0] == "" {
		return nil, fmt.Errorf("empty editor command")
	}

	// Check if command exists
	resolved, err := exec.LookPath(parts[0])
	if err != nil {
		return nil, fmt.Errorf("editor not found: %s", parts[0])
	}

	// Reject known nonblocking launchers before any editor buffer is created.
	// Prepare repeats this check and adds flags for both CLI and TUI callers.
	if _, err := editorCompletionArgs(resolved, parts[1:]); err != nil {
		return nil, err
	}

	editor := &Editor{
		command: resolved,
		args:    parts[1:],
		isGUI:   isGUIEditor(resolved),
	}

	return editor, nil
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
