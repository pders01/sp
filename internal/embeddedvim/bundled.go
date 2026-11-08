package embeddedvim

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"

	"github.com/pders01/sp/internal/editorcore"
)

// Public immutable program artifacts only; no documents, keys or configuration.
//
//go:embed assets/qjs.wasm assets/guest.js assets/manifest.json assets/licenses
var bundled embed.FS

//go:generate node ../../scripts/embedded-vim/build-bundle.js assets

// BundledAssets reads only linked program bytes. Installed SP never searches a
// CWD, sibling file, environment variable, Node installation or alternate asset.
func BundledAssets() (Assets, error) {
	var out Assets
	manifest, err := bundled.ReadFile("assets/manifest.json")
	if err != nil {
		return out, ErrAssets
	}
	var m struct {
		FormatVersion int                           `json:"formatVersion"`
		Bundle        struct{ File, SHA256 string } `json:"bundle"`
		Core          struct{ SHA256 string }       `json:"core"`
	}
	if json.Unmarshal(manifest, &m) != nil || m.FormatVersion != 3 || m.Bundle.File != "guest.js" || m.Core.SHA256 != "a8c2887193e966a6d1c4acf01105a0b6e8ed11e8ff26dfe3bb8a898649efe650" {
		return out, ErrAssets
	}
	out.Wasm, err = bundled.ReadFile("assets/qjs.wasm")
	if err != nil || len(out.Wasm) > 2<<20 {
		return Assets{}, ErrAssets
	}
	out.Guest, err = bundled.ReadFile("assets/guest.js")
	if err != nil || len(out.Guest) == 0 || len(out.Guest) > 2<<20 {
		return Assets{}, ErrAssets
	}
	w, g := sha256.Sum256(out.Wasm), sha256.Sum256(out.Guest)
	if hex.EncodeToString(w[:]) != RuntimeSHA256 || hex.EncodeToString(g[:]) != m.Bundle.SHA256 {
		return Assets{}, ErrAssets
	}
	return out, nil
}

// OpenBundledUI is the opt-in in-memory application adapter. It supplies NO OS
// enforcement and is not Driver.OpenProtected. The caller owns the returned UI
// on partial failure; persistence still belongs to privateedit, never the guest.
func OpenBundledUI(ctx context.Context, options editorcore.Options, text string) (editorcore.UI, error) {
	// Reject before creating an editable owner/guest. Canonical bytes stay in
	// the manager; no silent truncation, fallback or recovery publication.
	if !validText(text) {
		return nil, ErrInput
	}
	a, err := BundledAssets()
	if err != nil {
		return nil, err
	}
	u, err := OpenUI(ctx, a.Wasm, a.Guest, options, text)
	if u != nil {
		u.mu.Lock()
		u.viewport.application = true
		if renderErr := u.renderLocked(); renderErr != nil && err == nil {
			u.fault = errUIStartup
			err = errUIStartup
		}
		u.mu.Unlock()
	}
	return u, err
}
