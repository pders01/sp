package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCLIErrorsAreReportedOnceWithoutUsageDumps(t *testing.T) {
	for _, tc := range []struct {
		mode, want string
	}{
		{"flag", "unknown flag"},
		{"command", "unknown command"},
		{"required", "required flag"},
		{"views", "choose one view"},
		{"runtime", "save failed; buffer path: synthetic-recovery.md"},
		{"cancel", "privacy enable cancelled"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			stdout, stderr, code := runCLIErrorProcess(t, tc.mode)
			if code != 1 || stdout != "" || strings.Count(stderr, "Error:") != 1 || !strings.Contains(stderr, tc.want) {
				t.Fatalf("failure outcome: code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if strings.Contains(stderr, "Usage:") || strings.Contains(stderr, "Examples:") || strings.Contains(stderr, "Flags:") {
				t.Fatalf("failure dumped unrelated help: %q", stderr)
			}
		})
	}
}

func TestCLIExplicitHelpStillPrintsUsageWithoutAnError(t *testing.T) {
	stdout, stderr, code := runCLIErrorProcess(t, "help")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Usage:") || strings.Contains(stdout, "Error:") {
		t.Fatalf("help outcome: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func runCLIErrorProcess(t *testing.T, mode string) (string, string, int) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIErrorHelperProcess$")
	cmd.Env = append(os.Environ(), "SP_CLI_ERROR_CASE="+mode, "HOME="+home, "USERPROFILE="+home)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		code = exit.ExitCode()
	}
	if _, err := os.Stat(home + string(os.PathSeparator) + ".sp"); !os.IsNotExist(err) {
		t.Fatal("argument/help/synthetic-error execution initialized a note store")
	}
	return stdout.String(), stderr.String(), code
}

// Re-execute the test binary so Execute's real reporting and exit code are used.
func TestCLIErrorHelperProcess(_ *testing.T) {
	mode := os.Getenv("SP_CLI_ERROR_CASE")
	if mode == "" {
		return
	}
	rootCmd.SetArgs([]string{})
	rootCmd.RunE = func(*cobra.Command, []string) error {
		switch mode {
		case "runtime":
			return errors.New("save failed; buffer path: synthetic-recovery.md")
		case "cancel":
			return errors.New("privacy enable cancelled: confirmation did not match")
		default:
			return errors.New("unexpected command execution")
		}
	}
	switch mode {
	case "flag":
		rootCmd.SetArgs([]string{"--does-not-exist"})
	case "command":
		rootCmd.SetArgs([]string{"does-not-exist"})
	case "required":
		rootCmd.SetArgs([]string{"append", "2026-01-01", "--", "- item"})
	case "views":
		rootCmd.SetArgs([]string{"-c", "-n"})
	case "help":
		rootCmd.SetArgs([]string{"--help"})
	}
	Execute()
	os.Exit(0)
}
