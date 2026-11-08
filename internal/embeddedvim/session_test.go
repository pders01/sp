package embeddedvim

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

// Synthetic fixtures only. Never include snapshots, document text, or guest
// errors in assertion diagnostics. Missing prepared assets are failures, not skips.
func proofAssets(t *testing.T) (wasm, bundle []byte) {
	t.Helper()
	const missing = "proof: required synthetic assets missing or unreadable"
	var err error
	wasm, err = os.ReadFile("assets/qjs.wasm")
	if err != nil || len(wasm) == 0 {
		t.Fatal(missing)
	}
	bundle, err = os.ReadFile("assets/guest.js")
	if err != nil || len(bundle) == 0 {
		t.Fatal(missing)
	}
	data, err := os.ReadFile("assets/manifest.json")
	if err != nil || len(data) == 0 {
		t.Fatal(missing)
	}
	var manifest struct {
		FormatVersion int               `json:"formatVersion"`
		Sources       map[string]string `json:"sources"`
		Bundle        struct {
			File   string `json:"file"`
			SHA256 string `json:"sha256"`
		} `json:"bundle"`
	}
	if json.Unmarshal(data, &manifest) != nil || manifest.FormatVersion != 3 || manifest.Bundle.File != "guest.js" {
		t.Fatal("proof: invalid synthetic asset manifest")
	}
	for _, name := range []string{"adapter.js", "guest-entry.js", "bundle.js", "core-policy.js", "package.json", "package-lock.json"} {
		source, err := os.ReadFile("../../scripts/embedded-vim/" + name) // Fixed allowlist, not manifest-controlled paths.
		if err != nil {
			t.Fatal("proof: shared source unavailable")
		}
		sum := sha256.Sum256(source)
		if hex.EncodeToString(sum[:]) != manifest.Sources[name] {
			t.Fatal("proof: stale shared guest assets; rebuild them")
		}
	}
	bundleSum, runtimeSum := sha256.Sum256(bundle), sha256.Sum256(wasm)
	if hex.EncodeToString(bundleSum[:]) != manifest.Bundle.SHA256 {
		t.Fatal("proof: guest bundle digest mismatch")
	}
	if hex.EncodeToString(runtimeSum[:]) != RuntimeSHA256 {
		t.Fatal("proof: runtime artifact digest mismatch")
	}
	return wasm, bundle
}

func openProof(t *testing.T, initial string, opts Options) *Session {
	t.Helper()
	wasm, bundle := proofAssets(t)
	s, err := New(context.Background(), wasm, bundle, initial, opts)
	if err != nil || s == nil {
		t.Fatal("proof: synthetic session creation failed")
	}
	t.Cleanup(func() {
		if s.Close() != nil {
			t.Error("proof: synthetic session close failed")
		}
	})
	expectText(t, s.Snapshot(), initial)
	return s
}

func acknowledged(t *testing.T, s *Session, state Snapshot) Snapshot {
	t.Helper()
	if state.Cursor.Line < 0 || state.Cursor.Ch < 0 ||
		(state.Mode != "normal" && state.Mode != "insert" && state.Mode != "visual" && state.Mode != "replace") ||
		(state.Prompt != nil && state.Prompt.Kind == "") {
		t.Fatal("proof: invalid acknowledged snapshot shape")
	}
	if !reflect.DeepEqual(s.Snapshot(), state) {
		t.Fatal("proof: snapshot differs from last acknowledgement")
	}
	return state
}

func keys(t *testing.T, s *Session, input ...string) Snapshot {
	t.Helper()
	state, err := s.Keys(context.Background(), input...)
	if err != nil {
		t.Fatal("proof: synthetic key operation failed")
	}
	return acknowledged(t, s, state)
}

func literal(t *testing.T, s *Session, text string) Snapshot {
	t.Helper()
	state, err := s.InsertLiteral(context.Background(), text)
	if err != nil {
		t.Fatal("proof: synthetic literal operation failed")
	}
	return acknowledged(t, s, state)
}

