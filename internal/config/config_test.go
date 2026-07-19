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
	if cfg.Templates.AllowCommands {
		t.Error("template commands should be disabled by default")
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

[templates]
allow_commands = true

[[templates.items]]
id = "meeting"
name = "Meeting notes"
file = "~/.sp/templates/meeting.md"

[[templates.items]]
name = "Issues"
command = ["issue-template", "--markdown"]
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
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
	if !cfg.Templates.AllowCommands {
		t.Error("AllowCommands = false, want true")
	}
	if len(cfg.Templates.Items) != 2 {
		t.Fatalf("Templates = %+v", cfg.Templates.Items)
	}
	if cfg.Templates.Items[0].ID != "meeting" || cfg.Templates.Items[0].File == "" {
		t.Errorf("file template = %+v", cfg.Templates.Items[0])
	}
	if got := cfg.Templates.Items[1].Command; len(got) != 2 || got[1] != "--markdown" {
		t.Errorf("command template = %+v", cfg.Templates.Items[1])
	}
}

func TestLoadResolvesRelativeTemplateFilesFromConfigDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := "[[templates.items]]\nname = \"Notes\"\nfile = \"templates/notes.md\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "templates", "notes.md")
	if got := cfg.Templates.Items[0].File; got != want {
		t.Errorf("relative template file = %q, want %q", got, want)
	}
}

func TestLoadFillsBlankFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\n"), 0o644); err != nil {
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
	if err := os.WriteFile(path, []byte("not = = valid"), 0o644); err != nil {
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
