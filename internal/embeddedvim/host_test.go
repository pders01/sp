package embeddedvim

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func hostAssets(t *testing.T) ([]byte, []byte) {
	t.Helper()
	wasm, err := os.ReadFile("assets/qjs.wasm")
	if err != nil {
		t.Fatal("prepare the pinned runtime asset first")
	}
	guest, err := os.ReadFile("assets/guest.js")
	if err != nil {
		t.Fatal("build the shared guest bundle first")
	}
	return wasm, guest
}

func TestGoGuestSmoke(t *testing.T) {
	wasm, guest := hostAssets(t)
	s, err := New(context.Background(), wasm, guest, "cat cat", Options{})
	if err != nil {
		t.Fatal(err) // Session returns fixed diagnostics only, never raw guest errors.
	}
	defer s.Close()
	_, err = s.Keys(context.Background(), ":", "%", "s", "/", "c", "a", "t", "/", "d", "o", "g", "/", "g", "<CR>")
	if err != nil {
		t.Fatal("typed substitution failed")
	}
	if s.Snapshot().Text != "dog dog" {
		t.Fatal("substitution text did not match")
	}
}

func TestBuiltinModulesHaveNoHostResources(t *testing.T) {
	// A deliberately owned CWD fixture is readable/writable by the Go parent.
	// Were the wrapper's CWD mount accidentally restored, its relative guest
	// path would expose this exact fixture. Never inspect a normal store/path.
	dir, err := os.MkdirTemp(".", ".host-control-")
	if err != nil {
		t.Fatal("synthetic parent fixture creation failed")
	}
	t.Cleanup(func() {
		if os.RemoveAll(dir) != nil {
			t.Error("synthetic parent fixture cleanup failed")
		}
	})
	path := filepath.Join(dir, "control.txt")
	if os.WriteFile(path, []byte("parent-positive-control"), 0o600) != nil {
		t.Fatal("parent positive control write failed")
	}
	t.Setenv("SP_EMBEDDED_VIM_SYNTHETIC_CANARY", "synthetic-only")
	s := openProof(t, "synthetic fixture", Options{})
	encoded, _ := json.Marshal(filepath.ToSlash(path))
	expression := `(()=>{
	  globalThis.resourceObservation = null;
	  import('qjs:std').then(std => {
	    const r = std.open(` + string(encoded) + `, 'r');
	    const deniedRead = r === null;
	    if (r) r.close();
	    const w = std.open(` + string(encoded) + `, 'w');
	    const deniedWrite = w === null;
	    if (w) w.close();
	    globalThis.resourceObservation = deniedRead && deniedWrite &&
	      (std.getenv('SP_EMBEDDED_VIM_SYNTHETIC_CANARY') == null);
	  });
	  return true;
	})()`
	if _, err := s.EvaluateForTest(context.Background(), expression); err != nil {
		t.Fatal("builtin module resource observation failed")
	}
	out, err := s.EvaluateForTest(context.Background(), "globalThis.resourceObservation")
	if err != nil || out != "true" {
		t.Fatal("builtin module reached a host fixture or inherited environment")
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "parent-positive-control" {
		t.Fatal("parent fixture was not preserved")
	}
}

func TestLinearMemoryCeiling(t *testing.T) {
	s := openProof(t, "synthetic fixture", Options{})
	if s.module.Memory().Size() == 0 || s.module.Memory().Size() > 128<<20 {
		t.Fatal("invalid Wasm memory size")
	}
	// Current pages plus 2048 exceed the configured ceiling, so this must fail
	// without allocating or destroying the still-useful guest.
	if _, ok := s.module.Memory().Grow(2048); ok {
		t.Fatal("Wasm memory grew past its configured ceiling")
	}
	if out, err := s.EvaluateForTest(context.Background(), "true"); err != nil || out != "true" {
		t.Fatal("guest unusable after refused memory growth")
	}
}

func TestCallerDeadlineTerminatesPartiallyMutatedGuest(t *testing.T) {
	s := openProof(t, "synthetic fixture", Options{OperationTime: 2 * time.Second})
	owned := s.module
	before := s.Snapshot()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := s.EvaluateForTest(ctx, `(()=>{proof.keys(['i','Y']); for(;;){} })()`)
	if !errors.Is(err, ErrGuest) || !owned.IsClosed() || !s.Faulted() || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("caller deadline did not terminate ownership and retain the prior acknowledgement")
	}
}

func TestEmptyKeyBatchIsNoOp(t *testing.T) {
	s := openProof(t, "synthetic fixture", Options{})
	before := s.Snapshot()
	state, err := s.Keys(context.Background())
	if err != nil || s.Faulted() || !reflect.DeepEqual(before, state) {
		t.Fatal("empty key batch changed or faulted the guest")
	}
}

func TestJSONUnicodeRejectsLossyEscapes(t *testing.T) {
	for _, source := range []string{`"\ud800"`, `"\udc00"`, `"\ud800x"`, `"\ud800\u0061"`, `"\ud800\udc00\udc00"`} {
		if validJSONUnicode([]byte(source)) {
			t.Fatal("invalid UTF-16 escape accepted")
		}
	}
	for _, source := range []string{`"\ud83d\ude42"`, `"\ufffd"`, `"a\u0000suffix"`, `"\\ud800"`, `{"text":"é🙂\r\n"}`} {
		if !validJSONUnicode([]byte(source)) {
			t.Fatal("valid JSON Unicode rejected")
		}
	}
}
