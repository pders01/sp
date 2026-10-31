package editor

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestGUICompletionOptions(t *testing.T) {
	for _, tc := range []struct {
		command string
		args    []string
		want    []string
	}{
		{"/Applications/VS Code/bin/code", []string{"--new-window"}, []string{"--new-window", "--wait"}},
		{`C:\Program Files\VS Code\code.cmd`, []string{"--profile", "synthetic profile", "-w"}, []string{"--profile", "synthetic profile", "-w"}},
		{"/opt/code-insiders", nil, []string{"--wait"}},
		{`C:\Program Files\VS Code Insiders\code-insiders.cmd`, nil, []string{"--wait"}},
		{"codium.cmd", nil, []string{"--wait"}},
		{"VSCodium.cmd", nil, []string{"--wait"}},
		{"cursor", nil, []string{"--wait"}},
		{"windsurf", nil, []string{"--wait"}},
		{"subl", []string{"--new-window", "-w"}, []string{"--new-window", "-w"}},
		{"subl.exe", nil, []string{"--wait"}},
		{`C:\Program Files\Sublime Text\subl.exe`, []string{"-w"}, []string{"-w"}},
		{"atom", nil, []string{"--wait"}},
		{"gedit", []string{"--standalone"}, []string{"--standalone", "--wait"}},
		{"gedit", []string{"--wait"}, []string{"--wait"}},
		{"/usr/bin/kate", []string{"--new"}, []string{"--new", "--block"}},
		{"kate.exe", []string{"-b"}, []string{"-b"}},
		{"kate", []string{"--block"}, []string{"--block"}},
		{"gvim", nil, []string{"--nofork"}},
		{"gvim.exe", []string{"-f"}, []string{"-f"}},
		{"mvim", []string{"--nofork"}, []string{"--nofork"}},
		{"gvim", []string{"--remote-wait"}, []string{"--remote-wait", "--nofork"}},
		{`C:\Program Files\Notepad++\notepad++.exe`, []string{"-nosession"}, []string{"-nosession", "-multiInst"}},
		{"notepad++", []string{"-multiInst", "-ro"}, []string{"-multiInst", "-ro"}},
		{"code", []string{"--", "--wait"}, []string{"--wait", "--", "--wait"}},
		{"kate", []string{"--", "-b"}, []string{"--block", "--", "-b"}},
		{"/path named code/vim", []string{"--clean"}, []string{"--clean"}},
		{"/custom/editor-wrapper", []string{"--literal", "", "$HOME"}, []string{"--literal", "", "$HOME"}},
	} {
		t.Run(tc.command+fmt.Sprint(tc.args), func(t *testing.T) {
			original := slices.Clone(tc.args)
			// Extra capacity catches accidental mutation when appending options.
			supplied := make([]string, len(tc.args), len(tc.args)+3)
			copy(supplied, tc.args)
			got, err := editorCompletionArgs(tc.command, supplied)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("completion args = %q, %v; want %q", got, err, tc.want)
			}
			if !slices.Equal(supplied, original) {
				t.Fatalf("supplied options mutated: %q", supplied)
			}
			if len(got) > 0 {
				got[0] = "changed"
				if !slices.Equal(supplied, original) {
					t.Fatal("completion arguments alias stored editor options")
				}
			}
		})
	}
}

func TestGUIClassificationUsesBasename(t *testing.T) {
	for _, command := range []string{"code", "/opt/editor dir/code", `C:\Program Files\Code.EXE`, "SUBL.EXE", "notepad++.exe", "/Applications/TextEdit", `C:\Windows\notepad.exe`, "/usr/bin/open", "gnome-text-editor"} {
		if !isGUIEditor(command) {
			t.Errorf("GUI launcher not recognized: %q", command)
		}
	}
	for _, command := range []string{"vim", "/code/vim", `C:\code\nvim.exe`, "my-code-wrapper", "code-helper.exe"} {
		if isGUIEditor(command) {
			t.Errorf("misclassified launcher: %q", command)
		}
	}
}

