package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the top-level user configuration loaded from
// ~/.sp/config.toml. Missing fields fall back to defaults so a fresh
// install works without writing a file.
type Config struct {
	UI UIConfig `toml:"ui"`
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
	if cfg.UI.Icons == "" {
		cfg.UI.Icons = "unicode"
	}
	if cfg.UI.Theme == "" {
		cfg.UI.Theme = "auto"
	}
	return cfg, nil
}
