package sharedmcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pders01/sp/internal/scratchpad"
)

// TestProcessHelper has the SAME immediate-exit ownership rule as production.
// Blocking/deadline tests are never run as returning in-process servers.
func TestProcessHelper(t *testing.T) {
	if os.Getenv("SP_SYNTHETIC_HELPER") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	cfg, err := parseConfig(args)
	if err != nil {
		os.Exit(2)
	}
	var store sharedStore = managerAt(t, cfg.store)
	budget, grace := 2*time.Second, 200*time.Millisecond
	mode := os.Getenv("SP_SYNTHETIC_MODE")
	if mode == "deadline" {
		budget = 150 * time.Millisecond
	}
	if mode == "cancel-deadline" {
		budget, grace = 500*time.Millisecond, 2*time.Second
	}
	if mode == "blocked" || mode == "deadline" || mode == "real-lock" || mode == "cancel-deadline" {
		ready := os.NewFile(3, "synthetic-ready")
		store = &slowStore{store: store, ready: ready, path: filepath.Join(cfg.store, "must-not-appear"), real: mode == "real-lock"}
	}
	if mode == "instant-error" {
		store = &spyStore{err: fmt.Errorf("PRIVATE-CANARY /synthetic/secret-path")}
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	var output io.Writer = os.Stdout
	if mode == "cancel-deadline" {
		output = blockedCancellationOutput{output}
	}
	os.Exit(serveProcess(cfg, store, os.Stdin, output, signals, budget, grace))
}

type blockedCancellationOutput struct{ next io.Writer }

func (w blockedCancellationOutput) Write(data []byte) (int, error) {
	if bytes.Contains(data, []byte("APPEND_UNCONFIRMED")) {
		// Only in the owned helper process; its hard deadline kills this writer.
		select {}
	}
	return w.next.Write(data)
}

// Only this test backend delays; production always uses the existing manager.
type slowStore struct {
	store sharedStore
	ready *os.File
	path  string
	real  bool
}

func (s *slowStore) started() { _, _ = s.ready.Write([]byte{1}) }
func (s *slowStore) ReadShared(date string) (*scratchpad.SharedPage, error) {
	s.started()
	<-time.After(3 * time.Second)
	_ = os.WriteFile(s.path, []byte("BAD"), 0o600)
	return s.store.ReadShared(date)
}
func (s *slowStore) AppendDetailed(date, section, source string, bullets []string) (*scratchpad.AppendResult, error) {
	s.started()
	if !s.real {
		<-time.After(3 * time.Second)
		_ = os.WriteFile(s.path, []byte("BAD"), 0o600)
	}
	return s.store.AppendDetailed(date, section, source, bullets)
}

type session struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	stdout io.ReadCloser
	stderr bytes.Buffer
	ready  *os.File
	waited bool
}

func startSession(t *testing.T, store, mode, executable string, args ...string) *session {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		t.Skip("Unix subprocess readiness harness; Windows lifecycle unverified")
	}
	if executable == "" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		base := []string{"-test.run=^TestProcessHelper$", "--"}
		base = append(base, "--store="+store, "--read-dates="+testDate, "--append-dates="+testDate)
		cmd = exec.Command(self, base...)
		cmd.Env = []string{"SP_SYNTHETIC_HELPER=1", "SP_SYNTHETIC_MODE=" + mode, "GORACE=atexit_sleep_ms=0"}
	} else {
		cmd = exec.Command(executable, args...)
		cmd.Env = []string{}
	}
	s := &session{cmd: cmd}
	var err error
	s.in, err = cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	s.stdout = out
	s.out = bufio.NewReaderSize(out, maxOutput)
	cmd.Stderr = &s.stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	s.ready = r
	cmd.ExtraFiles = []*os.File{w}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	t.Cleanup(func() {
		_ = s.in.Close()
		_ = r.Close()
		if !s.waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return s
}
func (s *session) send(t *testing.T, v any) { t.Helper(); s.raw(t, string(mustJSON(t, v))+"\n") }
func (s *session) raw(t *testing.T, data string) {
	t.Helper()
	done := make(chan error, 1)
	go func() { _, err := io.WriteString(s.in, data); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("input pipe blocked")
	}
}
func (s *session) read(t *testing.T) map[string]any {
	t.Helper()
	ch := make(chan frame, 1)
	go func() { b, err := s.out.ReadBytes('\n'); ch <- frame{b, err} }()
	select {
	case f := <-ch:
		if f.err != nil {
			t.Fatalf("reply missing: %v", f.err)
		}
		if len(f.data) > maxOutput {
			t.Fatal("oversized output")
		}
		var m map[string]any
		if err := json.Unmarshal(f.data, &m); err != nil {
			t.Fatal("non-JSON stdout")
		}
		if m["jsonrpc"] != "2.0" {
			t.Fatal("bad protocol envelope")
		}
		return m
	case <-time.After(4 * time.Second):
		t.Fatal("reply blocked")
		return nil
	}
}
func (s *session) wait(t *testing.T, success bool) {
	t.Helper()
	s.waitWithin(t, success, 5*time.Second)
}

func (s *session) waitWithin(t *testing.T, success bool, timeout time.Duration) {
	t.Helper()
	ch := make(chan error, 1)
	s.waited = true
	go func() { ch <- s.cmd.Wait() }()
	select {
	case err := <-ch:
		if (err == nil) != success {
			t.Fatalf("exit=%v, want success=%v, stderr=%q", err, success, s.stderr.String())
		}
	case <-time.After(timeout):
		_ = s.cmd.Process.Kill()
		<-ch
		t.Fatal("process failed bounded termination")
	}
	if s.stderr.Len() > 256 || strings.Contains(s.stderr.String(), "CANARY") || strings.Contains(s.stderr.String(), "secret-path") {
		t.Fatal("unsafe diagnostics")
	}
}
func (s *session) initialized(t *testing.T) {
	t.Helper()
	s.send(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "synthetic", "version": "1"}}})
	m := s.read(t)
	if m["error"] != nil {
		t.Fatalf("initialize failed: %v", m)
	}
	s.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
}
func (s *session) call(t *testing.T, id int, isAppend bool) {
	t.Helper()
	name := "sp_shared_read"
	args := map[string]any{"date": testDate}
	if isAppend {
		name = "sp_shared_append"
		args = appendArgs("- one", "two", "one")
	}
	s.send(t, map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": callParams(name, args)})
}
func (s *session) waitStarted(t *testing.T) {
	t.Helper()
	ch := make(chan error, 1)
	go func() { var b [1]byte; _, err := io.ReadFull(s.ready, b[:]); ch <- err }()
	select {
	case err := <-ch:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker never started")
	}
}
func rpcRequest(id int, method string) any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
}
func responseError(t *testing.T, m map[string]any, code, outcome string) {
	t.Helper()
	b := mustJSON(t, m["result"])
	var r toolResult
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	toolError(t, r, code, outcome)
}

