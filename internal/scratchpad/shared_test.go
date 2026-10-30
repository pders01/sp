package scratchpad

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAppendPreservesCiphertextAndConflictsWithPrivateSave(t *testing.T) {
	m := privateTestManager(t)
	note := &Scratchpad{Date: "2024-01-01", Content: "private secret", Created: time.Now(), AppliedTemplates: []string{"private-template"}}
	if err := m.Save(note); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.storageDir, note.Date+".json")
	r, err := decodeRecord(readTestFile(t, path), note.Date)
	if err != nil {
		t.Fatal(err)
	}
	originalEnvelope := *r.Private
	writer, err := NewManagerAt(m.storageDir)
	if err != nil {
		t.Fatal(err)
	}
	if appendErr := writer.Append(note.Date, "Updates", "agent", []string{"one", "- two", "one"}); appendErr != nil {
		t.Fatal(appendErr)
	}
	r, err = decodeRecord(readTestFile(t, path), note.Date)
	if err != nil {
		t.Fatal(err)
	}
	if *r.Private != originalEnvelope {
		t.Fatal("append changed private ciphertext")
	}
	if !strings.Contains(r.Agent.Content, "### Updates\n\n- one\n- two\n") || len(r.Agent.Contributions) != 1 || strings.Join(r.Agent.Contributions[0].Bullets, ",") != "one,two" || r.Agent.Modified.IsZero() {
		t.Fatalf("agent: %+v", r.Agent)
	}
	note.Content = "private edit"
	if saveErr := m.Save(note); !errors.Is(saveErr, ErrConflict) {
		t.Fatalf("stale private save: %v", saveErr)
	}
	note, err = m.GetByDate(note.Date)
	if err != nil {
		t.Fatal(err)
	}
	sharedBefore := note.Agent
	note.Content = "private edit"
	if saveErr := m.Save(note); saveErr != nil {
		t.Fatal(saveErr)
	}
	got, err := m.GetByDate(note.Date)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Content != sharedBefore.Content || len(got.Agent.Contributions) != 1 || !got.Agent.Modified.Equal(sharedBefore.Modified) {
		t.Fatal("private save lost shared area")
	}
	before := readTestFile(t, path)
	if err := writer.Append(note.Date, "Updates", "another-agent", []string{"two", "one"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, readTestFile(t, path)) {
		t.Fatal("dedup-only operation changed note/provenance")
	}
	// The whole file is a revision, including shared contributions.
	if err := writer.Append(note.Date, "Updates", "another-agent", []string{"three"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(got); !errors.Is(err, ErrConflict) {
		t.Fatalf("agent-only revision must conflict: %v", err)
	}
}

func TestConcurrentSharedAppends(t *testing.T) {
	m := privateTestManager(t)
	note := &Scratchpad{Date: "2024-01-01", Content: "confidential"}
	if err := m.Save(note); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.storageDir, note.Date+".json")
	r, err := decodeRecord(readTestFile(t, path), note.Date)
	if err != nil {
		t.Fatal(err)
	}
	private := *r.Private
	m.Lock()
	const count = 20
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			writer, managerErr := NewManagerAt(m.storageDir)
			if managerErr != nil {
				errs <- managerErr
				return
			}
			errs <- writer.Append(note.Date, "Concurrent", fmt.Sprintf("agent-%d", i), []string{fmt.Sprintf("item-%d", i), "common"})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	r, err = decodeRecord(readTestFile(t, path), note.Date)
	if err != nil {
		t.Fatal(err)
	}
	if *r.Private != private {
		t.Fatal("concurrent append changed private payload")
	}
	if len(r.Agent.Contributions) != count || strings.Count(r.Agent.Content, "- common\n") != 1 {
		t.Fatalf("concurrent updates lost/duplicated: %+v", r.Agent)
	}
	ids := map[string]bool{}
	for _, c := range r.Agent.Contributions {
		if ids[c.ID] {
			t.Fatal("duplicate operation id")
		}
		ids[c.ID] = true
	}
	for i := 0; i < count; i++ {
		if strings.Count(r.Agent.Content, fmt.Sprintf("- item-%d\n", i)) != 1 {
			t.Fatalf("missing item %d", i)
		}
	}
}

func TestSharedOnlyAndLegacyNonDisclosure(t *testing.T) {
	m := setupTestManager(t)
	note := &Scratchpad{Date: "2024-01-01", Content: "legacy private", AppliedTemplates: []string{"secret-id"}, Created: time.Now()}
	if err := m.Save(note); err != nil {
		t.Fatal(err)
	}
	privateModified := note.Modified
	if err := m.Append(note.Date, "Updates", "agent", []string{"public"}); err != nil {
		t.Fatal(err)
	}
	shared, err := m.ReadShared(note.Date)
	if err != nil || !shared.Legacy || !shared.HasPrivate || strings.Contains(shared.Content, "private") || !strings.Contains(shared.Content, "public") {
		t.Fatalf("legacy shared view: %+v %v", shared, err)
	}
	got, err := m.GetByDate(note.Date)
	if err != nil || got.Content != note.Content || !got.Modified.Equal(privateModified) || strings.Join(got.AppliedTemplates, ",") != "secret-id" {
		t.Fatalf("append damaged legacy private doc: %+v %v", got, err)
	}
	if appendErr := m.Append("2024-01-02", "Updates", "agent", []string{"shared only"}); appendErr != nil {
		t.Fatal(appendErr)
	}
	shared, err = m.ReadShared("2024-01-02")
	if err != nil || shared.HasPrivate || shared.Legacy {
		t.Fatalf("shared-only: %+v %v", shared, err)
	}
	status, err := m.PrivacyStatus()
	if err != nil || status.Enabled || strings.Join(status.SharedOnlyDates, ",") != "2024-01-02" {
		t.Fatalf("status: %+v %v", status, err)
	}
	// Reading legacy content remains prohibited after metadata enables privacy,
	// including while the manager holds an unlocked identity.
	p := privateTestManager(t)
	writeJSON(t, filepath.Join(p.storageDir, note.Date+".json"), legacyDocument{Date: note.Date, Content: "legacy private", Agent: &AgentArea{Content: "shared"}})
	shared, err = p.ReadShared(note.Date)
	if err != nil || shared.Content != "shared" {
		t.Fatalf("unlocked legacy shared: %+v %v", shared, err)
	}
	p.Lock()
	shared, err = p.ReadShared(note.Date)
	if err != nil || shared.Content != "shared" {
		t.Fatalf("locked legacy shared: %+v %v", shared, err)
	}
	if err := p.Append("2024-01-03", "Updates", "agent", []string{"new shared only"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ReadShared("2024-01-03"); err != nil {
		t.Fatal(err)
	}
}

func TestBulletSectionsFencesAndBoundaries(t *testing.T) {
	content := "# Intro\n- one\n\n### Updates\n\n```md\n- one\n### Not a heading\n```\n~~~\n- two\n~~~\n#### Details\n- existing\n\n### Other\n- one\n"
	got, added, err := appendBullets(content, "Updates", []string{"one", "two", "existing", "one"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(added, ",") != "one,two" {
		t.Fatalf("dedup includes fenced/outside bullets: %v", added)
	}
	if !strings.Contains(got, "- one\n- two\n\n### Other") {
		t.Fatalf("wrong boundary:\n%s", got)
	}
	otherIndex := strings.Index(content, "### Other")
	if otherIndex < 0 {
		t.Fatal("fixture is missing the Other heading")
	}
	if !strings.HasPrefix(got, content[:otherIndex]) {
		t.Fatal("append replaced existing content")
	}
	for _, text := range []string{"### Updates\n```\nunclosed", "### Updates\n\n### Updates\n"} {
		if _, _, appendErr := appendBullets(text, "Updates", []string{"one"}); appendErr == nil {
			t.Fatalf("ambiguous Markdown accepted: %q", text)
		}
	}
	got, _, err = appendBullets("### Updates", "Updates", []string{"one"})
	if err != nil || got != "### Updates\n- one\n" {
		t.Fatalf("heading without newline: %q %v", got, err)
	}
	got, added, err = appendBullets("### Updates\n    - one\n", "Updates", []string{"one"})
	if err != nil || len(added) != 1 {
		t.Fatalf("indented code dedup: %q %v", got, err)
	}
}

func TestAppendRejectsInjection(t *testing.T) {
	m := setupTestManager(t)
	for _, bullet := range []string{"", " one", "one ", "one\ntwo", "one\rtwo", "# Heading", "- - nested", "```", "~~~", "one\x00", "one\u2028two"} {
		if err := m.Append("2024-01-01", "Updates", "agent", []string{bullet}); err == nil {
			t.Errorf("accepted bullet %q", bullet)
		}
	}
	for _, section := range []string{"", "# Heading", "Updates\n### Inject", " Updates", "```", "x\tY"} {
		if err := m.Append("2024-01-01", section, "agent", []string{"one"}); err == nil {
			t.Errorf("accepted section %q", section)
		}
	}
	if err := m.Append("../outside", "Updates", "agent", []string{"one"}); err == nil {
		t.Fatal("invalid date accepted")
	}
	if err := m.Append("2024-01-01", "Updates", "bad\nsource", []string{"one"}); err == nil {
		t.Fatal("invalid source accepted")
	}
	if dates, err := m.ListDates(); err != nil || len(dates) != 0 {
		t.Fatalf("rejection created files: %v %v", dates, err)
	}
}

func TestUnsupportedDocumentsFailClosed(t *testing.T) {
	m := privateTestManager(t)
	identity, err := m.unlockedIdentity()
	if err != nil {
		t.Fatal(err)
	}
	date := "2024-01-01"
	path := filepath.Join(m.storageDir, date+".json")
	cases := []string{
		`{"date":"2024-01-01","version":99,"agent":{"content":"keep"}}`,
		`{"date":"2024-01-01","content":"secret","future":"keep"}`,
		`{"date":"2024-01-01","version":2,"agent":{"content":"keep"},"future":true}`,
		`{"date":"2024-01-01","version":2,"agent":{"content":"keep","future":true}}`,
		`{"date":"2024-01-01","version":2,"agent":{"content":"keep"},"private":{"format":"future","payload":"eA=="}}`,
		`{"date":"2024-01-01","version":2,"agent":{"content":"keep"},"private":{"format":"age-x25519","payload":"invalid"}}`,
		`{"date":"2024-01-01","version":2,"agent":null}`,
		`{"date":"2024-01-01","version":2,"agent":{"content":"keep"},"content":"must not leak"}`,
		`{"date":"2024-01-01","version":2,"version":1,"agent":{"content":"keep"}}`,
		`{"date":"2024-01-02","version":2,"agent":{"content":"keep"}}`,
		`{"date":"2024-01-01","version":2,"agent":{"content":"keep","contributions":[{"id":"x","date":"2024-01-01","source":"a","section":"s","bullets":["one"],"future":true}]}}`,
	}
	for i, text := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			for name, operation := range map[string]func() error{
				"private": func() error { _, err := m.GetByDate(date); return err },
				"shared":  func() error { _, err := m.ReadShared(date); return err },
				"status":  func() error { _, err := m.PrivacyStatus(); return err },
				"append":  func() error { return m.Append(date, "Updates", "agent", []string{"one"}) },
			} {
				if err := operation(); err == nil {
					t.Errorf("%s accepted invalid document", name)
				}
			}
			if !bytes.Equal([]byte(text), readTestFile(t, path)) {
				t.Fatal("invalid document was rewritten")
			}
		})
	}
	// Authenticated but semantically invalid inner documents must also fail.
	for _, inner := range []string{
		`{"version":2,"date":"2024-01-02","content":"swapped"}`,
		`{"version":99,"date":"2024-01-01","content":"unsupported"}`,
		`{"version":2,"date":"2024-01-01","content":"secret","future":"keep"}`,
	} {
		payload, err := encryptAge([]byte(inner), identity.Recipient())
		if err != nil {
			t.Fatal(err)
		}
		writeJSON(t, path, diskRecord{Version: 2, Date: date, Private: &privateEnvelope{Format: privateFormat, Payload: base64.StdEncoding.EncodeToString(payload)}})
		if _, err := m.GetByDate(date); err == nil {
			t.Fatal("invalid inner document accepted")
		}
		if _, err := m.PrivacyStatus(); err == nil {
			t.Fatal("invalid unlocked status accepted")
		}
	}
}

func TestSwappedCiphertextAndTruncation(t *testing.T) {
	m := privateTestManager(t)
	for _, date := range []string{"2024-01-01", "2024-01-02"} {
		if err := m.Save(&Scratchpad{Date: date, Content: "secret " + date}); err != nil {
			t.Fatal(err)
		}
	}
	path1, path2 := filepath.Join(m.storageDir, "2024-01-01.json"), filepath.Join(m.storageDir, "2024-01-02.json")
	r1, err := decodeRecord(readTestFile(t, path1), "2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := decodeRecord(readTestFile(t, path2), "2024-01-02")
	if err != nil {
		t.Fatal(err)
	}
	r2.Private = r1.Private
	writeJSON(t, path2, r2)
	if _, readErr := m.GetByDate("2024-01-02"); readErr == nil {
		t.Fatal("swapped ciphertext accepted")
	}
	payload, err := decodePayload(r1.Private.Payload, maxDocumentSize)
	if err != nil {
		t.Fatal(err)
	}
	r1.Private.Payload = base64.StdEncoding.EncodeToString(payload[:len(payload)-1])
	writeJSON(t, path1, r1)
	if _, err := m.GetByDate("2024-01-01"); err == nil {
		t.Fatal("truncated authenticated stream accepted")
	}
	// Shared access never attempts to decrypt an otherwise valid envelope.
	m.Lock()
	if _, err := m.ReadShared("2024-01-01"); err != nil {
		t.Fatalf("shared read decrypted private body: %v", err)
	}
}

func TestUnknownLegacySaveCannotDiscardFields(t *testing.T) {
	m := setupTestManager(t)
	data := []byte(`{"date":"2024-01-01","content":"secret","future":"preserve"}`)
	path := filepath.Join(m.storageDir, "2024-01-01.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Even forging a loaded revision must not bypass schema validation.
	note := &Scratchpad{Date: "2024-01-01", Content: "new", exists: true}
	note.revision = sha256.Sum256(data)
	if err := m.Save(note); err == nil {
		t.Fatal("save dropped unknown field")
	}
	if !bytes.Equal(data, readTestFile(t, path)) {
		t.Fatal("unknown field discarded")
	}
}
