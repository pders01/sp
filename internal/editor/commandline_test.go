package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestParseEditorCommandFindsQuotedExecutableWithSpaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "editor dir with spaces")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "editor's name"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	// LookPath needs an executable file; this fixture is never run.
	if err := os.WriteFile(path, []byte("synthetic executable fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	want := []string{"-u", "config dir/init.vim", "-c", "set number", ""}
	ed, err := parseEditorCommand(editorTestCommand(append([]string{path}, want...)...))
	if err != nil || ed.command != path || !slices.Equal(ed.args, want) {
		t.Fatalf("quoted executable/arguments were not preserved: %+v, %v", ed, err)
	}
}

func TestQuotedEditorArgumentsReachProcessWithoutShellExpansion(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SP_QUOTED_EDITOR_HELPER", "1")
	want := []string{"argument with spaces", "", "apostrophe's value", `embedded"quote`, `trailing\`, "$HOME", "$(touch marker)", "`touch marker`", "*.md", "|", ";", "&&", "笔记 🙂"}
	parts := append([]string{exe, "-test.run=^TestQuotedEditorHelperProcess$", "--"}, want...)
	t.Setenv("EDITOR", editorTestCommand(parts...))
	ed, err := NewEditor()
	if err != nil {
		t.Fatal(err)
	}
	cmd, path, cleanup, err := ed.Prepare("synthetic note")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	result, err := ed.RunPrepared(cmd, path)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal([]byte(result), &got); err != nil || !slices.Equal(got, want) {
		t.Fatalf("process received %q, %v; want %q", got, err, want)
	}
}

func TestParseEditorCommandRejectsEmptyExecutableAndNUL(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{editorTestCommand(""), editorTestCommand(exe, "bad\x00arg")} {
		if _, err := parseEditorCommand(command); err == nil {
			t.Fatal("accepted an empty executable or invalid process argument")
		}
	}
}

func TestQuotedEditorHelperProcess(_ *testing.T) {
	if os.Getenv("SP_QUOTED_EDITOR_HELPER") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 || separator >= len(os.Args)-1 {
		os.Exit(2)
	}
	args := os.Args[separator+1:]
	result, err := json.Marshal(args[:len(args)-1])
	if err != nil || os.WriteFile(args[len(args)-1], result, 0o600) != nil {
		os.Exit(3)
	}
	os.Exit(0)
}
