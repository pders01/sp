//go:build windows

package editor

import (
	"reflect"
	"testing"

	"golang.org/x/sys/windows"
)

func editorTestCommand(parts ...string) string {
	return windows.ComposeCommandLine(parts)
}

func TestSplitEditorCommandWindowsQuoting(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    []string
	}{
		{`nvim --clean`, []string{"nvim", "--clean"}},
		{`  "C:\Program Files\Neovim\bin\nvim.exe" -u "C:\config dir\init.vim"`, []string{`C:\Program Files\Neovim\bin\nvim.exe`, "-u", `C:\config dir\init.vim`}},
		{`"\\server\share\editor.exe" ""`, []string{`\\server\share\editor.exe`, ""}},
		{`nvim "$HOME" "$(touch marker)" "*.md" "|" ";"`, []string{"nvim", "$HOME", "$(touch marker)", "*.md", "|", ";"}},
		{" \t ", nil},
	} {
		got, err := splitEditorCommand(tc.command)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("split(%q) = %q, %v; want %q", tc.command, got, err, tc.want)
		}
	}
	if _, err := splitEditorCommand("nvim \x00"); err == nil {
		t.Fatal("accepted NUL in a Windows command line")
	}
}

func TestWindowsEditorArgumentsRoundTripNativeQuoting(t *testing.T) {
	want := []string{`C:\Program Files\nvim.exe`, "", `ends-with-slash\`, `embedded"quote`, "笔记 🙂"}
	got, err := splitEditorCommand(windows.ComposeCommandLine(want))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("native argument round trip = %q, %v; want %q", got, err, want)
	}
}
