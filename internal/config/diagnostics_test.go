package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadWithDiagnosticsPreservesValidSettings(t *testing.T) {
	for _, icons := range []string{"unicode", "nerd"} {
		for _, theme := range []string{"auto", "light", "dark"} {
			t.Run(icons+"/"+theme, func(t *testing.T) {
				path := writeTestConfig(t, fmt.Sprintf(`
[ui]
icons = %q
theme = %q
[templates]
allow_commands = true
[[templates.items]]
id = "meeting"
name = "Meeting notes"
file = "templates/meeting.md"
[[templates.items]]
id = "issues"
name = "Issues"
command = ["issue-template", "--markdown"]
`, icons, theme))
				cfg, warnings, err := LoadWithDiagnostics(path)
				if err != nil || len(warnings) != 0 {
					t.Fatalf("LoadWithDiagnostics: warnings=%+v, err=%v", warnings, err)
				}
				want := &Config{
					UI:     UIConfig{Icons: icons, Theme: theme},
					Editor: EditorConfig{Private: "builtin"},
					Templates: TemplatesConfig{
						AllowCommands: true,
						Items: []TemplateConfig{
							{ID: "meeting", Name: "Meeting notes", File: filepath.Join(filepath.Dir(path), "templates", "meeting.md")},
							{ID: "issues", Name: "Issues", Command: []string{"issue-template", "--markdown"}},
						},
					},
				}
				if !reflect.DeepEqual(cfg, want) {
					t.Errorf("config = %+v, want %+v", cfg, want)
				}
				legacy, err := Load(path)
				if err != nil || !reflect.DeepEqual(legacy, cfg) {
					t.Errorf("Load wrapper = %+v, %v; want %+v", legacy, err, cfg)
				}
			})
		}
	}
}

func TestLoadWithDiagnosticsUnknownPaths(t *testing.T) {
	path := writeTestConfig(t, `
extra = true
[ui]
icons = "nerd"
theme = "light"
theme_typo = "dark"
[ui.nested]
value = 1
[templates]
allow_command = true
[[templates.items]]
id = "notes"
file = "notes.md"
unknown = "ignored"
[other]
key = "ignored"
`)
	cfg, warnings, err := LoadWithDiagnostics(path)
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := map[string]bool{
		"extra": false, "ui.theme_typo": false, "ui.nested": false,
		"ui.nested.value": false, "templates.allow_command": false,
		"templates.items.unknown": false, "other": false, "other.key": false,
	}
	for _, warning := range warnings {
		if warning.Code != WarningUnknownKey {
			t.Errorf("warning = %+v, want unknown-key warning", warning)
		}
		if _, ok := wantPaths[warning.Path]; !ok {
			t.Errorf("unexpected warning: %+v", warning)
		}
		wantPaths[warning.Path] = true
		if !strings.Contains(warning.Message, warning.Path) || !strings.Contains(warning.Message, "ignored") {
			t.Errorf("warning is not actionable: %+v", warning)
		}
	}
	for path, seen := range wantPaths {
		if !seen {
			t.Errorf("no warning for %s", path)
		}
	}
	if cfg.UI != (UIConfig{Icons: "nerd", Theme: "light"}) || len(cfg.Templates.Items) != 1 {
		t.Errorf("valid settings lost: %+v", cfg)
	}
	if cfg.Templates.AllowCommands {
		t.Error("unknown trust key must not enable commands")
	}
}

