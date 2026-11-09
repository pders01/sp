package sharedmcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/scratchpad"
)

const testDate = "2026-11-08"

func TestStrictProtocol(t *testing.T) {
	if !sameID(json.Number("0"), json.Number("-0")) || sameID("0", json.Number("0")) {
		t.Fatal("integer ID identity changed")
	}
	valid := []string{
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":"","method":"ping","params":{}}`,
		`{"jsonrpc":"2.0","id":-9007199254740991,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":"\ud83d\ude00","method":"ping"}`,
		`{"jsonrpc":"2.0","id":"\ufffd","method":"ping"}`,
	}
	for _, s := range valid {
		if _, code := decodeRequest([]byte(s)); code != 0 {
			t.Fatalf("valid rejected: %s", s)
		}
	}
	bad := []string{
		`[]`, `null`, `{}`, `{"jsonrpc":"2.0","id":null,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1.0,"method":"ping"}`, `{"jsonrpc":"2.0","id":1e0,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":9007199254740992,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":true,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":{},"method":"ping"}`,
		`{"jsonrpc":"2.0","id":"\ud800","method":"ping"}`,
		`{"jsonrpc":"2.0","id":"\udc00","method":"ping"}`,
		`{"jsonrpc":"2.0","id":"\ud800\u1234","method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":1,"\u0078":2}}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","method":"tools/call"}`,
		`{"jsonrpc":"2.0","id":1,"Method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":null}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":[]}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"anything":null}}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","extra":1}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"} {}`,
		`{"jsonrpc":"2.0","id":1,"result":{}}`,
		`{"jsonrpc":"2.0","id":"` + strings.Repeat("x", 129) + `","method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"` + strings.Repeat("x", 65) + `"}`,
		`{"jsonrpc":"2.0","id":"` + string([]byte{0xff}) + `","method":"ping"}`,
		strings.Repeat("[", 18) + `1` + strings.Repeat("]", 18),
	}
	for i, s := range bad {
		if _, code := decodeRequest([]byte(s)); code == 0 {
			t.Errorf("bad input %d accepted", i)
		}
	}
}

