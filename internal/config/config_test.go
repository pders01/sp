package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.UI.Icons != "unicode" {
		t.Errorf("Icons = %q, want %q", cfg.UI.Icons, "unicode")
	}
	if cfg.UI.Theme != "auto" {
		t.Errorf("Theme = %q, want %q", cfg.UI.Theme, "auto")
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.toml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load missing file: %v", err)
	}
	if cfg.UI.Theme != "auto" {
		t.Errorf("Theme = %q, want default %q", cfg.UI.Theme, "auto")
	}
}

func TestLoadParsesTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
[ui]
icons = "nerd"
theme = "dark"
`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.UI.Icons != "nerd" {
		t.Errorf("Icons = %q, want %q", cfg.UI.Icons, "nerd")
	}
	if cfg.UI.Theme != "dark" {
		t.Errorf("Theme = %q, want %q", cfg.UI.Theme, "dark")
	}
}

func TestLoadFillsBlankFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.UI.Icons != "unicode" || cfg.UI.Theme != "auto" {
		t.Errorf("blank ui section did not get defaults: %+v", cfg.UI)
	}
}

func TestLoadRejectsCorruptTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("not = = valid"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("expected parse error on corrupt TOML, got nil")
	}
}

func TestDefaultPath(t *testing.T) {
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if filepath.Base(path) != "config.toml" {
		t.Errorf("DefaultPath base = %q, want config.toml", filepath.Base(path))
	}
}