func TestLoadWithDiagnosticsInvalidUIValues(t *testing.T) {
	for _, key := range []string{"icons", "theme"} {
		for _, value := range []string{`"typo"`, `""`, `42`, `true`, `["dark"]`, `{ name = "dark" }`} {
			t.Run(key+"/"+value, func(t *testing.T) {
				// Keep the other UI setting non-default to catch broad resets.
				otherKey, otherValue := "theme", "light"
				if key == "theme" {
					otherKey, otherValue = "icons", "nerd"
				}
				path := writeTestConfig(t, fmt.Sprintf(`[ui]
%s = %s
%s = %q
[[templates.items]]
id = "notes"
file = "notes.md"
`, key, value, otherKey, otherValue))
				cfg, warnings, err := LoadWithDiagnostics(path)
				if err != nil {
					t.Fatal(err)
				}
				if len(warnings) != 1 || warnings[0].Code != WarningInvalidValue || warnings[0].Path != "ui."+key {
					t.Fatalf("warnings = %+v, want one invalid ui.%s warning", warnings, key)
				}
				wantUI, fallback := UIConfig{Icons: "unicode", Theme: "light"}, "unicode"
				if key == "theme" {
					wantUI, fallback = UIConfig{Icons: "nerd", Theme: "auto"}, "auto"
				}
				if cfg.UI != wantUI {
					t.Errorf("UI = %+v, want %+v", cfg.UI, wantUI)
				}
				if !strings.Contains(warnings[0].Message, "using "+fmt.Sprintf("%q", fallback)) {
					t.Errorf("warning lacks fallback: %+v", warnings[0])
				}
				if len(cfg.Templates.Items) != 1 || cfg.Templates.Items[0].ID != "notes" || cfg.Templates.AllowCommands {
					t.Errorf("template settings changed: %+v", cfg.Templates)
				}
				legacy, err := Load(path)
				if err != nil || !reflect.DeepEqual(legacy, cfg) {
					t.Errorf("Load wrapper = %+v, %v; want %+v", legacy, err, cfg)
				}
			})
		}
	}
}

func TestLoadWithDiagnosticsDefaults(t *testing.T) {
	for _, body := range []string{"", "[ui]\n", "[templates]\nallow_commands = false\n"} {
		t.Run(body, func(t *testing.T) {
			cfg, warnings, err := LoadWithDiagnostics(writeTestConfig(t, body))
			if err != nil || len(warnings) != 0 || !reflect.DeepEqual(cfg, Default()) {
				t.Errorf("got %+v, %+v, %v; want defaults without warnings", cfg, warnings, err)
			}
		})
	}
	cfg, warnings, err := LoadWithDiagnostics(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil || len(warnings) != 0 || !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("missing file: got %+v, %+v, %v", cfg, warnings, err)
	}
}

func TestLoadWithDiagnosticsErrors(t *testing.T) {
	for _, body := range []string{
		"not = = valid",
		"[ui]\ntheme = \"dark\"\ntheme = \"light\"\n",
		"[templates]\nallow_commands = \"true\"\n",
		"[[templates.items]]\ncommand = 123\n",
	} {
		t.Run(body, func(t *testing.T) {
			path := writeTestConfig(t, body)
			cfg, warnings, err := LoadWithDiagnostics(path)
			if err == nil || cfg != nil || len(warnings) != 0 {
				t.Fatalf("got %+v, %+v, %v; want nil config and error", cfg, warnings, err)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error lacks filename: %v", err)
			}
		})
	}
	cfg, warnings, err := LoadWithDiagnostics(t.TempDir())
	if err == nil || cfg != nil || len(warnings) != 0 {
		t.Errorf("directory read: got %+v, %+v, %v; want error", cfg, warnings, err)
	}
}

func TestLoadWithDiagnosticsNeverExecutesCommands(t *testing.T) {
	for _, trust := range []string{"", "allow_commands = false", "allow_commands = true"} {
		t.Run(trust, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "command-ran")
			// If loading ever executes this command, the helper test below creates
			// a marker. The executable is the test binary, not an external tool.
			path := writeTestConfig(t, fmt.Sprintf(`[ui]
theme = "invalid"
[templates]
%s
[[templates.items]]
name = "Sentinel"
command = [%q, "-test.run=^TestConfigCommandSentinel$", "--", %q]
`, trust, os.Args[0], marker))
			cfg, warnings, err := LoadWithDiagnostics(path)
			if err != nil || len(warnings) != 1 {
				t.Fatalf("LoadWithDiagnostics = %+v, %+v, %v", cfg, warnings, err)
			}
			if cfg.Templates.AllowCommands != (trust == "allow_commands = true") {
				t.Errorf("command trust changed: %+v", cfg.Templates)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("command marker: %v; loading must not execute commands", err)
			}
		})
	}
}

func TestConfigCommandSentinel(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	if err := os.WriteFile(os.Args[len(os.Args)-1], []byte("command ran"), 0o600); err != nil {
		t.Fatal(err)
	}
}
