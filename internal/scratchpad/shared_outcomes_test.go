package scratchpad

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestAppendDetailedOutcomesAndProtectedPayload(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	const date = "2024-01-01"
	note := &Scratchpad{Date: date, Content: "synthetic private outcome sentinel", AppliedTemplates: []string{"private-template"}}
	if err := m.Save(note); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.storageDir, date+".json")
	before, operationErr := decodeRecord(readTestFile(t, path), date)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	envelope := *before.Private
	// Even an unlocked shared operation must not authenticate/decrypt the body.
	payload, operationErr := base64.StdEncoding.DecodeString(envelope.Payload)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	payload[len(payload)-1] ^= 1
	before.Private.Payload = base64.StdEncoding.EncodeToString(payload)
	writeJSON(t, path, before)
	envelope = *before.Private
	result, operationErr := m.AppendDetailed(date, "Updates", "synthetic-agent", []string{"- one", "two", "one"})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	if !reflect.DeepEqual(result.Added, []string{"one", "two"}) || !reflect.DeepEqual(result.Duplicates, []string{"one"}) {
		t.Fatalf("result = %+v", result)
	}
	after, operationErr := decodeRecord(readTestFile(t, path), date)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	if *after.Private != envelope || len(after.Agent.Contributions) != 1 || !reflect.DeepEqual(after.Agent.Contributions[0].Bullets, result.Added) {
		t.Fatal("detailed append changed protected payload or provenance")
	}
	if _, err := m.GetByDate(date); err == nil {
		t.Fatal("fixture did not corrupt authentication")
	}
	page, operationErr := m.ReadShared(date)
	if operationErr != nil || !page.Exists || !strings.Contains(page.Content, "one") {
		t.Fatalf("unlocked shared read = %+v, %v", page, operationErr)
	}
	m.Lock()
	data := readTestFile(t, path)
	result, operationErr = m.AppendDetailed(date, "Updates", "another-source", []string{"two", "- one", "two"})
	if operationErr != nil || result == nil || len(result.Added) != 0 || !reflect.DeepEqual(result.Duplicates, []string{"two", "one", "two"}) {
		t.Fatalf("duplicate result = %+v, %v", result, operationErr)
	}
	if !bytes.Equal(data, readTestFile(t, path)) {
		t.Fatal("duplicate-only detailed append rewrote bytes/provenance")
	}
	if m.IsUnlocked() {
		t.Fatal("detailed append unlocked private data")
	}
	result, operationErr = m.AppendDetailed(date, "Other", "agent", []string{"one"})
	if operationErr != nil || len(result.Added) != 1 || len(result.Duplicates) != 0 {
		t.Fatalf("section-scoped result = %+v, %v", result, operationErr)
	}
	result, operationErr = m.AppendDetailed(date, "Updates", "agent", []string{"one\ninjection"})
	if operationErr == nil || result != nil {
		t.Fatalf("invalid input result = %+v, %v", result, operationErr)
	}
}

func TestAppendDetailedPreservesPrivateConflictAndConcurrentDedup(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	note := &Scratchpad{Date: "2024-01-01", Content: "synthetic concurrent sentinel"}
	if err := m.Save(note); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.storageDir, note.Date+".json")
	before, operationErr2 := decodeRecord(readTestFile(t, path), note.Date)
	if operationErr2 != nil {
		t.Fatal(operationErr2)
	}
	const count = 12
	var wg sync.WaitGroup
	results := make(chan *AppendResult, count)
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			writer, err := NewManagerAt(m.storageDir)
			if err != nil {
				errs <- err
				return
			}
			result, err := writer.AppendDetailed(note.Date, "Concurrent", "agent", []string{fmt.Sprintf("item-%d", i), "common", "common"})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}(i)
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Fatal(err)
	}
	added, duplicates := 0, 0
	for result := range results {
		added += len(result.Added)
		duplicates += len(result.Duplicates)
	}
	if added != count+1 || duplicates != 2*count-1 {
		t.Fatalf("concurrent added=%d duplicates=%d", added, duplicates)
	}
	after, operationErr2 := decodeRecord(readTestFile(t, path), note.Date)
	if operationErr2 != nil {
		t.Fatal(operationErr2)
	}
	if *after.Private != *before.Private || len(after.Agent.Contributions) != count || strings.Count(after.Agent.Content, "- common\n") != 1 {
		t.Fatal("concurrent outcomes lost updates or changed payload")
	}
	note.Content = "stale private edit"
	if err := m.Save(note); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale private save = %v", err)
	}
}

func TestSharedPresenceAndIdentityMetadataOnly(t *testing.T) {
	m := setupTestManager(t)
	for _, tc := range []struct {
		date, content string
		exists        bool
	}{
		{"2024-01-01", "", false},
		{"2024-01-02", "", true},
		{"2024-01-03", "public", true},
	} {
		if tc.exists {
			writeJSON(t, filepath.Join(m.storageDir, tc.date+".json"), diskRecord{Version: 2, Date: tc.date, Agent: AgentArea{Content: tc.content}})
		}
		page, err := m.ReadShared(tc.date)
		if err != nil || page.Exists != tc.exists || page.Content != tc.content {
			t.Fatalf("presence = %+v, %v", page, err)
		}
	}
	if present, err := m.HasPrivacyIdentity(); err != nil || present {
		t.Fatalf("fresh identity = %v, %v", present, err)
	}
	// Identity inspection must not read daily documents or decrypt private data.
	p := privateTestManager(t)
	defer p.Lock()
	if err := os.WriteFile(filepath.Join(p.storageDir, "2024-01-01.json"), []byte("invalid document"), 0o600); err != nil {
		t.Fatal(err)
	}
	if present, err := p.HasPrivacyIdentity(); err != nil || !present {
		t.Fatalf("existing identity = %v, %v", present, err)
	}
	metadata, _, err := p.readMetadata()
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"disabled", "decrypting"} {
		metadata.Version, metadata.State = 2, state
		writeJSON(t, filepath.Join(p.storageDir, "privacy.json"), metadata)
		p.Lock()
		if present, err := p.HasPrivacyIdentity(); err != nil || !present || p.IsUnlocked() {
			t.Fatalf("%s identity = %v, %v", state, present, err)
		}
	}
	if err := os.WriteFile(filepath.Join(p.storageDir, "privacy.json"), []byte(`{"version":999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.HasPrivacyIdentity(); err == nil {
		t.Fatal("identity inspection accepted corrupt metadata")
	}
}
