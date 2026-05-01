package tui

import (
	"testing"

	"github.com/charmbracelet/glamour/styles"
)

func TestPaletteForLightVsDark(t *testing.T) {
	light := paletteFor(styles.LightStyle)
	dark := paletteFor(styles.DarkStyle)

	if light.Text == dark.Text {
		t.Errorf("light and dark Text should differ, both = %q", light.Text)
	}
	if light.Highlight == dark.Highlight {
		t.Errorf("light and dark Highlight should differ, both = %q", light.Highlight)
	}
	if light.CursorFg == dark.CursorFg {
		t.Errorf("light and dark CursorFg should differ, both = %q", light.CursorFg)
	}
}

func TestPaletteForUnknownFallsBackToDark(t *testing.T) {
	got := paletteFor("nonsense")
	dark := darkPalette()
	if got.Text != dark.Text {
		t.Errorf("unknown style should fall back to dark, got %q want %q", got.Text, dark.Text)
	}
}

func TestPaletteForNoTTYFallsBackToDark(t *testing.T) {
	got := paletteFor(styles.NoTTYStyle)
	dark := darkPalette()
	if got.Text != dark.Text {
		t.Errorf("NoTTY should fall back to dark, got %q want %q", got.Text, dark.Text)
	}
}

func TestPaletteStylesPopulated(t *testing.T) {
	for name, p := range map[string]Palette{
		"dark":  darkPalette(),
		"light": lightPalette(),
	} {
		if p.Header.GetForeground() == nil {
			t.Errorf("%s.Header missing foreground", name)
		}
		if p.MutedText.GetForeground() == nil {
			t.Errorf("%s.MutedText missing foreground", name)
		}
		if p.SelectedDate.GetForeground() == nil {
			t.Errorf("%s.SelectedDate missing foreground", name)
		}
	}
}

func TestThemeWatcher_PaletteSwapsOnPrefChange(t *testing.T) {
	w := newThemeWatcher(ThemePrefDark)
	dark := w.Palette()

	w.SetPref(ThemePrefLight)
	light := w.Palette()

	if dark.Text == light.Text {
		t.Errorf("palette did not swap after SetPref: dark.Text=%q light.Text=%q", dark.Text, light.Text)
	}
}
