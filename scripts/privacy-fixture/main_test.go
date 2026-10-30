package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixtureRefusesExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keep")
	if err := os.WriteFile(path, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(dir); err == nil {
		t.Fatal("fixture generator accepted existing directory")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "unchanged" {
		t.Fatal("fixture generator modified existing data")
	}
}
