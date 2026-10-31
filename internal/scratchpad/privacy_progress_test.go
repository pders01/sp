package scratchpad

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func privacyProgressCollector(events *[]PrivacyProgress) func(PrivacyProgress) {
	return func(p PrivacyProgress) { *events = append(*events, p) }
}

func assertPrivacyProgress(t *testing.T, got, want []PrivacyProgress) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("progress = %+v, want %+v", got, want)
	}
}

func TestPrivacyProgressEnableMixedStore(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	created := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	writeJSON(t, filepath.Join(m.storageDir, "2024-01-01.json"), legacyDocument{
		Date: "2024-01-01", Content: "synthetic legacy secret", Created: created,
	})
	if err := m.Save(&Scratchpad{Date: "2024-01-02", Content: "synthetic encrypted secret", Created: created}); err != nil {
		t.Fatal(err)
	}
	if err := m.Append("2024-01-03", "Updates", "synthetic-agent", []string{"shared"}); err != nil {
		t.Fatal(err)
	}
	unchanged := map[string][]byte{}
	for _, name := range []string{"privacy.json", "2024-01-02.json", "2024-01-03.json"} {
		unchanged[name] = readTestFile(t, filepath.Join(m.storageDir, name))
	}
	var events []PrivacyProgress
	if err := m.EnablePrivacyWithProgress(fixturePassphrase, privacyProgressCollector(&events)); err != nil {
		t.Fatal(err)
	}
	want := []PrivacyProgress{{"inspecting", 0, 0}, {"migrating", 0, 3}, {"migrating", 1, 3}, {"migrating", 2, 3}, {"migrating", 3, 3}, {"complete", 3, 3}}
	assertPrivacyProgress(t, events, want)
	for name, before := range unchanged {
		if !bytes.Equal(before, readTestFile(t, filepath.Join(m.storageDir, name))) {
			t.Fatalf("migration changed %s", name)
		}
	}
	note, err := m.GetByDate("2024-01-01")
	if err != nil || note.Content != "synthetic legacy secret" || !note.Created.Equal(created) {
		t.Fatalf("migration changed note: %+v, %v", note, err)
	}
	// A resumed, fully migrated store still counts every date, without writes.
	events = nil
	if err := m.EnablePrivacyWithProgress(fixturePassphrase, privacyProgressCollector(&events)); err != nil {
		t.Fatal(err)
	}
	assertPrivacyProgress(t, events, want)
}