func expectText(t *testing.T, state Snapshot, expected string) {
	t.Helper()
	// Go string equality compares exact bytes, with no Unicode/newline normalization.
	if state.Text != expected {
		t.Fatal("proof: document differs from expected exact bytes")
	}
}

func expectCursor(t *testing.T, state Snapshot, line, ch int) {
	t.Helper()
	if state.Cursor.Line != line || state.Cursor.Ch != ch {
		t.Fatal("proof: cursor differs from expected position")
	}
}

func expectNormal(t *testing.T, state Snapshot) {
	t.Helper()
	if state.Mode != "normal" || state.Prompt != nil {
		t.Fatal("proof: completed or cancelled operation did not return to normal mode")
	}
}

func typePrompt(t *testing.T, s *Session, opener, value string) Snapshot {
	t.Helper()
	state := keys(t, s, opener)
	kind := "ex"
	if opener == "/" {
		kind = "search"
	}
	if state.Prompt == nil || state.Prompt.Kind != kind || state.Prompt.Value != "" {
		t.Fatal("proof: typed opener did not create the expected empty prompt")
	}
	for _, character := range value {
		state = keys(t, s, string(character))
		if state.Prompt == nil || state.Prompt.Kind != kind {
			t.Fatal("proof: typing left the opened prompt")
		}
	}
	if state.Prompt == nil || state.Prompt.Value != value {
		t.Fatal("proof: typed prompt did not retain exact input")
	}
	return state
}

func ex(t *testing.T, s *Session, command string) Snapshot {
	t.Helper()
	typePrompt(t, s, ":", command)
	return keys(t, s, "<CR>")
}

func expectNotice(t *testing.T, state Snapshot) {
	t.Helper()
	for _, notice := range state.Notices {
		if notice == "Unsupported substitute flags or count" {
			return
		}
	}
	t.Fatal("proof: unsupported substitution was not reported")
}

func TestPinnedAssets(t *testing.T) {
	wasm, bundle := proofAssets(t)
	// New must enforce its own artifact pin, not merely rely on this manifest check.
	altered := append([]byte(nil), wasm...)
	altered[len(altered)-1] ^= 1
	s, err := New(context.Background(), altered, bundle, "synthetic fixture", Options{})
	if s != nil {
		_ = s.Close()
		t.Fatal("proof: altered runtime artifact was accepted")
	}
	if !errors.Is(err, ErrInput) {
		t.Fatal("proof: altered runtime artifact did not produce the fixed input error")
	}
}

func TestTypedPrompts(t *testing.T) {
	t.Run("colon line motion", func(t *testing.T) {
		s := openProof(t, "first\nsecond\nthird", Options{})
		state := ex(t, s, "2")
		expectText(t, state, "first\nsecond\nthird")
		expectCursor(t, state, 1, 0)
		expectNormal(t, state)
	})
	t.Run("search repeat and history", func(t *testing.T) {
		s := openProof(t, "head\nneedle tail\nneedle end", Options{})
		typePrompt(t, s, "/", "needle")
		state := keys(t, s, "<CR>")
		expectNormal(t, state)
		expectText(t, state, "head\nneedle tail\nneedle end")
		expectCursor(t, state, 1, 0)
		expectCursor(t, keys(t, s, "n"), 2, 0)
		expectCursor(t, keys(t, s, "N"), 1, 0)
		keys(t, s, "/")
		state = keys(t, s, "<Up>")
		if state.Prompt == nil || state.Prompt.Kind != "search" || state.Prompt.Value != "needle" {
			t.Fatal("proof: search history did not recall typed input")
		}
		expectNormal(t, keys(t, s, "<Esc>"))
	})
	t.Run("cancel", func(t *testing.T) {
		s := openProof(t, "cat cat\nneedle", Options{})
		for _, prompt := range []struct{ opener, value string }{{":", "%s/cat/dog/g"}, {"/", "needle"}} {
			typePrompt(t, s, prompt.opener, prompt.value)
			state := keys(t, s, "<Esc>")
			expectNormal(t, state)
			expectText(t, state, "cat cat\nneedle")
		}
	})
}

