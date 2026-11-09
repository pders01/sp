package embeddedvim

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Exercise Git's Windows-style checkout conversion with public program files
// only. No guest, terminal, document, user Git configuration or network is used.
func TestPinnedAssetsSurviveCRLFCheckout(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("Git is required for the public checkout regression")
	}
	root := filepath.Join("..", "..")
	repo := t.TempDir()
	home := t.TempDir()
	globalConfig := filepath.Join(home, "gitconfig")
	if os.WriteFile(globalConfig, nil, 0o600) != nil {
		t.Fatal("owned Git configuration setup failed")
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		cmd.Env = []string{
			"HOME=" + home, "USERPROFILE=" + home,
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + globalConfig,
			"GIT_TERMINAL_PROMPT=0",
		}
		if cmd.Run() != nil {
			t.Fatal("owned public Git checkout failed")
		}
	}
	files := []string{
		"internal/embeddedvim/assets/qjs.wasm",
		"internal/embeddedvim/assets/guest.js",
		"internal/embeddedvim/assets/manifest.json",
	}
	for _, name := range []string{"adapter.js", "guest-entry.js", "bundle.js", "core-policy.js", "package.json", "package-lock.json"} {
		files = append(files, "scripts/embedded-vim/"+name)
	}
	licenses, err := filepath.Glob(filepath.Join(root, "internal", "embeddedvim", "assets", "licenses", "*.txt"))
	if err != nil || len(licenses) != 15 {
		t.Fatal("public attribution inputs missing")
	}
	for _, path := range licenses {
		files = append(files, "internal/embeddedvim/assets/licenses/"+filepath.Base(path))
	}
	expected := make(map[string][]byte, len(files))
	for _, path := range append([]string{".gitattributes"}, files...) {
		data, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		destination := filepath.Join(repo, filepath.FromSlash(path))
		if readErr != nil || os.MkdirAll(filepath.Dir(destination), 0o700) != nil || os.WriteFile(destination, data, 0o600) != nil {
			t.Fatal("owned public checkout input setup failed")
		}
		expected[path] = data
	}
	run("init", "--quiet")
	// Add the original bytes, then exercise the same CRLF policy as Windows.
	run("-c", "core.autocrlf=false", "add", "--all")
	checkout := t.TempDir()
	run("-c", "core.autocrlf=true", "checkout-index", "--all", "--prefix="+filepath.ToSlash(checkout)+"/")
	for _, path := range files {
		data, readErr := os.ReadFile(filepath.Join(checkout, filepath.FromSlash(path)))
		if readErr != nil || !bytes.Equal(data, expected[path]) {
			t.Fatalf("checkout changed public program/attribution bytes: %s", path)
		}
	}
}
