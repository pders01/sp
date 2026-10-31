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

// WarningCode identifies a recoverable configuration problem.
type WarningCode string

const (
	WarningUnknownKey   WarningCode = "unknown_key"
	WarningInvalidValue WarningCode = "invalid_value"
)

// Warning describes an ignored key or a setting replaced with a safe default.
// Path is the TOML key path, not the configuration filename. Message is suitable
// for display; callers choose where and when to present it.
type Warning struct {
	Code    WarningCode
	Path    string
	Message string
}

// Load is the backwards-compatible wrapper around LoadWithDiagnostics. It
// discards warnings; interactive callers should use LoadWithDiagnostics instead.
func Load(path string) (*Config, error) {
	cfg, _, err := LoadWithDiagnostics(path)
	return cfg, err
}

// LoadWithDiagnostics reads path and merges its contents on top of Default().
// A missing file returns defaults without warnings. Unknown keys are ignored,
// and invalid UI values use defaults, with warnings for both. Read errors,
// malformed TOML, and type errors outside UI settings return a nil config and
// an error. Loading never executes template commands or enables them implicitly.
func LoadWithDiagnostics(path string) (*Config, []Warning, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	// Decode UI values without enforcing their types so a mistyped appearance
	// preference cannot prevent access to notes. Keep template decoding strict,
	// in particular the explicit boolean trust opt-in for commands.
	var decoded struct {
		UI struct {
			Icons any `toml:"icons"`
			Theme any `toml:"theme"`
		} `toml:"ui"`
		Templates TemplatesConfig `toml:"templates"`
	}
	metadata, err := toml.Decode(string(data), &decoded)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var warnings []Warning
	for _, key := range metadata.Undecoded() {
		// A table supplied in place of a UI scalar is covered by that
		// setting's invalid-value warning, not separate unknown-key warnings.
		if len(key) > 2 && key[0] == "ui" && (key[1] == "icons" || key[1] == "theme") {
			continue
		}
		warnings = append(warnings, Warning{
			Code:    WarningUnknownKey,
			Path:    key.String(),
			Message: fmt.Sprintf("unknown configuration key %s; ignored", key.String()),
		})
	}
	cfg.UI.Icons = normalizeUIValue("ui.icons", decoded.UI.Icons, cfg.UI.Icons, []string{"unicode", "nerd"}, &warnings)
	cfg.UI.Theme = normalizeUIValue("ui.theme", decoded.UI.Theme, cfg.UI.Theme, []string{"auto", "light", "dark"}, &warnings)
	cfg.Templates = decoded.Templates
	for i := range cfg.Templates.Items {
		file := cfg.Templates.Items[i].File
		if file != "" && file != "~" && !filepath.IsAbs(file) && !strings.HasPrefix(file, "~/") {
			cfg.Templates.Items[i].File = filepath.Join(filepath.Dir(path), file)
		}
	}
	return cfg, warnings, nil
}

func normalizeUIValue(path string, value any, fallback string, allowed []string, warnings *[]Warning) string {
	if value == nil {
		return fallback
	}
	if text, ok := value.(string); ok {
		for _, candidate := range allowed {
			if text == candidate {
				return text
			}
		}
	}
	*warnings = append(*warnings, Warning{
		Code: WarningInvalidValue,
		Path: path,
		Message: fmt.Sprintf("invalid value for %s: %#v; expected %s; using %q",
			path, value, strings.Join(allowed, ", "), fallback),
	})
	return fallback
}