func TestSubstitute(t *testing.T) {
	for _, tc := range []struct{ name, initial, setup, command, want string }{
		{"first match per line", "cat cat\ncat cat", "", "%s/cat/dog/", "dog cat\ndog cat"},
		{"global", "cat cat\ncat cat", "", "%s/cat/dog/g", "dog dog\ndog dog"},
		{"case insensitive", "cat CAT Cat", "", "%s/cat/dog/gi", "dog dog dog"},
		{"captures", "a11 a12\na13", "set nopcre", `%s/\(\d\+\)/\1\1/g`, "a1111 a1212\na1313"},
		{"alternate delimiter", "a/b a/b", "", "%s#a/b#ok#g", "ok ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openProof(t, tc.initial, Options{})
			if tc.setup != "" {
				expectNormal(t, ex(t, s, tc.setup))
			}
			state := ex(t, s, tc.command)
			expectText(t, state, tc.want)
			expectNormal(t, state)
		})
	}
	t.Run("confirmation skip accept all", func(t *testing.T) {
		s := openProof(t, "cat cat\ncat cat", Options{})
		state := ex(t, s, "%s/cat/dog/gc")
		if state.Prompt == nil || state.Prompt.Kind != "confirm" {
			t.Fatal("proof: substitution did not open confirmation")
		}
		expectText(t, state, "cat cat\ncat cat")
		state = keys(t, s, "n")
		if state.Prompt == nil {
			t.Fatal("proof: skipped match prematurely closed confirmation")
		}
		expectText(t, state, "cat cat\ncat cat")
		state = keys(t, s, "y")
		if state.Prompt == nil {
			t.Fatal("proof: accepted match prematurely closed confirmation")
		}
		expectText(t, state, "cat dog\ncat cat")
		state = keys(t, s, "a")
		expectText(t, state, "cat dog\ndog dog")
		expectNormal(t, state)
	})
	t.Run("confirmation quit", func(t *testing.T) {
		s := openProof(t, "cat cat\ncat cat", Options{})
		ex(t, s, "%s/cat/dog/gc")
		keys(t, s, "y")
		state := keys(t, s, "q")
		expectText(t, state, "dog cat\ncat cat")
		expectNormal(t, state)
	})
	t.Run("confirmation escape", func(t *testing.T) {
		s := openProof(t, "cat cat", Options{})
		ex(t, s, "%s/cat/dog/gc")
		state := keys(t, s, "<Esc>")
		expectText(t, state, "cat cat")
		expectNormal(t, state)
	})
	for _, tc := range []struct{ name, initial, setup, command string }{
		{"reject n", "cat cat\ncat", "", "%s/cat/dog/gn"},
		{"reject I", "alpha ALPHA alpha", "set ignorecase", "%s/alpha/Z/gI"},
		{"reject alternate delimiter flags", "cat cat", "", "%s#cat#dog#gn"},
		{"reject count", "cat cat", "", "s/cat/dog/g 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openProof(t, tc.initial, Options{})
			if tc.setup != "" {
				expectNormal(t, ex(t, s, tc.setup))
			}
			state := ex(t, s, tc.command)
			expectText(t, state, tc.initial)
			expectNormal(t, state)
			expectNotice(t, state)
		})
	}
	t.Run("mixed endings undo", func(t *testing.T) {
		const initial = "cat🙂\r\ncat e\u0301\ncat\r\n"
		s := openProof(t, initial, Options{})
		expectText(t, ex(t, s, "%s/cat/dog/g"), "dog🙂\r\ndog e\u0301\ndog\r\n")
		// Match the JS proof: upstream multi-range substitutions are not claimed
		// to be grouped into one native-Vim undo transaction.
		var state Snapshot
		for i := 0; i < 3; i++ {
			state = keys(t, s, "u")
		}
		expectText(t, state, initial)
	})
}

