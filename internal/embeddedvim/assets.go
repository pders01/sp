package embeddedvim

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

var ErrAssets = errors.New("embedded Vim: rebuild the pinned public assets")

// Assets contains only generated public program code, never documents or keys.
type Assets struct{ Wasm, Guest []byte }

func readPublicAsset(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, ErrAssets
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrAssets
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, ErrAssets
	}
	return data, nil
}

// LoadAssets reads only fixed public build inputs/outputs. Manifest values never
// select paths. A stale source/lock/bundle pair is rejected before opening a UI.
func LoadAssets(dir string) (Assets, error) {
	var out Assets
	manifestBytes, err := readPublicAsset(filepath.Join(dir, "manifest.json"), 64<<10)
	if err != nil {
		return out, err
	}
	var manifest struct {
		FormatVersion int                           `json:"formatVersion"`
		Bundle        struct{ File, SHA256 string } `json:"bundle"`
		Sources       map[string]string             `json:"sources"`
	}
	if json.Unmarshal(manifestBytes, &manifest) != nil || manifest.FormatVersion != 3 || manifest.Bundle.File != "guest.js" {
		return out, ErrAssets
	}
	out.Wasm, err = readPublicAsset(filepath.Join(dir, "qjs.wasm"), 2<<20)
	if err != nil {
		return Assets{}, err
	}
	out.Guest, err = readPublicAsset(filepath.Join(dir, "guest.js"), 2<<20)
	if err != nil {
		return Assets{}, err
	}
	wasmSum, guestSum := sha256.Sum256(out.Wasm), sha256.Sum256(out.Guest)
	if hex.EncodeToString(wasmSum[:]) != RuntimeSHA256 || hex.EncodeToString(guestSum[:]) != manifest.Bundle.SHA256 {
		return Assets{}, ErrAssets
	}
	for _, name := range []string{"adapter.js", "guest-entry.js", "bundle.js", "core-policy.js", "package.json", "package-lock.json"} {
		data, err := readPublicAsset(filepath.Join(dir, "..", "..", name), 2<<20)
		if err != nil {
			return Assets{}, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != manifest.Sources[name] {
			return Assets{}, ErrAssets
		}
	}
	return out, nil
}