func TestPrivacyProgressEnableDoesNotDecryptExistingArchive(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	if err := m.Save(&Scratchpad{Date: "2024-01-01", Content: "synthetic secret"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.storageDir, "2024-01-01.json")
	r, err := decodeRecord(readTestFile(t, path), "2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	// Valid envelope, unauthentic payload: migration historically only inspects
	// already encrypted records, rather than opening the entire archive again.
	r.Private.Payload = r.Private.Payload[:len(r.Private.Payload)-4] + "AAAA"
	writeJSON(t, path, r)
	before := readTestFile(t, path)
	var events []PrivacyProgress
	if err := m.EnablePrivacyWithProgress(fixturePassphrase, privacyProgressCollector(&events)); err != nil {
		t.Fatal(err)
	}
	assertPrivacyProgress(t, events, []PrivacyProgress{{"inspecting", 0, 0}, {"migrating", 0, 1}, {"migrating", 1, 1}, {"complete", 1, 1}})
	if !bytes.Equal(before, readTestFile(t, path)) {
		t.Fatal("migration rewrote existing ciphertext")
	}
	if _, err := m.GetByDate("2024-01-01"); err == nil {
		t.Fatal("fixture should fail authentication")
	}
}

func TestPrivacyProgressDisableMixedStore(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	if err := m.Save(&Scratchpad{Date: "2024-01-01", Content: "synthetic secret"}); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(m.storageDir, "2024-01-02.json"), legacyDocument{Date: "2024-01-02", Content: "legacy"})
	if err := m.Append("2024-01-03", "Updates", "synthetic-agent", []string{"shared"}); err != nil {
		t.Fatal(err)
	}
	before := readTestFile(t, filepath.Join(m.storageDir, "2024-01-01.json"))
	var events []PrivacyProgress
	if err := m.DisablePrivacyWithProgress(fixturePassphrase, func(p PrivacyProgress) {
		events = append(events, p)
		// These assertions run synchronously at the reported boundary, without
		// reentering the manager or acquiring any of its locks.
		if p.Stage == "verifying" || p.Stage == "decrypting" && p.Completed == 0 {
			if !bytes.Equal(before, readTestFile(t, filepath.Join(m.storageDir, "2024-01-01.json"))) {
				t.Fatal("plaintext published before preflight/transition completed")
			}
		}
		if p.Stage == "complete" {
			var metadata privacyMetadata
			if err := strictJSON(readTestFile(t, filepath.Join(m.storageDir, "privacy.json")), &metadata); err != nil || metadata.State != "disabled" {
				t.Fatalf("completion preceded final policy publication: %+v, %v", metadata, err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	assertPrivacyProgress(t, events, []PrivacyProgress{
		{"inspecting", 0, 0}, {"verifying", 0, 3}, {"verifying", 1, 3}, {"verifying", 2, 3}, {"verifying", 3, 3},
		{"decrypting", 0, 3}, {"decrypting", 1, 3}, {"decrypting", 2, 3}, {"decrypting", 3, 3}, {"complete", 3, 3},
	})
	note, err := m.GetByDate("2024-01-01")
	if err != nil || note.Content != "synthetic secret" {
		t.Fatalf("disable lost private content: %+v, %v", note, err)
	}
}

func TestPrivacyProgressEmptyAndNoop(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	var events []PrivacyProgress
	collect := privacyProgressCollector(&events)
	if err := m.EnablePrivacyWithProgress(fixturePassphrase, collect); err != nil {
		t.Fatal(err)
	}
	assertPrivacyProgress(t, events, []PrivacyProgress{{"inspecting", 0, 0}, {"migrating", 0, 0}, {"complete", 0, 0}})
	events = nil
	if err := m.DisablePrivacyWithProgress(fixturePassphrase, collect); err != nil {
		t.Fatal(err)
	}
	assertPrivacyProgress(t, events, []PrivacyProgress{{"inspecting", 0, 0}, {"verifying", 0, 0}, {"decrypting", 0, 0}, {"complete", 0, 0}})
	events = nil
	if err := m.DisablePrivacyWithProgress(nil, collect); err != nil {
		t.Fatal(err)
	}
	assertPrivacyProgress(t, events, []PrivacyProgress{{"inspecting", 0, 0}, {"complete", 0, 0}})
	fresh, err := NewManagerAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	events = nil
	if err := fresh.DisablePrivacyWithProgress(nil, collect); err != nil {
		t.Fatal(err)
	}
	assertPrivacyProgress(t, events, []PrivacyProgress{{"inspecting", 0, 0}, {"complete", 0, 0}})
	if err := fresh.DisablePrivacy(nil); err != nil {
		t.Fatal(err)
	}
	if err := m.EnablePrivacyWithProgress(fixturePassphrase, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.DisablePrivacyWithProgress(fixturePassphrase, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPrivacyProgressFailures(t *testing.T) {
	for _, operation := range []string{"enable", "disable"} {
		t.Run(operation, func(t *testing.T) {
			m := privateTestManager(t)
			defer m.Lock()
			var events []PrivacyProgress
			call := m.EnablePrivacyWithProgress
			if operation == "disable" {
				call = m.DisablePrivacyWithProgress
			}
			if err := call([]byte("wrong synthetic passphrase"), privacyProgressCollector(&events)); err == nil {
				t.Fatal("wrong passphrase accepted")
			}
			assertPrivacyProgress(t, events, []PrivacyProgress{{"inspecting", 0, 0}})
			// An invalid date document must not increment a success count or emit
			// completion, and its unknown field/error must never enter progress.
			path := filepath.Join(m.storageDir, "2024-01-01.json")
			if err := os.WriteFile(path, []byte(`{"date":"2024-01-01","secret-error-field":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			events = nil
			if err := call(fixturePassphrase, privacyProgressCollector(&events)); err == nil {
				t.Fatal("malformed document accepted")
			}
			stage := "migrating"
			if operation == "disable" {
				stage = "verifying"
			}
			assertPrivacyProgress(t, events, []PrivacyProgress{{"inspecting", 0, 0}, {stage, 0, 1}})
		})
	}
}

func TestPrivacyProgressDisableDurabilityAndResume(t *testing.T) {
	for failAt := 1; failAt <= 4; failAt++ {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			m := privateTestManager(t)
			defer m.Lock()
			for _, date := range []string{"2024-01-01", "2024-01-02"} {
				if err := m.Save(&Scratchpad{Date: date, Content: "synthetic secret"}); err != nil {
					t.Fatal(err)
				}
			}
			var events []PrivacyProgress
			calls := 0
			err := m.disablePrivacyWithProgress(fixturePassphrase, func(dir string) error {
				calls++
				if calls == failAt {
					return errors.New("synthetic sync failure")
				}
				return syncDirectory(dir)
			}, privacyProgressCollector(&events))
			if !errors.Is(err, ErrDurabilityUnconfirmed) {
				t.Fatalf("disable error = %v", err)
			}
			want := []PrivacyProgress{{"inspecting", 0, 0}, {"verifying", 0, 2}, {"verifying", 1, 2}, {"verifying", 2, 2}}
			if failAt > 1 {
				want = append(want, PrivacyProgress{"decrypting", 0, 2})
				for i := 1; i <= failAt-2; i++ {
					want = append(want, PrivacyProgress{"decrypting", i, 2})
				}
			}
			assertPrivacyProgress(t, events, want)
			events = nil
			if err := m.DisablePrivacyWithProgress(fixturePassphrase, privacyProgressCollector(&events)); err != nil {
				t.Fatalf("resume: %v", err)
			}
			if failAt == 4 {
				assertPrivacyProgress(t, events, []PrivacyProgress{{"inspecting", 0, 0}, {"complete", 0, 0}})
			} else {
				assertPrivacyProgress(t, events, []PrivacyProgress{{"inspecting", 0, 0}, {"verifying", 0, 2}, {"verifying", 1, 2}, {"verifying", 2, 2}, {"decrypting", 0, 2}, {"decrypting", 1, 2}, {"decrypting", 2, 2}, {"complete", 2, 2}})
			}
		})
	}
}

func TestPrivacyProgressNoopDurabilityFailure(t *testing.T) {
	m, err := NewManagerAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var events []PrivacyProgress
	err = m.disablePrivacyWithProgress(nil, func(string) error { return errors.New("synthetic sync failure") }, privacyProgressCollector(&events))
	if !errors.Is(err, ErrDurabilityUnconfirmed) {
		t.Fatalf("disable error = %v", err)
	}
	assertPrivacyProgress(t, events, []PrivacyProgress{{"inspecting", 0, 0}})
}

func TestPrivacyProgressShape(t *testing.T) {
	// Keep callbacks structurally incapable of carrying notes, dates, keys,
	// passphrases, or errors, not just free of those values in happy-path tests.
	typ := reflect.TypeOf(PrivacyProgress{})
	if typ.NumField() != 3 {
		t.Fatalf("unexpected progress payload: %v", typ)
	}
	for i, want := range []struct {
		name string
		kind reflect.Kind
	}{{"Stage", reflect.String}, {"Completed", reflect.Int}, {"Total", reflect.Int}} {
		field := typ.Field(i)
		if field.Name != want.name || field.Type.Kind() != want.kind || !field.IsExported() {
			t.Fatalf("unexpected progress field: %+v", field)
		}
	}
}