func TestRegistersMacrosAndUndo(t *testing.T) {
	t.Run("named linewise register", func(t *testing.T) {
		s := openProof(t, "first\nsecond", Options{})
		expectText(t, keys(t, s, `"`, "a", "y", "y"), "first\nsecond")
		state := keys(t, s, "j", `"`, "a", "p")
		expectText(t, state, "first\nsecond\nfirst")
		expectNormal(t, state)
	})
	t.Run("macro replay", func(t *testing.T) {
		s := openProof(t, "abcd", Options{})
		expectText(t, keys(t, s, "q", "a", "x", "q"), "bcd")
		expectText(t, keys(t, s, "@", "a"), "cd")
		state := keys(t, s, "@", "@")
		expectText(t, state, "d")
		expectNormal(t, state)
	})
	t.Run("typed insert undo redo", func(t *testing.T) {
		s := openProof(t, "seed", Options{})
		if keys(t, s, "i").Mode != "insert" {
			t.Fatal("proof: insert key did not enter insert mode")
		}
		state := keys(t, s, "X", "Y", "<Esc>")
		expectText(t, state, "XYseed")
		expectNormal(t, state)
		expectText(t, keys(t, s, "u"), "seed")
		state = keys(t, s, "<C-r>")
		expectText(t, state, "XYseed")
		expectNormal(t, state)
	})
}

func TestExactBytesEditUndo(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"empty", ""},
		{"CRLF final", "alpha\r\nbeta\r\n"},
		{"CRLF no final", "alpha\r\nbeta"},
		{"mixed endings", "alpha\r\nbeta\ngamma\r\ndelta\r"},
		{"LF no final", "alpha\nbeta"},
		{"LF final", "alpha\nbeta\n"},
		{"multiple final empty lines", "alpha\n\n\n"},
		{"Unicode unnormalized", "é e\u0301 漢字 🙂 𝄞\nΩ\u2028λ\u2029"},
		{"control bytes and NUL suffix", "a\x00b\x01c\x1bd\x7f\tend\nnext"},
		{"replacement character", "before\ufffdafter"},
		{"emoji", "🙂suffix"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openProof(t, tc.text, Options{})
			keys(t, s, "i")
			expectText(t, literal(t, s, "X"), "X"+tc.text)
			state := keys(t, s, "<Esc>")
			expectText(t, state, "X"+tc.text)
			expectNormal(t, state)
			state = keys(t, s, "u")
			expectText(t, state, tc.text)
			expectNormal(t, state)
		})
	}
}

func TestInsertLiteral(t *testing.T) {
	for _, tc := range []struct{ name, payload string }{
		{"command looking", ":s/seed/changed/g<Enter><Esc>\n:%s/seed/changed/g\n<C-r>"},
		{"exact bytes", "CRLF\r\nmixed\nfinal\r\n\x00suffix\ufffd🙂e\u0301\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openProof(t, "seed", Options{})
			keys(t, s, "i")
			state := literal(t, s, tc.payload)
			expectText(t, state, tc.payload+"seed")
			if state.Mode != "insert" || state.Prompt != nil {
				t.Fatal("proof: literal input executed mode or prompt keys")
			}
			state = keys(t, s, "<Esc>")
			expectText(t, state, tc.payload+"seed")
			expectNormal(t, state)
			expectText(t, keys(t, s, "u"), "seed")
		})
	}
}

func TestNoAmbientHostIO(t *testing.T) {
	s := openProof(t, "synthetic fixture", Options{})
	// The adapter deliberately has an inert document/window and guest-local
	// timers. The artifact also retains registered qjs:std/os modules: this is
	// not a claim that those imports are absent, nor an I/O attempt through them.
	const expression = `(()=>{
		const absent = ['process','require','fetch','XMLHttpRequest','WebSocket',
		  'localStorage','sessionStorage','indexedDB','console','std','os','bjson',
		  'print','loadScript','scriptArgs','navigator','Worker','Deno','Bun'];
		return absent.every(name => typeof globalThis[name] === 'undefined') &&
		  typeof window.fetch === 'undefined' &&
		  Function('return typeof process')() === 'undefined' &&
		  Function('return typeof require')() === 'undefined' &&
		  Function('return typeof std')() === 'undefined' &&
		  Function('return typeof os')() === 'undefined' &&
		  (function(){}).constructor('return typeof fetch')() === 'undefined';
	})()`
	before := s.Snapshot()
	out, err := s.EvaluateForTest(context.Background(), expression)
	if err != nil || out != "true" {
		t.Fatal("proof: ambient host capability or function-constructor escape appeared")
	}
	if s.Faulted() || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("proof: capability inspection changed acknowledged state")
	}
}

