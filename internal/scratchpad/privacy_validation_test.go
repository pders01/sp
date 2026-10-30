package scratchpad

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Exercise a published-but-not-confirmed migration, then resume through a fresh
// manager with the cheap synthetic metadata from privateTestManager. This does
// not claim to simulate a crash or verify physical filesystem persistence.
func TestPrivacyValidationMigrationDurabilityResume(t *testing.T) {
	m := privateTestManager(t)
	const firstDate, secondDate, sharedDate = "2024-02-27", "2024-02-28", "2024-02-29"
	created := time.Date(2020, 1, 2, 3, 4, 5, 123456789, time.UTC)
	modified := created.Add(37*time.Hour + 17*time.Nanosecond)
	agent := AgentArea{
		Content:  "### Updates\r\n\r\n- shared α\r\n\tkeep spacing",
		Modified: modified,
		Contributions: []Contribution{{
			ID: "synthetic-contribution", Date: firstDate, Source: "synthetic agent", Section: "Updates",
			Bullets: []string{"shared α"}, Created: created,
		}},
	}
	first := legacyDocument{Version: 1, Date: firstDate, Content: "private α\r\n\tspacing\n\nno final newline", Created: created, Modified: modified, AppliedTemplates: []string{"α", "repeat", "repeat", ""}, Agent: &agent}
	second := legacyDocument{Date: secondDate, Content: "second private", Created: created, Modified: modified}
	firstPath := filepath.Join(m.storageDir, firstDate+".json")
	secondPath := filepath.Join(m.storageDir, secondDate+".json")
	writeJSON(t, firstPath, first)
	writeJSON(t, secondPath, second)
	if err := m.Append(sharedDate, "Updates", "synthetic agent", []string{"shared-only"}); err != nil {
		t.Fatal(err)
	}
	sharedPath := filepath.Join(m.storageDir, sharedDate+".json")
	sharedBefore := readTestFile(t, sharedPath)
	metadataBefore := readTestFile(t, filepath.Join(m.storageDir, "privacy.json"))
	secondBefore := readTestFile(t, secondPath)
	note, err := m.GetByDate(firstDate)
	if err != nil {
		t.Fatal(err)
	}
	syncErr := errors.New("synthetic migration sync failure")
	if saveErr := m.saveInternal(note, func(string) error { return syncErr }, true); !errors.Is(saveErr, ErrDurabilityUnconfirmed) || !errors.Is(saveErr, syncErr) {
		t.Fatalf("migration publication error = %v", saveErr)
	}
	published := readTestFile(t, firstPath)
	if bytes.Contains(published, []byte(first.Content)) || bytes.Equal(published, secondBefore) {
		t.Fatal("migration did not publish an encrypted first page")
	}
	if !bytes.Equal(secondBefore, readTestFile(t, secondPath)) {
		t.Fatal("one-page migration touched the remaining legacy page")
	}
	fresh, err := NewManagerAt(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, date := range []string{firstDate, secondDate, sharedDate, "2024-03-01"} {
		if _, readErr := fresh.GetByDate(date); !errors.Is(readErr, ErrLocked) {
			t.Errorf("fresh locked private read of %s = %v", date, readErr)
		}
	}
	shared, err := fresh.ReadShared(firstDate)
	if err != nil || shared.Content != agent.Content || !shared.HasPrivate || shared.Legacy {
		t.Fatalf("locked shared read = %+v, %v", shared, err)
	}
	status, err := fresh.PrivacyStatus()
	if err != nil || !status.Enabled || !reflect.DeepEqual(status.PlaintextDates, []string{secondDate}) || !reflect.DeepEqual(status.EncryptedDates, []string{firstDate}) || !reflect.DeepEqual(status.SharedOnlyDates, []string{sharedDate}) {
		t.Fatalf("interrupted migration status = %+v, %v", status, err)
	}
	if err := fresh.EnablePrivacy(fixturePassphrase); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(metadataBefore, readTestFile(t, filepath.Join(m.storageDir, "privacy.json"))) || !bytes.Equal(published, readTestFile(t, firstPath)) || !bytes.Equal(sharedBefore, readTestFile(t, sharedPath)) {
		t.Fatal("resume rotated identity or rewrote migrated/shared-only pages")
	}
	for _, want := range []legacyDocument{first, second} {
		got, err := fresh.GetByDate(want.Date)
		if err != nil {
			t.Fatal(err)
		}
		if got.Content != want.Content || !got.Created.Equal(want.Created) || !got.Modified.Equal(want.Modified) || !reflect.DeepEqual(got.AppliedTemplates, want.AppliedTemplates) {
			t.Fatalf("migration lost meaningful legacy fields for %s", want.Date)
		}
		if want.Agent != nil && !privacyFuzzEqualJSON(got.Agent, *want.Agent) {
			t.Fatal("migration changed shared content/provenance")
		}
	}
	beforeRepeat := readTestFile(t, firstPath)
	fresh.Lock()
	if err := fresh.Append(firstDate, "Updates", "different source", []string{"shared α"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeRepeat, readTestFile(t, firstPath)) {
		t.Fatal("locked repeat append changed ciphertext, formatting, or provenance")
	}
	assertNoSaveTemps(t, fresh)
}

func TestPrivacyValidationPublishedRecoveryRemainsReadable(t *testing.T) {
	m := privateTestManager(t)
	identity, err := m.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	const date = "2024-02-29"
	const content = "synthetic unsaved α\r\n\tspacing\nno final newline"
	plain, err := json.Marshal(privateDocument{Version: 2, Date: date, Content: content, Created: time.Unix(1, 2).UTC(), Modified: time.Unix(3, 4).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := encryptAge(plain, identity.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.storageDir, ".sp-recovery-"+date+"-"+strings.Repeat("0", 32)+".age")
	syncErr := errors.New("synthetic recovery sync failure")
	published, err := publish(path, ciphertext, true, func(string) error { return syncErr })
	if !published || !errors.Is(err, ErrDurabilityUnconfirmed) || !errors.Is(err, syncErr) {
		t.Fatalf("recovery publication = %v, %v", published, err)
	}
	if bytes.Contains(readTestFile(t, path), []byte(content)) {
		t.Fatal("published recovery contains plaintext")
	}
	got, err := m.ReadRecovery(path)
	if err != nil || got.Date != date || got.Content != content {
		t.Fatalf("published recovery read = %+v, %v", got, err)
	}
	before := readTestFile(t, path)
	if published, err := publish(path, []byte("must not replace"), true, func(string) error { t.Error("sync after failed exclusive publication"); return nil }); published || err == nil {
		t.Fatalf("exclusive recovery collision = %v, %v", published, err)
	}
	if !bytes.Equal(before, readTestFile(t, path)) {
		t.Fatal("exclusive publication replaced existing recovery")
	}
	assertNoSaveTemps(t, m)
}

// ReadRecovery and openRecord consume the same privateDocument format. Recovery
// must not silently turn missing/null content into a successful empty recovery.
func TestPrivacyValidationRecoveryRequiredFields(t *testing.T) {
	m := privateTestManager(t)
	identity, err := m.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	const date = "2024-02-29"
	path := filepath.Join(m.storageDir, ".sp-recovery-"+date+"-"+strings.Repeat("1", 32)+".age")
	base := map[string]json.RawMessage{
		"version": json.RawMessage(`2`), "date": json.RawMessage(`"2024-02-29"`),
		"content": json.RawMessage(`"synthetic recovery"`),
		"created": json.RawMessage(`"2024-02-29T00:00:00Z"`), "modified": json.RawMessage(`"2024-02-29T01:00:00Z"`),
	}
	for _, field := range []string{"version", "date", "content", "created", "modified"} {
		for _, mode := range []string{"missing", "null"} {
			t.Run(field+"/"+mode, func(t *testing.T) {
				fields := make(map[string]json.RawMessage, len(base))
				for key, value := range base {
					fields[key] = value
				}
				if mode == "missing" {
					delete(fields, field)
				} else {
					fields[field] = json.RawMessage(`null`)
				}
				plain, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				payload, err := encryptAge(plain, identity.Recipient())
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, payload, 0o600); err != nil {
					t.Fatal(err)
				}
				r := &diskRecord{Version: 2, Date: date, Private: &privateEnvelope{Format: privateFormat, Payload: base64.StdEncoding.EncodeToString(payload)}}
				if _, err := m.openRecord(r); err == nil {
					t.Fatal("daily private read accepted invalid fixture")
				}
				if got, err := m.ReadRecovery(path); err == nil {
					t.Errorf("recovery accepted %s %s: %+v", mode, field, got)
				}
				if !bytes.Equal(payload, readTestFile(t, path)) {
					t.Fatal("recovery read modified the source")
				}
			})
		}
	}
}

// encoding/json replaces lone escaped UTF-16 surrogates with U+FFFD without an
// error. Raw UTF-8 validation alone does not prevent a lossy migration.
func TestPrivacyValidationRejectsLossyUnicodeMigration(t *testing.T) {
	for _, field := range []string{"content", "applied_templates", "agent"} {
		t.Run(field, func(t *testing.T) {
			m := privateTestManager(t)
			const date = "2024-02-29"
			fields := map[string]json.RawMessage{"date": json.RawMessage(`"2024-02-29"`), "content": json.RawMessage(`"synthetic text"`)}
			switch field {
			case "content":
				fields[field] = json.RawMessage(`"before\ud800after"`)
			case "applied_templates":
				fields[field] = json.RawMessage(`["before\udc00after"]`)
			case "agent":
				fields[field] = json.RawMessage(`{"content":"before\ud800after"}`)
			}
			data, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(m.storageDir, date+".json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := m.EnablePrivacy(fixturePassphrase); err == nil {
				got, readErr := m.GetByDate(date)
				if readErr != nil {
					t.Fatal(readErr)
				}
				var transformed string
				switch field {
				case "content":
					transformed = got.Content
				case "applied_templates":
					transformed = strings.Join(got.AppliedTemplates, ",")
				case "agent":
					transformed = got.Agent.Content
				}
				t.Errorf("migration accepted unpaired surrogate; resulting %s = %q", field, transformed)
			}
			if !bytes.Equal(data, readTestFile(t, path)) {
				t.Error("migration rewrote a lossy legacy document instead of preserving its source")
			}
		})
	}
}

func TestPrivacyValidationCaseAliasedDateCannotPermitPlaintext(t *testing.T) {
	m := privateTestManager(t)
	const date = "2024-02-29"
	if saveErr := m.Save(&Scratchpad{Date: date, Content: "synthetic encrypted fixture"}); saveErr != nil {
		t.Fatal(saveErr)
	}
	if renameErr := os.Rename(filepath.Join(m.storageDir, date+".json"), filepath.Join(m.storageDir, date+".JSON")); renameErr != nil {
		t.Fatal(renameErr)
	}
	if removeErr := os.Remove(filepath.Join(m.storageDir, "privacy.json")); removeErr != nil {
		t.Fatal(removeErr)
	}
	fresh, err := NewManagerAt(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, policyErr := fresh.PrivacyEnabled(); policyErr == nil {
		t.Fatal("case-aliased date filename was ignored by policy checks")
	}
	if saveErr := fresh.Save(&Scratchpad{Date: "2024-03-01", Content: "synthetic private text"}); saveErr == nil {
		t.Fatal("case-aliased orphan ciphertext permitted new plaintext")
	}
	if _, statErr := os.Stat(filepath.Join(m.storageDir, "2024-03-01.json")); !os.IsNotExist(statErr) {
		t.Fatal("rejected write created a page")
	}
}

// Both the target and symlink are synthetic fixtures inside one temporary
// store. A date path readable by GetByDate must not be invisible to the orphan
// ciphertext check that decides whether new plaintext writes are safe.
func TestPrivacyValidationOrphanSymlinkCannotPermitPlaintext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires a Windows privilege not assumed by this test")
	}
	m := privateTestManager(t)
	const date = "2024-02-29"
	if err := m.Save(&Scratchpad{Date: date, Content: "synthetic encrypted fixture"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.storageDir, date+".json")
	fixture := filepath.Join(m.storageDir, "synthetic-page.fixture")
	if err := os.Rename(path, fixture); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(fixture), path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(m.storageDir, "privacy.json")); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewManagerAt(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.GetByDate(date); err == nil {
		t.Fatal("orphan ciphertext unexpectedly readable")
	}
	if enabled, err := fresh.PrivacyEnabled(); err == nil {
		t.Errorf("orphan ciphertext at symlink date was ignored: enabled=%v", enabled)
	}
	newPath := filepath.Join(m.storageDir, "2024-03-01.json")
	if err := fresh.Save(&Scratchpad{Date: "2024-03-01", Content: "synthetic new private content"}); err == nil {
		plaintext := bytes.Contains(readTestFile(t, newPath), []byte("synthetic new private content"))
		t.Errorf("orphan ciphertext at symlink date permitted a new save; plaintext visible=%v", plaintext)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Errorf("fail-closed save created a page: %v", err)
	}
}
