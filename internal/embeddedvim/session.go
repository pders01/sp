// Package embeddedvim hosts a pinned Vim-compatible engine in capability-limited
// QuickJS/Wasm. It provides no OS enforcement, stores, keys or terminal itself.
package embeddedvim

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const RuntimeSHA256 = "88fcbeead33cca3ec8394feb0fb3eec8047ec9ac5062aba7a63799cee11989b7"
const MaxTextBytes = 1 << 20

// One immutable, pinned Wasm program and one fixed runtime configuration. Cache
// only its compiled public code in memory, never JS sources, drafts or instances.
// Recompiling the interpreter for every fixture is needlessly costly under race.
var compilationCache = wazero.NewCompilationCache()

var (
	ErrInput       = errors.New("proof: unsupported input or budget")
	ErrGuest       = errors.New("proof: guest operation failed")
	ErrUnavailable = errors.New("proof: session unavailable")
	ErrSnapshot    = errors.New("proof: invalid guest snapshot")
)

type Cursor struct {
	Line int `json:"line"`
	Ch   int `json:"ch"` // UTF-16 code units, NOT bytes/runes/terminal cells.
}
type Prompt struct {
	Kind   string `json:"kind"`
	Prefix string `json:"prefix"` // Whitelisted prompt punctuation, including backward search.
	Value  string `json:"value"`
	Cursor int    `json:"cursor"` // UTF-16 code units in the prompt value.
}
type GuestAction struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// SelectionRange is half-open for character/block selections. In line mode its
// endpoints are Vim's inclusive source line bounds (columns do not select cells).
type SelectionRange struct {
	Anchor Cursor `json:"anchor"`
	Head   Cursor `json:"head"`
}
type GuestViewport struct {
	Top int `json:"top"`
}

type Snapshot struct {
	Text          string           `json:"text"`
	Cursor        Cursor           `json:"cursor"`
	Mode          string           `json:"mode"`
	Prompt        *Prompt          `json:"prompt"`
	Notices       []string         `json:"notices"`
	Action        *GuestAction     `json:"action"`
	SelectionMode string           `json:"selectionMode"` // Empty, char, line or block; visual only.
	Selections    []SelectionRange `json:"selections"`
	Matches       []SelectionRange `json:"matches"`
	Viewport      *GuestViewport   `json:"viewport"`
}
type Options struct {
	HeapBytes     uint32
	OperationTime time.Duration
}

// Session serializes all owned Wasm calls. A failed operation destroys its
// guest, retaining only the last acknowledged copy with latest text unknown.
// No cancellation detaches an executing guest or promotes partial state.
type Session struct {
	mu      sync.Mutex
	runtime wazero.Runtime
	module  api.Module
	jsctx   uint64
	ack     Snapshot
	budget  time.Duration
	faulted bool
	closed  bool
}