func TestTransportInitializationAndProtocol(t *testing.T) {
	store := t.TempDir()
	s := startSession(t, store, "", "")
	s.send(t, rpcRequest(2, "tools/list"))
	if s.read(t)["error"] == nil {
		t.Fatal("pre-init tool allowed")
	}
	s.raw(t, `{"jsonrpc":"2.0","id":3,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`+"\n")
	if s.read(t)["error"] == nil {
		t.Fatal("unsupported version accepted")
	}
	s.initialized(t)
	s.send(t, rpcRequest(4, "tools/list"))
	if len(s.read(t)["result"].(map[string]any)["tools"].([]any)) != 2 {
		t.Fatal("tool list changed")
	}
	for _, method := range []string{"resources/list", "roots/list", "logging/setLevel", "private/unlock", "shutdown", "$/setPolicy"} {
		s.send(t, rpcRequest(5, method))
		if s.read(t)["error"] == nil {
			t.Fatal("unknown method accepted")
		}
	}
	for _, raw := range []string{`[]`, `{"jsonrpc":"2.0","id":1,"id":2,"method":"ping"}`, `{"jsonrpc":"2.0","id":"\ud800","method":"ping"}`, `{"jsonrpc":"2.0","id":2,"method":"ping","params":null}`} {
		s.raw(t, raw+"\n")
		if s.read(t)["error"] == nil {
			t.Fatal("malformed protocol accepted")
		}
	}
	s.send(t, rpcRequest(6, "ping"))
	if s.read(t)["error"] != nil {
		t.Fatal("ping failed")
	}
	s.call(t, 7, false)
	m := s.read(t)
	result := m["result"].(map[string]any)
	if result["structuredContent"].(map[string]any)["status"] != "missing" {
		t.Fatal("wrong missing result")
	}
	_ = s.in.Close()
	s.wait(t, true)
}

func TestTransportBusyCancelEOFSignalAndDeadline(t *testing.T) {
	for _, action := range []string{"cancel-append", "cancel-read", "eof", "partial-eof", "signal", "deadline", "malformed-frame"} {
		t.Run(action, func(t *testing.T) {
			store := t.TempDir()
			mode := "blocked"
			if action == "deadline" {
				mode = "deadline"
			}
			s := startSession(t, store, mode, "")
			s.initialized(t)
			s.call(t, 10, action != "cancel-read")
			s.waitStarted(t)
			if action != "deadline" {
				s.call(t, 11, true)
				responseError(t, s.read(t), "BUSY", "not_attempted")
				s.send(t, rpcRequest(12, "ping"))
				if s.read(t)["error"] != nil {
					t.Fatal("busy ping blocked")
				}
				// Reuse of an active ID must not produce a stale success.
				s.send(t, rpcRequest(10, "ping"))
				if s.read(t)["error"] == nil {
					t.Fatal("active ID reused")
				}
			}
			switch action {
			case "cancel-append", "cancel-read":
				s.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": 10, "reason": "PRIVATE-CANARY"}})
				m := s.read(t)
				if action == "cancel-append" {
					responseError(t, m, "APPEND_UNCONFIRMED", "unknown")
				} else {
					responseError(t, m, "STORE_UNAVAILABLE", "not_applicable")
				}
			case "eof":
				_ = s.in.Close()
			case "partial-eof":
				s.raw(t, `{"jsonrpc":"2.0","id":19,"method":"tools/call"`)
				_ = s.in.Close()
			case "signal":
				if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			case "malformed-frame":
				s.raw(t, strings.Repeat("x", maxFrame)+"\n")
			}
			s.wait(t, false)
			if _, err := os.Stat(filepath.Join(store, "must-not-appear")); !os.IsNotExist(err) {
				t.Fatal("worker mutated after fatal shutdown")
			}
			if _, err := os.Stat(filepath.Join(store, testDate+".json")); !os.IsNotExist(err) {
				t.Fatal("blocked append published")
			}
		})
	}
}

