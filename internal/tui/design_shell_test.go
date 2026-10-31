package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
)

func TestDesignShellKeepsFooterWithinBudget(t *testing.T) {
	for _, size := range layoutSizes {
		p := darkPalette()
		footer := screenFooter([]helpEntry{{keys: "Esc", label: "return", visible: true}, {keys: "F1", label: "help", visible: true}}, p, size.Width)
		view := screenView("Form", "", strings.Repeat("body\n", 100), footer, p, size.Width, size.Height)
		assertFrameFits(t, view, size)
		if size.Width >= 24 && size.Height >= 8 && !strings.Contains(view, "Esc: return") {
			t.Fatal("oversized body hid required footer")
		}
	}
}

func TestDesignMarkdownUsesBrandWithoutMutatingPresets(t *testing.T) {
	oldPrefix, oldBackground := styles.DarkStyleConfig.H1.Prefix, *styles.DarkStyleConfig.H1.BackgroundColor
	for _, theme := range []string{styles.DarkStyle, styles.LightStyle} {
		cfg := documentStyle(theme)
		if cfg.H1.BackgroundColor != nil || cfg.H1.Prefix != "" || cfg.H2.Prefix != "" || *cfg.H1.Color != string(paletteFor(theme).Secondary) {
			t.Fatal("unrelated heading chrome remains")
		}
		text := "# Title\n\n## Subheading\n\nExact\ttabs\r\n\n```go\n" + strings.Repeat("long_identifier ", 30) + "\n```\n"
		view := renderDocument(text, theme, 56)
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > 56 {
				t.Fatal("markdown exceeds document width")
			}
		}
		if !strings.Contains(ansi.Strip(view), "Title") {
			t.Fatal("headings disappeared")
		}
	}
	if styles.DarkStyleConfig.H1.Prefix != oldPrefix || *styles.DarkStyleConfig.H1.BackgroundColor != oldBackground {
		t.Fatal("global preset changed")
	}
}
