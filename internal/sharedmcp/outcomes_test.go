package sharedmcp

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pders01/sp/internal/scratchpad"
)

type publishedFailureStore struct{ sharedStore }

func (s publishedFailureStore) AppendDetailed(date, section, source string, bullets []string) (*scratchpad.AppendResult, error) {
	if _, err := s.sharedStore.AppendDetailed(date, section, source, bullets); err != nil {
		return nil, err
	}
	// Synthetic adapter-level fault AFTER visible publication. This is not a
	// backend fsync fault injector or evidence of crash durability.
	return nil, errors.New("PRIVATE-CANARY simulated lost confirmation")
}

func TestPublishedButUnconfirmedRetry(t *testing.T) {
	store := t.TempDir()
	m := managerAt(t, store)
	call := toolCall{append: true, date: testDate, section: "Updates", source: "synthetic", bullets: []string{"one", "two", "one"}}
	toolError(t, execute(publishedFailureStore{m}, call), "APPEND_UNCONFIRMED", "unknown")
	path := filepath.Join(store, testDate+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("publication did not occur")
	}
	out := execute(m, call).Structured.(appendResult)
	if len(out.Added) != 0 || len(out.Duplicates) != 3 {
		t.Fatal("unconfirmed retry not exact dedup")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("retry rewrote unconfirmed publication")
	}
}

func TestBackendBoundsNeverBecomeMissingOrEmptySuccess(t *testing.T) {
	for _, name := range []string{testDate + ".json", "privacy.json"} {
		t.Run(name, func(t *testing.T) {
			store := t.TempDir()
			fixture(t, store, testDate+".json", sharedFixture("public"))
			path := filepath.Join(store, name)
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			size := int64((16 << 20) + 1)
			if name == "privacy.json" {
				size = (64 << 10) + 1
			}
			if err = f.Truncate(size); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			m := managerAt(t, store)
			toolError(t, execute(m, toolCall{date: testDate}), "STORE_UNAVAILABLE", "not_applicable")
			toolError(t, execute(m, toolCall{append: true, date: testDate, section: "Updates", source: "synthetic", bullets: []string{"public"}}), "APPEND_UNCONFIRMED", "unknown")
			info, err := os.Stat(path)
			if err != nil || info.Size() != size {
				t.Fatal("oversized fixture changed")
			}
		})
	}
}

func TestPrivateOnlyLegacyAndCaseAliasedPolicy(t *testing.T) {
	store := t.TempDir()
	record := legacyFixture()
	delete(record, "agent")
	fixture(t, store, testDate+".json", record)
	read := execute(managerAt(t, store), toolCall{date: testDate}).Structured.(readResult)
	if read.Status != "no_shared" || read.Content != "" {
		t.Fatal("private-only legacy fallback")
	}
	if err := os.Rename(filepath.Join(store, testDate+".json"), filepath.Join(store, testDate+".JSON")); err != nil {
		t.Fatal(err)
	}
	toolError(t, execute(managerAt(t, store), toolCall{append: true, date: testDate, section: "Updates", source: "synthetic", bullets: []string{"public"}}), "APPEND_UNCONFIRMED", "unknown")
}
