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

func TestSaveContentWithPrivacyPolicyPreservesExactBytesAndMetadata(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	note := &Scratchpad{
		Date:             "2026-01-01",
		Content:          "\t synthetic original\r\ne\u0301 \n\n",
		AppliedTemplates: []string{"synthetic-private-template", "second-template"},
		Created:          time.Date(2025, 12, 31, 12, 0, 0, 0, time.UTC),
	}
	if err := m.Save(note); err != nil {
		t.Fatal(err)
	}
	// Shared changes since review must be preserved by loading a fresh note.
	if err := m.Append(note.Date, "Updates", "synthetic-agent", []string{"shared synthetic update"}); err != nil {
		t.Fatal(err)
	}
	before, err := m.GetByDate(note.Date)
	if err != nil {
		t.Fatal(err)
	}
	original := note.Content
	for _, content := range []string{" \t synthetic reviewed\r\n\r\ne\u0301\t ", "", "\n\nsynthetic final\n\n"} {
		if err := m.SaveContentWithPrivacyPolicy(note.Date, original, content, true); err != nil {
			t.Fatal(err)
		}
		got, err := m.GetByDate(note.Date)
		if err != nil {
			t.Fatal(err)
		}
		if got.Content != content {
			t.Fatalf("content bytes = %q, want %q", got.Content, content)
		}
		if got.Date != before.Date || !got.Created.Equal(before.Created) ||
			!reflect.DeepEqual(got.AppliedTemplates, before.AppliedTemplates) || !reflect.DeepEqual(got.Agent, before.Agent) {
			t.Fatal("content save changed creation, template, or shared metadata")
		}
		data := readTestFile(t, filepath.Join(m.storageDir, note.Date+".json"))
		record, err := decodeRecord(data, note.Date)
		if err != nil {
			t.Fatal(err)
		}
		if record.Private == nil || record.legacy != nil {
			t.Fatal("reviewed content was not saved encrypted")
		}
		for _, secret := range []string{content, note.AppliedTemplates[0]} {
			if secret != "" && bytes.Contains(data, []byte(secret)) {
				t.Fatal("private content or template metadata disclosed in stored record")
			}
		}
		original = content
	}
}

func TestSaveContentWithPrivacyPolicyCreatesEncryptedDate(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	const date = "2026-01-02"
	const content = " \t synthetic new date\r\n\n"
	if err := m.SaveContentWithPrivacyPolicy(date, "", content, true); err != nil {
		t.Fatal(err)
	}
	note, err := m.GetByDate(date)
	if err != nil {
		t.Fatal(err)
	}
	if note.Content != content || note.disk.Private == nil {
		t.Fatal("missing date did not become an exact-byte encrypted note")
	}
}

func TestSaveContentWithPrivacyPolicyRejectsMismatchedOriginal(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	note := &Scratchpad{Date: "2026-01-01", Content: "synthetic original\r\ne\u0301 \n"}
	if err := m.Save(note); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.storageDir, note.Date+".json")
	before := readTestFile(t, path)
	for _, original := range []string{"stale synthetic original", "synthetic original\ne\u0301 \n", "synthetic original\r\n\u00e9 \n", "synthetic original\r\ne\u0301 \n\n"} {
		err := m.SaveContentWithPrivacyPolicy(note.Date, original, "must not replace", true)
		want := fmt.Sprintf("%s: %s; reload before retrying", ErrConflict, note.Date)
		if !errors.Is(err, ErrConflict) || err.Error() != want {
			t.Fatalf("mismatched original = %v, want %q wrapping ErrConflict", err, want)
		}
		if !bytes.Equal(before, readTestFile(t, path)) {
			t.Fatal("mismatched original changed stored note")
		}
	}
}

func TestSaveContentWithPrivacyPolicyRejectsChangedPolicy(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("disabled=%t", disabled), func(t *testing.T) {
			m := privateTestManager(t)
			defer m.Lock()
			note := &Scratchpad{Date: "2026-01-01", Content: "synthetic original"}
			if err := m.Save(note); err != nil {
				t.Fatal(err)
			}
			if disabled {
				if err := m.DisablePrivacy(fixturePassphrase); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(m.storageDir, note.Date+".json")
			before := readTestFile(t, path)
			// Invalid dates and stale originals also prove policy is checked before loading.
			for _, date := range []string{note.Date, "2026-01-02", "invalid-date"} {
				if err := m.SaveContentWithPrivacyPolicy(date, "stale original", "must remain private", disabled); !errors.Is(err, ErrPrivacyPolicyChanged) {
					t.Fatalf("changed policy for %q = %v", date, err)
				}
			}
			if !bytes.Equal(before, readTestFile(t, path)) {
				t.Fatal("changed policy replaced existing note")
			}
			if _, err := os.Stat(filepath.Join(m.storageDir, "2026-01-02.json")); !os.IsNotExist(err) {
				t.Fatalf("changed policy created absent date: %v", err)
			}
		})
	}
}

func TestSaveContentWithPrivacyPolicyRejectsLockedStore(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	note := &Scratchpad{Date: "2026-01-01", Content: "synthetic original"}
	if err := m.Save(note); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.storageDir, note.Date+".json")
	before := readTestFile(t, path)
	m.Lock()
	for _, date := range []string{note.Date, "2026-01-02"} {
		if err := m.SaveContentWithPrivacyPolicy(date, note.Content, "must stay private", true); !errors.Is(err, ErrLocked) {
			t.Fatalf("locked save for %q = %v", date, err)
		}
		if err := m.SaveContentWithPrivacyPolicy(date, note.Content, "must stay private", false); !errors.Is(err, ErrPrivacyPolicyChanged) {
			t.Fatalf("locked mismatched policy for %q = %v", date, err)
		}
	}
	if !bytes.Equal(before, readTestFile(t, path)) {
		t.Fatal("locked save changed existing note")
	}
	if _, err := os.Stat(filepath.Join(m.storageDir, "2026-01-02.json")); !os.IsNotExist(err) {
		t.Fatalf("locked save created absent date: %v", err)
	}
}