// New executes the same JS bundle used by the Node proof, on an existing pinned
// QuickJS-NG WASI artifact. It does NOT use fastschema/qjs.New: that factory
// mounts the host CWD and defaults stdout/stderr to host files. This small ABI
// host deliberately supplies no filesystem, environment, arguments or host FDs.
// The artifact is reused without rebuilding/porting the JS interpreter.
func New(ctx context.Context, wasm, bundle []byte, initial string, opts Options) (_ *Session, err error) {
	if ctx == nil || !validText(initial) || len(bundle) == 0 || len(bundle) > 2<<20 || !utf8.Valid(bundle) {
		return nil, ErrInput
	}
	sum := sha256.Sum256(wasm)
	if hex.EncodeToString(sum[:]) != RuntimeSHA256 {
		return nil, ErrInput
	}
	if opts.HeapBytes == 0 {
		opts.HeapBytes = 64 << 20
	}
	if opts.OperationTime == 0 {
		opts.OperationTime = 500 * time.Millisecond
	}
	if opts.HeapBytes < 8<<20 || opts.HeapBytes > 64<<20 || opts.OperationTime < time.Millisecond || opts.OperationTime > 2*time.Second {
		return nil, ErrInput
	}
	if ctx.Err() != nil {
		return nil, ErrGuest
	}
	cfg := wazero.NewRuntimeConfig().WithCompilationCache(compilationCache).WithMemoryLimitPages(2048).
		WithCloseOnContextDone(true).WithDebugInfoEnabled(false)
	s := &Session{runtime: wazero.NewRuntimeWithConfig(context.Background(), cfg), budget: opts.OperationTime}
	defer func() {
		if err != nil {
			_ = s.runtime.Close(context.Background())
		}
	}()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	compiled, e := s.runtime.CompileModule(startup, wasm)
	if e != nil {
		return nil, ErrGuest
	}
	// Only the pinned WASI imports and a rejecting callback stub are admissible.
	for _, fn := range compiled.ImportedFunctions() {
		module, name, _ := fn.Import()
		if module != wasi_snapshot_preview1.ModuleName && (module != "env" || name != "jsFunctionProxy") {
			return nil, ErrGuest
		}
	}
	if _, e = wasi_snapshot_preview1.Instantiate(startup, s.runtime); e != nil {
		return nil, ErrGuest
	}
	for _, fn := range compiled.ImportedFunctions() {
		module, name, _ := fn.Import()
		if module == "env" && name == "jsFunctionProxy" {
			_, e = s.runtime.NewHostModuleBuilder("env").NewFunctionBuilder().
				WithGoModuleFunction(api.GoModuleFunc(func(context.Context, api.Module, []uint64) {
					panic("proof: guest Go callback unavailable")
				}), fn.ParamTypes(), fn.ResultTypes()).Export(name).Instantiate(startup)
			if e != nil {
				return nil, ErrGuest
			}
		}
	}
	// Default WASI has no preopens/env/args/stdin; stdout and stderr are explicit
	// discard sinks, NOT host descriptors. No WithFS/WithDirMount/WithSys* calls.
	s.module, e = s.runtime.InstantiateModule(startup, compiled,
		wazero.NewModuleConfig().WithStartFunctions().WithStdout(io.Discard).WithStderr(io.Discard))
	if e != nil {
		return nil, ErrGuest
	}
	// The shipped C factory registers built-in std/os modules. They have NO
	// resources in this host. Remove their ambient global bindings before any
	// draft, but do not claim registered module imports have been removed.
	rt, e := s.call(startup, "New_QJS", uint64(opts.HeapBytes), 512<<10, 0, 0)
	if e != nil || rt == 0 {
		return nil, ErrGuest
	}
	s.jsctx, e = s.call(startup, "QJS_GetContext", rt)
	if e != nil || s.jsctx == 0 {
		return nil, ErrGuest
	}
	pending, e := s.call(startup, "JS_HasException", s.jsctx)
	if e != nil || pending != 0 {
		return nil, errors.Join(errors.New("proof: constructor pending exception"), ErrGuest)
	}
	if _, e = s.eval(startup, "void 0", false); e != nil {
		return nil, errors.Join(errors.New("proof: startup ABI evaluation"), e)
	}
	for _, name := range []string{"std", "os", "bjson", "print", "console", "loadScript", "scriptArgs",
		"navigator", "setTimeout", "setInterval", "clearTimeout", "clearInterval"} {
		// name is a fixed public allowlist entry, never guest/draft metadata.
		bootstrap := "delete globalThis." + name + "; if(typeof globalThis." + name + "!=='undefined') throw Error('ambient binding remains')"
		if _, e = s.eval(startup, bootstrap, false); e != nil {
			return nil, errors.Join(errors.New("proof: startup sanitize "+name), e)
		}
	}
	if _, e = s.eval(startup, string(bundle), false); e != nil {
		return nil, errors.Join(errors.New("proof: startup bundle"), e)
	}
	encoded, _ := json.Marshal(initial)
	if _, e = s.operation(startup, "proof.open("+string(encoded)+")"); e != nil {
		return nil, errors.Join(errors.New("proof: startup open"), e)
	}
	return s, nil
}

func validText(s string) bool { return len(s) <= MaxTextBytes && utf8.ValidString(s) }

func (s *Session) call(ctx context.Context, name string, args ...uint64) (uint64, error) {
	fn := s.module.ExportedFunction(name)
	if fn == nil {
		return 0, errors.Join(errors.New("proof: missing ABI export "+name), ErrGuest)
	}
	results, err := fn.Call(ctx, args...)
	if err != nil {
		// name comes only from fixed host-side ABI calls; discard raw Wasm errors.
		return 0, errors.Join(errors.New("proof: failed ABI call "+name), ErrGuest)
	}
	if len(results) == 0 {
		return 0, nil
	}
	// Wazero ABI results are uint64 slots. Interpret i32 booleans/pointers as
	// i32, not as whole slots (upper bits are not part of the Wasm value).
	if fn.Definition().ResultTypes()[0] == api.ValueTypeI32 {
		return uint64(uint32(results[0])), nil
	}
	return results[0], nil
}

func (s *Session) cstring(ctx context.Context, text string) (uint64, error) {
	if strings.IndexByte(text, 0) >= 0 {
		return 0, ErrInput
	}
	ptr, err := s.call(ctx, "malloc", uint64(len(text)+1))
	if err != nil || ptr == 0 || !s.module.Memory().Write(uint32(ptr), append([]byte(text), 0)) {
		return 0, ErrGuest
	}
	return ptr, nil
}