func expectFault(t *testing.T, s *Session, before Snapshot, err error) {
	t.Helper()
	if err == nil || (!errors.Is(err, ErrGuest) && !errors.Is(err, ErrSnapshot)) {
		t.Fatal("proof: guest fault did not return a fixed failure")
	}
	if !s.Faulted() || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("proof: fault did not retain only the last acknowledged snapshot")
	}
	if _, err := s.Keys(context.Background(), "i"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("proof: faulted session accepted keys")
	}
	if _, err := s.InsertLiteral(context.Background(), "X"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("proof: faulted session accepted literal input")
	}
	if _, err := s.EvaluateForTest(context.Background(), "true"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("proof: faulted session accepted evaluation")
	}
}

func TestLoopDeadline(t *testing.T) {
	s := openProof(t, "synthetic fixture", Options{OperationTime: 50 * time.Millisecond})
	before := s.Snapshot()
	started := time.Now()
	_, err := s.EvaluateForTest(context.Background(), "(()=>{for(;;){} })()")
	if time.Since(started) > 5*time.Second {
		t.Fatal("proof: infinite loop exceeded bounded deadline tolerance")
	}
	expectFault(t, s, before, err)
}

func TestHeapLimit(t *testing.T) {
	s := openProof(t, "synthetic fixture", Options{HeapBytes: 8 << 20, OperationTime: 2 * time.Second})
	before := s.Snapshot()
	// A caught allocation failure must return normally: otherwise a deadline
	// alone could masquerade as evidence that the heap budget was enforced.
	out, err := s.EvaluateForTest(context.Background(), `(()=>{
		try { new Array(50000000).fill(42); return false; }
		catch (_) { return true; }
	})()`)
	if err != nil || out != "true" || s.Faulted() || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("proof: heap allocation did not fail within a live guest")
	}
	_, err = s.EvaluateForTest(context.Background(), "new Array(50000000).fill(42)")
	expectFault(t, s, before, err)
}

func TestPartialMutationFaultRetainsAcknowledgement(t *testing.T) {
	s := openProof(t, "synthetic fixture", Options{})
	keys(t, s, "i", "A", "<Esc>")
	before := s.Snapshot()
	_, err := s.EvaluateForTest(context.Background(), `(()=>{
		proof.keys(['i', 'X']);
		throw new Error('synthetic-sensitive-detail');
	})()`)
	// Only a fixed allowlisted class may accompany the shared error sentinel.
	if !errors.Is(err, ErrGuest) || err.Error() != "proof: guest exception Error\nproof: guest operation failed" {
		t.Fatal("proof: guest exception was not replaced by fixed diagnostics")
	}
	expectFault(t, s, before, err)
}

func TestInvalidUTF8RejectedWithoutMutation(t *testing.T) {
	wasm, bundle := proofAssets(t)
	invalid := string([]byte{0xff, 0xfe})
	for _, tc := range []struct {
		initial string
		bundle  []byte
	}{{invalid, bundle}, {"synthetic fixture", []byte(invalid)}} {
		s, err := New(context.Background(), wasm, tc.bundle, tc.initial, Options{})
		if s != nil {
			_ = s.Close()
			t.Fatal("proof: invalid UTF-8 was accepted during initialization")
		}
		if err != ErrInput {
			t.Fatal("proof: invalid UTF-8 initialization did not return the fixed input error")
		}
	}
	s := openProof(t, "synthetic fixture", Options{})
	before := s.Snapshot()
	operations := []func() error{
		func() error { _, err := s.Keys(context.Background(), "i", "X", invalid); return err },
		func() error { _, err := s.InsertLiteral(context.Background(), invalid); return err },
		func() error { _, err := s.EvaluateForTest(context.Background(), invalid); return err },
	}
	for _, operation := range operations {
		if operation() != ErrInput {
			t.Fatal("proof: invalid UTF-8 did not return the fixed input error")
		}
		if s.Faulted() || !reflect.DeepEqual(before, s.Snapshot()) {
			t.Fatal("proof: host-rejected UTF-8 changed or faulted the session")
		}
	}
	expectText(t, keys(t, s, "i", "X", "<Esc>"), "Xsynthetic fixture")
}

