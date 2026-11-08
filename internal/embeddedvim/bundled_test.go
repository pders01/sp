package embeddedvim

import (
	"context"
	"github.com/pders01/sp/internal/editorcore"
	"strings"
	"testing"
)

func TestBundledUIIgnoresCWDAndHasNoDemoChrome(t *testing.T) {
	// Owned empty CWD proves installed code needs no source tree, Node or files.
	t.Chdir(t.TempDir())
	a, err := BundledAssets()
	if err != nil {
		t.Fatal("bundled program validation failed")
	}
	a.Guest[0] ^= 1
	b, err := BundledAssets()
	if err != nil || a.Guest[0] == b.Guest[0] {
		t.Fatal("bundled bytes aliased a caller mutation")
	}
	u, err := OpenBundledUI(context.Background(), editorcore.Options{DocumentID: "2000-01-01", Width: 40, Height: 8}, "synthetic only")
	if err != nil || u == nil {
		t.Fatal("bundled application UI failed without source tree")
	}
	defer u.Discard(context.Background(), true)
	f := u.Frame()
	if f.StatusRow == nil || strings.Contains(f.Lines[*f.StatusRow], "[demo]") || !strings.HasPrefix(f.Lines[*f.StatusRow], "NORMAL") {
		t.Fatal("application inherited demo chrome")
	}
	if _, err = u.Type(context.Background(), "AX"); err != nil {
		t.Fatal("bundled application input failed")
	}
	if _, err = u.Key(context.Background(), "Escape"); err != nil {
		t.Fatal("bundled application escape failed")
	}
	if u.Retained().Observed.Text != "synthetic onlyX" {
		t.Fatal("bundled application changed bytes")
	}
}
