//go:build windows

package editor

import (
	"strings"

	"golang.org/x/sys/windows"
)

// Use native argument rules so quoted paths, UNC paths, and backslashes are not
// reinterpreted as POSIX escapes. This parses arguments; it never runs a shell.
func splitEditorCommand(command string) ([]string, error) {
	command = strings.TrimLeft(command, " \t")
	if command == "" {
		return nil, nil
	}
	return windows.DecomposeCommandLine(command)
}