func TestCancellationBlockedReplyHonorsRemainingOperationBudget(t *testing.T) {
	store := t.TempDir()
	s := startSession(t, store, "cancel-deadline", "")
	s.initialized(t)
	s.call(t, 10, true)
	s.waitStarted(t)
	start := time.Now()
	s.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": 10}})
	s.wait(t, false)
	if time.Since(start) >= 1500*time.Millisecond {
		t.Fatal("blocked cancellation reply exceeded the remaining operation budget")
	}
	if _, err := os.Stat(filepath.Join(store, "must-not-appear")); !os.IsNotExist(err) {
		t.Fatal("worker survived deadline termination")
	}
}

func TestTransportUnconfirmedAndFrameAtomicity(t *testing.T) {
	store := t.TempDir()
	s := startSession(t, store, "instant-error", "")
	s.initialized(t)
	s.call(t, 10, true)
	responseError(t, s.read(t), "APPEND_UNCONFIRMED", "unknown")
	s.call(t, 11, false)
	responseError(t, s.read(t), "STORE_UNAVAILABLE", "not_applicable")
	_ = s.in.Close()
	s.wait(t, true)
	for _, input := range []string{
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sp_shared_append","arguments":{"date":"` + testDate + `","section":"Updates","source":"synthetic","bullets":["public"]}}}`,
		strings.Repeat(" ", maxFrame) + "\n",
	} {
		store := t.TempDir()
		s := startSession(t, store, "", "")
		s.initialized(t)
		s.raw(t, input)
		_ = s.in.Close()
		s.wait(t, false)
		if _, err := os.Stat(filepath.Join(store, testDate+".json")); !os.IsNotExist(err) {
			t.Fatal("oversized/unterminated prefix published")
		}
	}
}

func TestExecutableSyntheticStore(t *testing.T) {
	if testing.Short() {
		t.Skip("CLI build subprocess")
	}
	exe := buildExecutable(t)
	store := t.TempDir()
	s := startSession(t, store, "", exe, "--store="+store, "--read-dates="+testDate, "--append-dates="+testDate)
	s.initialized(t)
	s.call(t, 10, true)
	m := s.read(t)
	r := m["result"].(map[string]any)
	if r["isError"] != false {
		t.Fatal("CLI append failed")
	}
	structured := r["structuredContent"]
	var text map[string]any
	if err := json.Unmarshal([]byte(r["content"].([]any)[0].(map[string]any)["text"].(string)), &text); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustJSON(t, structured), mustJSON(t, text)) {
		t.Fatal("CLI dual output differs")
	}
	s.call(t, 11, true)
	r = s.read(t)["result"].(map[string]any)
	if len(r["structuredContent"].(map[string]any)["added_bullets"].([]any)) != 0 {
		t.Fatal("CLI retry not duplicate-only")
	}
	s.call(t, 12, false)
	r = s.read(t)["result"].(map[string]any)
	if !strings.Contains(r["structuredContent"].(map[string]any)["content"].(string), "- one\n- two\n") {
		t.Fatal("CLI content wrong")
	}
	_ = s.in.Close()
	s.wait(t, true)
	for _, pipeMode := range []string{"blocked", "broken"} {
		t.Run("CLI-stdout-"+pipeMode, func(t *testing.T) {
			pipeStore := t.TempDir()
			fixture(t, pipeStore, testDate+".json", sharedFixture(strings.Repeat("\x00", maxRead)))
			p := startSession(t, pipeStore, "", exe, "--store="+pipeStore, "--read-dates="+testDate, "--append-dates=")
			p.initialized(t)
			if pipeMode == "broken" {
				_ = p.stdout.Close()
			}
			p.call(t, 20, false)
			// Do not read the large reply. A full pipe must terminate the
			// actual executable instead of keeping a detached worker/host.
			p.wait(t, false)
		})
	}
	// Missing explicit policy: safe startup error, protocol stdout stays empty.
	bad := startSession(t, store, "", exe, "--store="+store)
	_ = bad.in.Close()
	bad.wait(t, false)
	if b, err := io.ReadAll(bad.out); err == nil && len(b) != 0 {
		t.Fatal("startup stdout is not protocol-only")
	}
}
