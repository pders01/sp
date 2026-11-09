package sharedmcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/pders01/sp/internal/scratchpad"
)

// Direct JSON construction is confined to synthetic fixtures, never production.
func fixture(t *testing.T, store, name string, v any) []byte {
	t.Helper()
	b := mustJSON(t, v)
	if err := os.WriteFile(filepath.Join(store, name), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return b
}
func sharedFixture(content string) map[string]any {
	return map[string]any{"version": 2, "date": testDate, "agent": map[string]any{"content": content, "contributions": []any{}, "modified": "2026-01-01T00:00:00Z"}}
}
func legacyFixture() map[string]any {
	return map[string]any{"date": testDate, "content": "PRIVATE-CANARY $(never)", "created": "2026-01-01T00:00:00Z", "modified": "2026-01-02T00:00:00Z", "applied_templates": []string{"PRIVATE-TEMPLATE-CANARY"}, "agent": map[string]any{"content": " \r\n\t", "contributions": []any{}, "modified": "2026-01-03T00:00:00Z"}}
}
func readFixture(t *testing.T, store, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(store, name))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
func managerAt(t *testing.T, store string) *scratchpad.Manager {
	t.Helper()
	m, err := scratchpad.NewManagerAt(store)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Generates ephemeral test material only. Wrapped bytes intentionally are NOT
// an identity, and the daily encrypted body is corrupted after encryption. Shared
// operations must only validate outer structure; trying to decrypt would fail.
func encryptedFixture(t *testing.T, store, state string) (privacyJSON []byte, record map[string]any) {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	scrypt, err := age.NewScryptRecipient("synthetic disposable passphrase")
	if err != nil {
		t.Fatal(err)
	}
	scrypt.SetWorkFactor(1)
	var wrapped bytes.Buffer
	w, err := age.Encrypt(&wrapped, scrypt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("SYNTHETIC-NOT-AN-IDENTITY")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	metadata := map[string]any{"version": 1, "recipient": identity.Recipient().String(), "wrapped_identity": base64.StdEncoding.EncodeToString(wrapped.Bytes())}
	if state != "" {
		metadata["version"] = 2
		metadata["state"] = state
	}
	meta := fixture(t, store, "privacy.json", metadata)
	var private bytes.Buffer
	w, err = age.Encrypt(&private, identity.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("ENCRYPTED-PRIVATE-CANARY")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	payload := private.Bytes()
	payload[len(payload)-1] ^= 0xff
	r := sharedFixture("### Updates\n\n- old\n")
	r["private"] = map[string]any{"format": "age-x25519", "payload": base64.StdEncoding.EncodeToString(payload)}
	return meta, r
}

func TestSyntheticStoreExactRetryAndPrivatePreservation(t *testing.T) {
	for _, mode := range []string{"legacy", "enabled", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			store := t.TempDir()
			var before map[string]any
			var metadata []byte
			if mode == "legacy" {
				before = legacyFixture()
			} else {
				state := ""
				if mode == "disabled" {
					state = "disabled"
				}
				metadata, before = encryptedFixture(t, store, state)
				if mode == "disabled" {
					delete(before, "private")
				}
			}
			fixture(t, store, testDate+".json", before)
			m := managerAt(t, store) // Always locked; no private/key API in adapter tests.
			read := execute(m, toolCall{date: testDate})
			if read.IsError {
				t.Fatal("locked shared read failed")
			}
			if bytes.Contains(mustJSON(t, read), []byte("CANARY")) {
				t.Fatal("private canary disclosed")
			}
			call := toolCall{append: true, date: testDate, section: "Updates", source: "forged-system", bullets: []string{"- one", "two", "one", "$(touch never)", "é", "é", "�"}}
			r := execute(m, call)
			if r.IsError {
				t.Fatalf("append failed %v", r)
			}
			out := r.Structured.(appendResult)
			want := []string{"one", "two", "$(touch never)", "é", "é", "�"}
			if !reflect.DeepEqual(out.Added, want) || !reflect.DeepEqual(out.Duplicates, []string{"one"}) {
				t.Fatal("changed exact bullet order")
			}
			after := readFixture(t, store, testDate+".json")
			for _, field := range []string{"private", "content", "created", "modified", "applied_templates"} {
				if !bytes.Equal(mustJSON(t, before[field]), mustJSON(t, after[field])) {
					t.Errorf("private field %s changed", field)
				}
			}
			if metadata != nil {
				b, err := os.ReadFile(filepath.Join(store, "privacy.json"))
				if err != nil || !bytes.Equal(b, metadata) {
					t.Fatal("metadata changed")
				}
			}
			path := filepath.Join(store, testDate+".json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			// Source does not bypass dedup. Duplicate-only must not rewrite bytes,
			// timestamps or contribution history.
			call.source = "another-source"
			r = execute(m, call)
			out = r.Structured.(appendResult)
			if len(out.Added) != 0 || !reflect.DeepEqual(out.Duplicates, []string{"one", "two", "one", "$(touch never)", "é", "é", "�"}) {
				t.Fatal("retry contract changed")
			}
			again, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			newInfo, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, again) || !info.ModTime().Equal(newInfo.ModTime()) {
				t.Fatal("duplicate-only rewrote record")
			}
			if bytes.Contains(mustJSON(t, r), []byte("CANARY")) {
				t.Fatal("private data in append result")
			}
			// A separate section is a distinct dedup scope.
			call.section = "Other"
			call.bullets = []string{"one"}
			if len(execute(m, call).Structured.(appendResult).Added) != 1 {
				t.Fatal("cross-section dedup")
			}
		})
	}
}

func TestStoreFailurePreservationAndPolicy(t *testing.T) {
	for _, mode := range []string{"enabled-legacy", "decrypting", "disabled-ciphertext", "orphan", "malformed-metadata", "malformed-record", "unknown-format", "duplicate-heading", "unclosed-fence", "outside-policy-scan", "symlink-date"} {
		t.Run(mode, func(t *testing.T) {
			store := t.TempDir()
			record := sharedFixture("")
			switch mode {
			case "enabled-legacy":
				encryptedFixture(t, store, "")
				record = legacyFixture()
			case "decrypting":
				encryptedFixture(t, store, "decrypting")
			case "disabled-ciphertext":
				_, record = encryptedFixture(t, store, "disabled")
			case "orphan":
				_, record = encryptedFixture(t, store, "")
				if err := os.Remove(filepath.Join(store, "privacy.json")); err != nil {
					t.Fatal(err)
				}
			case "malformed-metadata":
				fixture(t, store, "privacy.json", map[string]any{"secret": "PRIVATE-CANARY"})
			case "malformed-record":
				record["unknown"] = "PRIVATE-CANARY"
			case "unknown-format":
				record["version"] = 99
			case "duplicate-heading":
				record = sharedFixture("### Updates\n\n### Updates\n")
			case "unclosed-fence":
				record = sharedFixture("```\n")
			case "outside-policy-scan":
				fixture(t, store, "2026-11-07.json", map[string]any{"version": 99, "private": "PRIVATE-CANARY"})
			}
			before := fixture(t, store, testDate+".json", record)
			if mode == "symlink-date" {
				if err := os.Rename(filepath.Join(store, testDate+".json"), filepath.Join(store, "synthetic-target")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(store, "synthetic-target"), filepath.Join(store, testDate+".json")); err != nil {
					t.Fatal(err)
				}
			}
			m := managerAt(t, store)
			r := execute(m, toolCall{append: true, date: testDate, section: "Updates", source: "synthetic", bullets: []string{"public"}})
			toolError(t, r, "APPEND_UNCONFIRMED", "unknown")
			after, err := os.ReadFile(filepath.Join(store, testDate+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("failed operation changed record")
			}
			if bytes.Contains(mustJSON(t, r), []byte("CANARY")) {
				t.Fatal("failure reflected private canary")
			}
		})
	}
}

func TestReadStatusesAndBoundsFromStore(t *testing.T) {
	store := t.TempDir()
	m := managerAt(t, store)
	if got := execute(m, toolCall{date: testDate}).Structured.(readResult).Status; got != "missing" {
		t.Fatal(got)
	}
	for _, content := range []string{" \t\r\n", "exact é\r\n", strings.Repeat("a", maxRead), strings.Repeat("a", maxRead+1)} {
		fixture(t, store, testDate+".json", sharedFixture(content))
		r := execute(m, toolCall{date: testDate})
		if len(content) > maxRead {
			toolError(t, r, "OUTPUT_TOO_LARGE", "not_applicable")
			continue
		}
		out := r.Structured.(readResult)
		if out.Content != content {
			t.Fatal("read normalized stored content")
		}
	}
	fixture(t, store, testDate+".json", legacyFixture())
	if out := execute(m, toolCall{date: testDate}).Structured.(readResult); out.Status != "no_shared" || out.Content != " \r\n\t" {
		t.Fatal("legacy private fallback or normalization")
	}
	if err := os.WriteFile(filepath.Join(store, testDate+".json"), []byte(`{"private":"PRIVATE-CANARY"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	toolError(t, execute(m, toolCall{date: testDate}), "STORE_UNAVAILABLE", "not_applicable")
}

func TestDeniedDatesNoTargetAccess(t *testing.T) {
	store := t.TempDir()
	fixture(t, store, testDate+".json", legacyFixture())
	fixture(t, store, "privacy.json", map[string]any{"invalid": "PRIVATE-CANARY"})
	before, err := os.ReadDir(store)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{store: store, read: map[string]bool{"2026-11-01": true}, append: map[string]bool{"2026-11-01": true}}
	spy := &spyStore{}
	var last []byte
	for _, date := range []string{testDate, "2026-11-09"} {
		for _, op := range []string{"sp_shared_read", "sp_shared_append"} {
			args := map[string]any{"date": date}
			if op == "sp_shared_append" {
				args = appendArgs("public")
				args["date"] = date
			}
			call, rejected := prepare(callParams(op, args), cfg, 1)
			if rejected == nil {
				execute(spy, call)
				t.Fatal("denied date dispatched")
			}
			b := mustJSON(t, rejected)
			if last != nil && !bytes.Equal(last, b) {
				t.Fatal("denial distinguishes existence or operation")
			}
			last = b
		}
	}
	if spy.reads+spy.appends != 0 {
		t.Fatal("denied target accessed")
	}
	after, err := os.ReadDir(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatal("denial created lock/record")
	}
}
