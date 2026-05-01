package tui

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/charmbracelet/glamour/styles"
	"golang.org/x/term"
)

// Theme preference values accepted by config.UI.Theme and the runtime
// toggle key. Stored as strings so they round-trip through TOML.
const (
	ThemePrefAuto  = "auto"
	ThemePrefLight = "light"
	ThemePrefDark  = "dark"
)

// resolveGlamourStyle picks the glamour style ("dark"/"light") for a
// given user preference. Resolution order:
//
//  1. "light" / "dark" — explicit override, return immediately.
//  2. "auto" (or empty / unknown) — fall through to detection:
//     a. GLAMOUR_STYLE env var.
//     b. COLORFGBG env var, when set by the terminal (xterm/rxvt
//     convention: trailing field 0–6 or 8 = dark, others = light).
//     c. Non-TTY → NoTTYStyle (renders without ANSI).
//     d. macOS only: read AppleInterfaceStyle from defaults; the key
//     exists only when Dark Appearance is active.
//     e. Default to dark (matches termenv's post-timeout fallback).
//
// COLORFGBG beats the TTY check so terminals that set it win even when
// stdout is not a tty (integration tests, ssh tunnels). OSC 11 probes
// are intentionally avoided because they can block startup for up to
// 5 s on terminals that never reply; the macOS plist watcher handles
// dynamic detection there.
func resolveGlamourStyle(pref string) string {
	switch strings.ToLower(strings.TrimSpace(pref)) {
	case ThemePrefLight:
		return styles.LightStyle
	case ThemePrefDark:
		return styles.DarkStyle
	}

	if s := os.Getenv("GLAMOUR_STYLE"); s != "" {
		return s
	}
	if fgbg := os.Getenv("COLORFGBG"); strings.Contains(fgbg, ";") {
		parts := strings.Split(fgbg, ";")
		if bg, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			if bg < 7 || bg == 8 {
				return styles.DarkStyle
			}
			return styles.LightStyle
		}
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return styles.NoTTYStyle
	}
	if runtime.GOOS == "darwin" {
		if isMacOSDarkAppearance() {
			return styles.DarkStyle
		}
		return styles.LightStyle
	}
	return styles.DarkStyle
}

// isMacOSDarkAppearance returns true if AppleInterfaceStyle is set to
// "Dark" in the user's global defaults. The defaults binary exits 1
// when the key is absent (which is the macOS light-mode signal), so a
// non-zero exit is treated as "not dark" rather than an error.
func isMacOSDarkAppearance() bool {
	cmd := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(string(out)), "Dark")
}

// nextThemePref cycles auto → light → dark → auto.
func nextThemePref(cur string) string {
	switch strings.ToLower(strings.TrimSpace(cur)) {
	case ThemePrefAuto:
		return ThemePrefLight
	case ThemePrefLight:
		return ThemePrefDark
	default:
		return ThemePrefAuto
	}
}

// MsgThemeApplied describes a glamour style swap. style is the resolved
// glamour style; pref is the user-facing preference label.
func MsgThemeApplied(pref, style string) string {
	pref = strings.TrimSpace(pref)
	style = strings.TrimSpace(style)
	if pref == "" {
		pref = "auto"
	}
	if pref == style || pref == "auto" {
		return "Theme: " + pref + " (" + style + ")"
	}
	return "Theme: " + pref
}