// eval keeps a single operation context active across guest evaluation, JSON
// serialization and result copying. JSON escapes embedded NUL; never export a
// raw document through the artifact's strlen-based QJS_ToCString helper.
func (s *Session) eval(ctx context.Context, source string, resultNeeded bool) (out string, err error) {
	src, err := s.cstring(ctx, source)
	if err != nil {
		return "", err
	}
	defer func() {
		_, e := s.call(ctx, "free", src)
		if e != nil && err == nil {
			err = e
		}
	}()
	file, err := s.cstring(ctx, "synthetic-guest.js")
	if err != nil {
		return "", err
	}
	defer func() {
		_, e := s.call(ctx, "free", file)
		if e != nil && err == nil {
			err = e
		}
	}()
	opts, err := s.call(ctx, "QJS_CreateEvalOption", src, 0, 0, file, 8)
	if err != nil || opts == 0 {
		return "", ErrGuest
	}
	defer func() {
		_, e := s.call(ctx, "free", opts)
		if e != nil && err == nil {
			err = e
		}
	}()
	fields, ok := s.module.Memory().Read(uint32(opts), 20)
	if !ok || uint64(binary.LittleEndian.Uint32(fields)) != src ||
		uint64(binary.LittleEndian.Uint32(fields[12:])) != file ||
		binary.LittleEndian.Uint32(fields[16:]) != 8 {
		return "", errors.Join(errors.New("proof: unexpected eval option layout"), ErrGuest)
	}
	value, err := s.call(ctx, "QJS_Eval", s.jsctx, opts)
	if err != nil {
		return "", err
	}
	defer func() {
		_, e := s.call(ctx, "QJS_FreeValue", s.jsctx, value)
		if e != nil && err == nil {
			err = e
		}
	}()
	exception, err := s.call(ctx, "QJS_IsException", value)
	if err != nil {
		return "", err
	}
	if exception != 0 {
		return "", errors.Join(errors.New("proof: guest exception "+s.exceptionClass(ctx)), ErrGuest)
	}
	if !resultNeeded {
		return "", nil
	}
	isString, err := s.call(ctx, "QJS_IsString", value)
	if err != nil || isString == 0 {
		return "", ErrGuest
	}
	packed, err := s.call(ctx, "QJS_ToCString", s.jsctx, value)
	if err != nil || packed == 0 {
		return "", ErrGuest
	}
	defer func() {
		_, e := s.call(ctx, "free", packed)
		if e != nil {
			err = ErrGuest
		}
	}()
	raw, ok := s.module.Memory().Read(uint32(packed), 8)
	if !ok {
		return "", ErrGuest
	}
	pair := binary.LittleEndian.Uint64(raw)
	ptr, size := uint32(pair>>32), uint32(pair)
	defer func() {
		_, e := s.call(ctx, "JS_FreeCString", s.jsctx, uint64(ptr))
		if e != nil {
			err = ErrGuest
		}
	}()
	if size > 8*MaxTextBytes {
		return "", ErrSnapshot
	}
	data, ok := s.module.Memory().Read(ptr, size)
	if !ok || !utf8.Valid(data) {
		return "", ErrSnapshot
	}
	return string(data), nil // Copies bytes before either allocation is freed.
}

func (s *Session) operation(ctx context.Context, expression string) (Snapshot, error) {
	encoded, err := s.eval(ctx, "JSON.stringify("+expression+")", true)
	if err != nil || !validJSONUnicode([]byte(encoded)) {
		return Snapshot{}, ErrGuest
	}
	var snapshot Snapshot
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || !validText(snapshot.Text) || snapshot.Cursor.Line < 0 || snapshot.Cursor.Ch < 0 ||
		(snapshot.Mode != "normal" && snapshot.Mode != "insert" && snapshot.Mode != "visual" && snapshot.Mode != "replace") ||
		len(snapshot.Notices) > 32 || !validSelectionSnapshot(snapshot) ||
		(snapshot.Mode == "visual" && snapshot.SelectionMode == "") {
		return Snapshot{}, ErrSnapshot
	}
	for _, notice := range snapshot.Notices {
		if len(notice) > 512 {
			return Snapshot{}, ErrSnapshot
		}
	}
	if snapshot.Prompt != nil && (len(snapshot.Prompt.Kind) > 32 || len(snapshot.Prompt.Value) > 4096 || snapshot.Prompt.Cursor < 0) {
		return Snapshot{}, ErrSnapshot
	}
	if snapshot.Action != nil && (!validActionKind(snapshot.Action.Kind) ||
		!validText(snapshot.Action.Text) || snapshot.Action.Text != snapshot.Text) {
		return Snapshot{}, ErrSnapshot
	}
	if ctx.Err() != nil {
		return Snapshot{}, ErrGuest
	}
	s.ack = snapshot
	return clone(snapshot), nil
}

