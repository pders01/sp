package sharedmcp

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func buildExecutable(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "sp-shared-mcp")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command(goBinary, "build", "-o", exe, "../../cmd/sp-shared-mcp")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("CLI build: %v\n%s", err, out)
	}
	return exe
}