func TestEscapedSurrogateSnapshotRejected(t *testing.T) {
	for _, tc := range []struct{ name, expression string }{
		{"lone high", `proof.open("\ud800suffix")`},
		{"lone low", `proof.open("\udc00suffix")`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openProof(t, "synthetic fixture", Options{})
			before := s.Snapshot()
			// Evaluation can produce escaped UTF-16 JSON, but does not acknowledge
			// editor state. The next snapshot must reject it before encoding/json
			// could silently replace the lone surrogate with U+FFFD.
			if _, err := s.EvaluateForTest(context.Background(), tc.expression); err != nil {
				t.Fatal("proof: synthetic escaped-surrogate fixture setup failed")
			}
			_, err := s.Keys(context.Background(), "<Esc>")
			expectFault(t, s, before, err)
		})
	}
}

func TestSnapshotDeepCopies(t *testing.T) {
	s := openProof(t, "cat cat", Options{})
	// Notices are drained at each guest snapshot. Submit the rejected command
	// and reopen the prompt in one call so both nested fields are populated.
	typePrompt(t, s, ":", "%s/cat/dog/gn")
	returned := keys(t, s, "<CR>", ":")
	expectNotice(t, returned)
	before := s.Snapshot()
	if returned.Prompt == nil || len(returned.Notices) == 0 {
		t.Fatal("proof: deep-copy fixture lacks mutable nested fields")
	}
	mutate := func(state *Snapshot) {
		state.Text = "changed host copy"
		state.Cursor.Line = 999
		state.Cursor.Ch = 999
		state.Mode = "insert"
		state.Prompt.Kind = "changed"
		state.Prompt.Value = "changed"
		state.Notices[0] = "changed"
	}
	mutate(&returned)
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("proof: caller mutated acknowledgement through returned operation snapshot")
	}
	copy := s.Snapshot()
	mutate(&copy)
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("proof: caller mutated acknowledgement through snapshot copy")
	}
	keys(t, s, "<Esc>", "i")
	inserted := literal(t, s, "X")
	before = s.Snapshot()
	inserted.Text = "changed host copy"
	inserted.Cursor.Line = 999
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("proof: literal result shared mutable acknowledgement fields")
	}
}

func TestRepeatedClose(t *testing.T) {
	s := openProof(t, "synthetic fixture", Options{})
	before := s.Snapshot()
	if s.Close() != nil || s.Close() != nil {
		t.Fatal("proof: repeated close was not successful")
	}
	if s.Faulted() || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("proof: clean close faulted or changed acknowledged state")
	}
	if _, err := s.Keys(context.Background(), "i"); err != ErrUnavailable {
		t.Fatal("proof: closed session accepted keys")
	}
	if _, err := s.InsertLiteral(context.Background(), "X"); err != ErrUnavailable {
		t.Fatal("proof: closed session accepted literal input")
	}
	if _, err := s.EvaluateForTest(context.Background(), "true"); err != ErrUnavailable {
		t.Fatal("proof: closed session accepted evaluation")
	}
}

func TestSessionIndependence(t *testing.T) {
	first := openProof(t, "first", Options{})
	second := openProof(t, "second", Options{})
	before := second.Snapshot()
	keys(t, first, "q", "a", "x", "q")
	typePrompt(t, first, ":", "2")
	if !reflect.DeepEqual(before, second.Snapshot()) {
		t.Fatal("proof: one session changed another session's acknowledgement")
	}
	// Inspect the second guest too; checking only its cached ack is insufficient.
	expectText(t, keys(t, second, "i", "Y", "<Esc>"), "Ysecond")
	if first.Close() != nil {
		t.Fatal("proof: independent session close failed")
	}
	expectText(t, keys(t, second, "u"), "second")
	expectNormal(t, second.Snapshot())
}