func TestExplicitConfig(t *testing.T) {
	store := t.TempDir()
	valid := []string{"--store", store, "--read-dates=", "--append-dates=" + testDate}
	cfg, err := parseConfig(valid)
	if err != nil || len(cfg.read) != 0 || !cfg.append[testDate] {
		t.Fatal("explicit append-only grants rejected")
	}
	bad := [][]string{
		nil, {"--store", store}, {"--store", store, "--read-dates=" + testDate},
		{"--store", store, "--read-dates=", "--append-dates="},
		{"--store", "relative", "--read-dates=" + testDate, "--append-dates="},
		{"--store", store + "/missing", "--read-dates=" + testDate, "--append-dates="},
		append(append([]string{}, valid...), "--store="+store),
		append(append([]string{}, valid...), "--network=true"),
		{"--store", store, "--read-dates=2026-02-29", "--append-dates="},
	}
	for _, args := range bad {
		if _, err := parseConfig(args); err == nil {
			t.Errorf("bad config accepted: %q", args)
		}
	}
	if _, err := os.Stat(filepath.Join(store, "missing")); !os.IsNotExist(err) {
		t.Fatal("created missing store")
	}
	file := filepath.Join(store, "file")
	if err := os.WriteFile(file, []byte("canary"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{file, filepath.Join(store, "link")} {
		if strings.HasSuffix(name, "link") {
			if err := os.Symlink(store, name); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := parseConfig([]string{"--store", name + "/", "--read-dates=" + testDate, "--append-dates="}); err == nil {
			t.Fatal("non-directory/symlink accepted")
		}
	}
	for _, s := range []string{"2024-02-29", "0000-01-01", testDate} {
		// Year zero follows the existing shared validator; no adapter reinterpretation.
		if _, err := parseDates(s); err != nil {
			t.Errorf("valid date rejected %s", s)
		}
	}
	for _, s := range []string{"2026-02-29", "2024-02-30", "2026-00-01", "2026-11-8", "../2026-11-08", "/2026-11-08", "file://2026-11-08", testDate + " ", testDate + "," + testDate, testDate + ","} {
		if _, err := parseDates(s); err == nil {
			t.Errorf("bad dates accepted %q", s)
		}
	}
	var dates []string
	for i := 1; i <= 31; i++ {
		dates = append(dates, fmt.Sprintf("2026-12-%02d", i))
	}
	if _, err := parseDates(strings.Join(dates, ",")); err != nil {
		t.Fatal(err)
	}
	if _, err := parseDates(strings.Join(append(dates, testDate), ",")); err == nil {
		t.Fatal("32 grants accepted")
	}
}

type spyStore struct {
	reads, appends int
	read           *scratchpad.SharedPage
	result         *scratchpad.AppendResult
	err            error
}

func (s *spyStore) ReadShared(string) (*scratchpad.SharedPage, error) {
	s.reads++
	return s.read, s.err
}
func (s *spyStore) AppendDetailed(string, string, string, []string) (*scratchpad.AppendResult, error) {
	s.appends++
	return s.result, s.err
}

func callParams(name string, a map[string]any) any {
	return map[string]any{"name": name, "arguments": a}
}
func appendArgs(bullets ...string) map[string]any {
	items := make([]any, len(bullets))
	for i, v := range bullets {
		items[i] = v
	}
	return map[string]any{"date": testDate, "section": "Updates", "source": "synthetic", "bullets": items}
}
func toolError(t *testing.T, result toolResult, code, outcome string) {
	t.Helper()
	if !result.IsError || result.Structured != nil || len(result.Content) != 1 {
		t.Fatalf("invalid tool error: %#v", result)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(result.Content[0].Text), &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 3 || m["code"] != code || m["write_outcome"] != outcome || m["message"] == "" {
		t.Fatalf("wrong fixed error: %v", m)
	}
}

func TestPrepareValidationAndGrants(t *testing.T) {
	cfg := config{read: map[string]bool{testDate: true}, append: map[string]bool{testDate: true}}
	for _, bullets := range [][]string{{"- one", "two", "one"}, {"$(touch never)", "[link](https://invalid.example)", "é", "é", "�"}} {
		if _, err := prepare(callParams("sp_shared_append", appendArgs(bullets...)), cfg, 1); err != nil {
			t.Fatal("literal valid data rejected")
		}
	}
	bad := []any{
		callParams("private_load", map[string]any{"date": testDate}),
		callParams("sp_shared_read", map[string]any{"date": testDate, "path": "private-canary"}),
		callParams("sp_shared_read", map[string]any{"Date": testDate}),
		callParams("sp_shared_read", map[string]any{"date": "2026-02-29"}),
		callParams("sp_shared_append", appendArgs()),
		callParams("sp_shared_append", appendArgs(strings.Repeat("a", 8195))),
		callParams("sp_shared_append", appendArgs(strings.Repeat("é", 4097))),
		callParams("sp_shared_append", appendArgs(strings.Repeat("a", 8192), strings.Repeat("b", 8192), strings.Repeat("c", 8192), strings.Repeat("d", 8192))),
	}
	for _, bullet := range []string{"", " leading", "trailing ", "a\nb", "a\r", "a\t", "a\x00", "a\u2028b", "a\u2029b", "# heading", "```", "~~~", "- - item", "* block", "+ block", string([]byte{255})} {
		bad = append(bad, callParams("sp_shared_append", appendArgs(bullet)))
	}
	for _, key := range []string{"section", "source"} {
		for _, value := range []any{nil, "", " x", "x\n", strings.Repeat("x", 501)} {
			a := appendArgs("x")
			a[key] = value
			bad = append(bad, callParams("sp_shared_append", a))
		}
	}
	for _, key := range []string{"path", "command", "root", "private", "_meta"} {
		a := appendArgs("x")
		a[key] = "CANARY"
		bad = append(bad, callParams("sp_shared_append", a))
	}
	many := make([]string, 101)
	for i := range many {
		many[i] = "x"
	}
	bad = append(bad, callParams("sp_shared_append", appendArgs(many...)))
	for i, p := range bad {
		if _, err := prepare(p, cfg, 1); err == nil {
			t.Errorf("bad input %d accepted", i)
		} else {
			toolError(t, *err, "INVALID_INPUT", "not_attempted")
		}
	}
	many = many[:100]
	if _, err := prepare(callParams("sp_shared_append", appendArgs(many...)), cfg, strings.Repeat("x", 128)); err != nil {
		t.Fatal("100 bullets rejected")
	}
	for _, op := range []string{"sp_shared_read", "sp_shared_append"} {
		args := map[string]any{"date": testDate}
		if op == "sp_shared_append" {
			args = appendArgs("x")
		}
		_, err := prepare(callParams(op, args), config{}, 1)
		if err == nil {
			t.Fatal("empty grants authorized")
		}
		toolError(t, *err, "DATE_NOT_ALLOWED", "not_attempted")
	}
	// Independent grants, frozen copied from startup lists, no date discovery.
	cfg.append = nil
	if _, err := prepare(callParams("sp_shared_append", appendArgs("x")), cfg, 1); err == nil {
		t.Fatal("read implied write")
	}
	cfg.read = nil
	cfg.append = map[string]bool{testDate: true}
	if _, err := prepare(callParams("sp_shared_read", map[string]any{"date": testDate}), cfg, 1); err == nil {
		t.Fatal("write implied read")
	}
}

func TestResultBoundsAndSanitization(t *testing.T) {
	for _, content := range []string{"", " \r\n\t", "é\r\n  exact\n", strings.Repeat("x", maxRead)} {
		s := &spyStore{read: &scratchpad.SharedPage{Exists: true, Content: content, HasPrivate: true, Legacy: true}}
		r := execute(s, toolCall{date: testDate})
		if r.IsError {
			t.Fatal("valid read rejected")
		}
		out := r.Structured.(readResult)
		if out.Content != content || !out.NotEncrypted || out.Provenance != provenance {
			t.Fatal("changed stored text or disclosure")
		}
		if strings.TrimSpace(content) == "" && out.Status != "no_shared" {
			t.Fatal("wrong empty status")
		}
		if r.Content[0].Text != string(mustJSON(t, out)) {
			t.Fatal("dual representations differ")
		}
		if _, ok := encodeResponse(1, r, nil); !ok {
			t.Fatal("read unexpectedly too large")
		}
	}
	s := &spyStore{read: &scratchpad.SharedPage{Exists: false}}
	if execute(s, toolCall{date: testDate}).Structured.(readResult).Status != "missing" {
		t.Fatal("wrong missing status")
	}
	s.read = &scratchpad.SharedPage{Exists: true, Content: strings.Repeat("x", maxRead+1)}
	toolError(t, execute(s, toolCall{date: testDate}), "OUTPUT_TOO_LARGE", "not_applicable")
	s.read = &scratchpad.SharedPage{Exists: true, Content: strings.Repeat("\x00", maxRead)}
	r := execute(s, toolCall{date: testDate})
	if _, ok := encodeResponse(1, r, nil); !ok {
		t.Fatal("worst-case bounded read should fit both representations")
	}
	if _, ok := encodeResponse(1, success(strings.Repeat("\x00", maxOutput)), nil); ok {
		t.Fatal("complete output bound not enforced")
	}
	s.err = fmt.Errorf("SECRET-PATH PRIVATE-CANARY raw diagnostic")
	toolError(t, execute(s, toolCall{date: testDate}), "STORE_UNAVAILABLE", "not_applicable")
	r = execute(s, toolCall{append: true, date: testDate})
	toolError(t, r, "APPEND_UNCONFIRMED", "unknown")
	b, _ := encodeResponse(1, r, nil)
	if bytes.Contains(b, []byte("CANARY")) || bytes.Contains(b, []byte("SECRET")) {
		t.Fatal("backend diagnostic reflected")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSchemasAndInitialization(t *testing.T) {
	p := map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "synthetic", "version": "1"}}
	if _, ok := initialize(p); !ok {
		t.Fatal("init rejected")
	}
	for _, version := range []string{"2024-11-05", "2025-03-26", "", ProtocolVersion + " "} {
		p["protocolVersion"] = version
		if _, ok := initialize(p); ok {
			t.Fatal("version downgrade")
		}
	}
	p["protocolVersion"] = ProtocolVersion
	p["capabilities"] = map[string]any{"roots": map[string]any{}}
	if _, ok := initialize(p); ok {
		t.Fatal("unsupported capability accepted")
	}
	list := toolsList().(map[string]any)["tools"].([]any)
	if len(list) != 2 {
		t.Fatal("tools not static")
	}
	for i, v := range list {
		m := v.(map[string]any)
		for _, key := range []string{"inputSchema", "outputSchema"} {
			schema := m[key].(map[string]any)
			if schema["additionalProperties"] != false {
				t.Fatal("open schema")
			}
		}
		ann := m["annotations"].(map[string]any)
		if ann["openWorldHint"] != false || ann["idempotentHint"] != (i == 0) {
			t.Fatal("unsafe annotations")
		}
	}
	b, _ := encodeResponse(strings.Repeat("\x00", 128), success(appendResult{testDate, strings.Repeat("é", 100), strings.Repeat("é", 250), []string{strings.Repeat("\\", 8192)}, []string{}, true, provenance}), nil)
	if len(b) == 0 || !json.Valid(bytes.TrimSpace(b)) {
		t.Fatal("invalid response")
	}
	if !reflect.DeepEqual(toolsList(), toolsList()) {
		t.Fatal("dynamic tools")
	}
}
