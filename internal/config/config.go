package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the top-level user configuration loaded from
// ~/.sp/config.toml. Missing fields fall back to defaults so a fresh
// install works without writing a file.
type Config struct {
	UI        UIConfig        `toml:"ui"`
	Templates TemplatesConfig `toml:"templates"`
}

// TemplatesConfig controls user-defined template sections. Executable
// templates require an explicit trust opt-in.
type TemplatesConfig struct {
	AllowCommands bool             `toml:"allow_commands"`
	Items         []TemplateConfig `toml:"items"`
}

// TemplateConfig adds an opt-in Markdown section to the day-template chooser.
// File and Command are mutually exclusive; Command is executed directly
// without a shell and its stdout is treated as Markdown.
type TemplateConfig struct {
	ID      string   `toml:"id"`
	Name    string   `toml:"name"`
	File    string   `toml:"file"`
	Command []string `toml:"command"`
}

// UIConfig holds preferences for the terminal interface.
type UIConfig struct {
	// Icons selects the glyph set: "nerd" assumes a Nerd Font is
	// installed, "unicode" uses geometric fallbacks. Default "unicode".
	Icons string `toml:"icons"`
	// Theme controls the glamour render style for the notebook view.
	// Accepted values:
	//   "auto"  — detect from terminal/OS (default)
	//   "light" — force light style
	//   "dark"  — force dark style
	Theme string `toml:"theme"`
}

// DefaultPath returns the canonical config path: ~/.sp/config.toml.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".sp", "config.toml"), nil
}

// Default returns a Config populated with built-in defaults.
func Default() *Config {
	return &Config{
		UI: UIConfig{
			Icons: "unicode",
			Theme: "auto",
		},
	}
}

// Load reads path and merges its contents on top of Default(). When the
// file is absent the defaults are returned without error so first-run
// users do not need to create a file.
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for i := range cfg.Templates.Items {
		file := cfg.Templates.Items[i].File
		if file != "" && file != "~" && !filepath.IsAbs(file) && !strings.HasPrefix(file, "~/") {
			cfg.Templates.Items[i].File = filepath.Join(filepath.Dir(path), file)
		}
	}
	if cfg.UI.Icons == "" {
		cfg.UI.Icons = "unicode"
	}
	if cfg.UI.Theme == "" {
		cfg.UI.Theme = "auto"
	}
	return cfg, nil
}