func TestUnsafeLaunchersFailBeforeBufferCreation(t *testing.T) {
	for _, name := range []string{"notepad", "TextEdit", "open", "gnome-text-editor", "Code", "Code - Insiders", "VSCodium", "Cursor", "sublime_text", "Electron"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			useSyntheticTempDir(t, dir)
			command := filepath.Join(dir, name+".exe")
			// LookPath fixture only, never executed.
			if err := os.WriteFile(command, []byte("synthetic launcher"), 0o700); err != nil {
				t.Fatal(err)
			}
			_, err := parseEditorCommand(editorTestCommand(command, "--wait"))
			assertConfigurationAdvice(t, err)
			ed := &Editor{command: command, args: []string{"--wait"}}
			cmd, path, cleanup, err := ed.Prepare("synthetic content")
			assertConfigurationAdvice(t, err)
			if cmd != nil || path != "" || cleanup != nil {
				t.Fatal("unsupported launcher returned a prepared buffer")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("created a buffer before rejecting the launcher: %v, %v", entries, err)
			}
		})
	}
	for _, option := range []string{"--remote", "--remote-silent", "--remote-tab", "--remote-send", "--remote-expr"} {
		_, err := editorCompletionArgs("gvim.exe", []string{"-f", option})
		assertConfigurationAdvice(t, err)
	}
}

func assertConfigurationAdvice(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "configure EDITOR (or VISUAL)") {
		t.Fatalf("expected actionable configuration advice, got %v", err)
	}
}

func useSyntheticTempDir(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, dir)
	}
}

// TestMain intercepts only this synthetic fixture before Go's flag parser, so
// the copied executable can receive real GUI launcher flags without a shell.
func TestMain(m *testing.M) {
	if os.Getenv("SP_COMPLETION_HELPER") == "1" {
		os.Exit(runCompletionHelper())
	}
	os.Exit(m.Run())
}

func runCompletionHelper() int {
	args := os.Args[1:]
	end := slices.Index(args, "--")
	if end < 0 || !slices.Contains(args[:end], "--wait") {
		return 9 // Simulate a launcher that cannot block without its wait option.
	}
	data, err := json.Marshal(args)
	if err != nil || os.WriteFile(os.Getenv("SP_COMPLETION_ARGV"), data, 0o600) != nil {
		return 10
	}
	buffer := args[len(args)-1]
	fmt.Println("ready")
	input := bufio.NewScanner(os.Stdin)
	saves := 0
	for input.Scan() {
		switch input.Text() {
		case "save":
			saves++
			content := "first save"
			if saves > 1 {
				content = "last save\r\n\t笔记 🙂\n"
			}
			if os.WriteFile(buffer, []byte(content), 0o600) != nil {
				return 11
			}
			fmt.Println("saved")
		case "exit":
			if os.Getenv("SP_COMPLETION_MODE") == "failure" {
				if os.WriteFile(buffer, []byte("retained failed draft"), 0o600) != nil {
					return 12
				}
				return 7
			}
			return 0
		}
	}
	return 13
}

