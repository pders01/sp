package embeddedvim

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicAssetManifestRejectsOldSelectionABI(t *testing.T) {
	// Public code only, copied into an explicitly owned fixture. A positive
	// control ensures failure cannot be explained by missing files/hash inputs.
	base := t.TempDir()
	dir := filepath.Join(base, "go-proof", "assets")
	if os.MkdirAll(dir, 0o700) != nil {
		t.Fatal("owned public asset fixture setup failed")
	}
	for _, name := range []string{"qjs.wasm", "guest.js", "manifest.json"} {
		data, err := os.ReadFile(filepath.Join("assets", name))
		if err != nil || os.WriteFile(filepath.Join(dir, name), data, 0o600) != nil {
			t.Fatal("owned public asset fixture copy failed")
		}
	}
	for _, name := range []string{"adapter.js", "guest-entry.js", "bundle.js", "core-policy.js", "package.json", "package-lock.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "embedded-vim", name))
		if err != nil || os.WriteFile(filepath.Join(base, name), data, 0o600) != nil {
			t.Fatal("owned public source fixture copy failed")
		}
	}
	if assets, err := LoadAssets(dir); err != nil || len(assets.Wasm) == 0 || len(assets.Guest) == 0 {
		t.Fatal("valid public asset positive control failed")
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal("owned public manifest read failed")
	}
	var manifest map[string]any
	if json.Unmarshal(data, &manifest) != nil {
		t.Fatal("owned public manifest decode failed")
	}
	for _, version := range []int{1, 2, 4} {
		manifest["formatVersion"] = version
		data, err := json.Marshal(manifest)
		if err != nil || os.WriteFile(manifestPath, data, 0o600) != nil {
			t.Fatal("owned public manifest update failed")
		}
		assets, err := LoadAssets(dir)
		if !errors.Is(err, ErrAssets) || len(assets.Wasm) != 0 || len(assets.Guest) != 0 {
			t.Fatal("incompatible public snapshot ABI accepted")
		}
	}
}