func clone(s Snapshot) Snapshot {
	s.Notices = append([]string(nil), s.Notices...)
	s.Selections = append([]SelectionRange(nil), s.Selections...)
	s.Matches = append([]SelectionRange(nil), s.Matches...)
	if s.Viewport != nil {
		v := *s.Viewport
		s.Viewport = &v
	}
	if s.Prompt != nil {
		p := *s.Prompt
		s.Prompt = &p
	}
	if s.Action != nil {
		a := *s.Action
		s.Action = &a
	}
	return s
}

func (s *Session) run(ctx context.Context, expression string) (Snapshot, error) {
	if s.closed || s.faulted {
		return Snapshot{}, ErrUnavailable
	}
	if ctx == nil {
		return Snapshot{}, ErrInput
	}
	op, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()
	out, err := s.operation(op, expression)
	if err != nil {
		s.faulted = true
		_ = s.destroy()
	}
	return out, err
}

func (s *Session) Keys(ctx context.Context, keys ...string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(keys) > 4096 {
		return Snapshot{}, ErrInput
	}
	for _, key := range keys {
		if !utf8.ValidString(key) || len(key) > 32 {
			return Snapshot{}, ErrInput
		}
	}
	if keys == nil {
		keys = []string{}
	}
	data, _ := json.Marshal(keys)
	return s.run(ctx, "proof.keys("+string(data)+")")
}
func (s *Session) InsertLiteral(ctx context.Context, text string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validText(text) {
		return Snapshot{}, ErrInput
	}
	data, _ := json.Marshal(text)
	return s.run(ctx, "proof.insertLiteral("+string(data)+")")
}
func (s *Session) FreezeAction(ctx context.Context, kind string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validActionKind(kind) {
		return Snapshot{}, ErrInput
	}
	data, _ := json.Marshal(kind)
	return s.run(ctx, "proof.freeze("+string(data)+")")
}
func (s *Session) ResumeAction(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ack.Action == nil {
		return Snapshot{}, ErrInput
	}
	return s.run(ctx, "proof.resume()")
}
func (s *Session) ResizeEditor(ctx context.Context, width, height int) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if width < 1 || width > 240 || height < 1 || height > 100 {
		return Snapshot{}, ErrInput
	}
	data, _ := json.Marshal([]int{width, height})
	return s.run(ctx, "proof.resize(..."+string(data)+")")
}
func (s *Session) Snapshot() Snapshot { s.mu.Lock(); defer s.mu.Unlock(); return clone(s.ack) }
func (s *Session) Faulted() bool      { s.mu.Lock(); defer s.mu.Unlock(); return s.faulted }
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.destroy()
}

func (s *Session) destroy() error {
	s.closed = true
	err := s.runtime.Close(context.Background())
	s.module, s.runtime = nil, nil
	if err != nil {
		return ErrGuest
	}
	return nil
}

// EvaluateForTest is ONLY for synthetic capability/resource negatives. It is not
// exposed to SP input or an editor adapter; failures destroy the guest as above.
func (s *Session) EvaluateForTest(ctx context.Context, expression string) (out string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.faulted {
		return "", ErrUnavailable
	}
	if ctx == nil || !utf8.ValidString(expression) || len(expression) > MaxTextBytes {
		return "", ErrInput
	}
	op, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()
	out, err = s.eval(op, "JSON.stringify(("+expression+"))", true)
	if err != nil {
		s.faulted = true
		_ = s.destroy()
	}
	return out, err
}

// encoding/json replaces invalid UTF-16 escapes with U+FFFD. Reject those before
// decoding so an editor's split surrogate can never normalize stored bytes.
func validJSONUnicode(data []byte) bool {
	if !utf8.Valid(data) || !json.Valid(data) {
		return false
	}
	inString := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || data[i] != '\\' {
			continue
		}
		i++
		if data[i] != 'u' {
			continue
		}
		unit := func(p []byte) uint16 {
			var n uint16
			for _, b := range p {
				n <<= 4
				switch {
				case b >= '0' && b <= '9':
					n += uint16(b - '0')
				case b >= 'a' && b <= 'f':
					n += uint16(b - 'a' + 10)
				default:
					n += uint16(b - 'A' + 10)
				}
			}
			return n
		}
		c := unit(data[i+1 : i+5])
		i += 4
		if c >= 0xdc00 && c <= 0xdfff {
			return false
		}
		if c >= 0xd800 && c <= 0xdbff {
			if i+6 >= len(data) || !bytes.Equal(data[i+1:i+3], []byte{'\\', 'u'}) {
				return false
			}
			low := unit(data[i+3 : i+7])
			if low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