func TestGUIProcessFinishedContract(t *testing.T) {
	// Copy only the synthetic test executable, never an installed/user editor.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "editor dir with spaces")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "code"
	if runtime.GOOS == "windows" {
		name = "subl.exe" // Native blocking CLI, not a direct Electron GUI binary.
	}
	launcher := filepath.Join(dir, name)
	if err := os.WriteFile(launcher, binary, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, flow := range []string{"RunPrepared", "ExecProcess contract"} {
		for _, mode := range []string{"unchanged", "multiple saves", "failure"} {
			t.Run(flow+"/"+mode, func(t *testing.T) {
				bufferDir := filepath.Join(t.TempDir(), "buffer dir 笔记")
				if err := os.Mkdir(bufferDir, 0o700); err != nil {
					t.Fatal(err)
				}
				useSyntheticTempDir(t, bufferDir)
				supplied := []string{"--new-window", "--profile", "synthetic profile", "", "$HOME", "$(touch marker)", "--", "literal ; * 笔记"}
				ed, err := parseEditorCommand(editorTestCommand(append([]string{launcher}, supplied...)...))
				if err != nil || !ed.IsGUI() {
					t.Fatalf("parse GUI fixture: %v", err)
				}
				cmd, path, cleanup, err := ed.Prepare("original synthetic note")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(cleanup)
				argvPath := filepath.Join(bufferDir, "argv.json")
				cmd.Env = append(os.Environ(), "SP_COMPLETION_HELPER=1", "SP_COMPLETION_MODE="+mode, "SP_COMPLETION_ARGV="+argvPath)
				inR, inW, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer inR.Close()
				defer inW.Close()
				outR, outW, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer outR.Close()
				defer outW.Close()
				cmd.Stdin, cmd.Stdout = inR, outW
				events := make(chan string, 3)
				go func() {
					scanner := bufio.NewScanner(outR)
					for scanner.Scan() {
						events <- scanner.Text()
					}
					close(events)
				}()
				type result struct {
					content string
					err     error
				}
				done := make(chan result, 1)
				go func() {
					var r result
					if flow == "RunPrepared" {
						r.content, r.err = ed.RunPrepared(cmd, path)
					} else {
						// tea.ExecProcess runs this exact prepared command and only
						// its successful callback may read/persist the buffer.
						r.err = cmd.Run()
						if r.err == nil {
							r.content, r.err = ReadEdited(path)
						}
					}
					done <- r
				}()
				awaitEvent := func(want string) {
					t.Helper()
					select {
					case got := <-events:
						if got != want {
							t.Fatalf("helper event = %q, want %q", got, want)
						}
					case r := <-done:
						t.Fatalf("returned before editor exit: %+v", r)
					case <-time.After(10 * time.Second):
						t.Fatal("synthetic editor did not reach event")
					}
				}
				assertPending := func(want string) {
					t.Helper()
					select {
					case r := <-done:
						t.Fatalf("returned before editor exit: %+v", r)
					default:
					}
					got, err := ReadEdited(path)
					if err != nil || got != want {
						t.Fatalf("live buffer = %q, %v; want %q", got, err, want)
					}
				}
				awaitEvent("ready")
				assertPending("original synthetic note")
				want := "original synthetic note"
				if mode == "multiple saves" {
					for _, content := range []string{"first save", "last save\r\n\t笔记 🙂\n"} {
						if _, err := fmt.Fprintln(inW, "save"); err != nil {
							t.Fatal(err)
						}
						awaitEvent("saved")
						assertPending(content)
						want = content
					}
				}
				if _, err := fmt.Fprintln(inW, "exit"); err != nil {
					t.Fatal(err)
				}
				var r result
				select {
				case r = <-done:
				case <-time.After(10 * time.Second):
					t.Fatal("editor closed, but completion still waits for a modification")
				}
				if mode == "failure" {
					var exitErr *exec.ExitError
					if !errors.As(r.err, &exitErr) || exitErr.ExitCode() != 7 || r.content != "" {
						t.Fatalf("nonzero exit returned content or lost exit status: %+v", r)
					}
					want = "retained failed draft"
				} else if r.err != nil || r.content != want {
					t.Fatalf("completed edit = %+v; want %q", r, want)
				}
				got, err := ReadEdited(path)
				if err != nil || got != want {
					t.Fatalf("buffer not retained: %q, %v", got, err)
				}
				data, err := os.ReadFile(argvPath)
				if err != nil {
					t.Fatal(err)
				}
				var argv []string
				if err := json.Unmarshal(data, &argv); err != nil {
					t.Fatal(err)
				}
				wantArgv := append(slices.Insert(slices.Clone(supplied), 6, "--wait"), path)
				if !slices.Equal(argv, wantArgv) || !slices.Equal(ed.args, supplied) {
					t.Fatalf("arguments not preserved: process %q; stored %q; want %q", argv, ed.args, wantArgv)
				}
				cleanup()
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("explicit cleanup did not remove buffer: %v", err)
				}
			})
		}
	}
}
