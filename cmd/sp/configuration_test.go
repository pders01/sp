package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigurationWarningsPreserveSafeValidSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\ntheme='wrong'\nicons='nerd'\ntyop=true\n[templates]\nallow_commands=false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, warnings := loadConfiguration(path)
	if cfg.UI.Theme != "auto" || cfg.UI.Icons != "nerd" || cfg.Templates.AllowCommands || len(warnings) != 2 || !strings.Contains(strings.Join(warnings, " "), "ui.tyop") {
		t.Fatalf("config/warnings = %+v %q", cfg, warnings)
	}
	if err := os.WriteFile(path, []byte("invalid = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, warnings = loadConfiguration(path)
	if cfg.Templates.AllowCommands || len(warnings) != 1 || !strings.Contains(warnings[0], "safe defaults") {
		t.Fatal("syntax error did not retain access with safe defaults and a warning")
	}
}
