package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
)

func TestDesignHeaderKeepsDividerAndAlignsNoticeWithDocument(t *testing.T) {
	p := lightPalette()
	for _, status := range []string{"", "No page for today; opened the newest earlier page."} {
		plain := ansi.Strip(viewHeader("Notebook · 2026-02-14", status, p, 80))
		lines := strings.Split(plain, "\n")
		if len(lines) != viewHeaderRows || lines[2] != strings.Repeat("─", 80) {
			t.Fatalf("notice displaced the document boundary: %q", plain)
		}
		if !strings.HasPrefix(lines[0], "  sp / ") || (status != "" && !strings.HasPrefix(lines[1], "  No page")) {
			t.Fatal("header and notice do not share the document gutter")
		}
	}
	footer := ansi.Strip(screenFooter([]helpEntry{{keys: "Esc", label: "return", visible: true}}, p, 80))
	if !strings.HasPrefix(strings.Split(footer, "\n")[1], "  Esc:") {
		t.Fatal("footer text uses a different gutter")
	}
}

func TestNotebookStartupNoticeDoesNotRunIntoWriting(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "light")
	const date = "2026-02-14"
	const original = "# Synthetic project\n\nGoal: keep the layout readable.\n\n- First item\n- Second item\n"
	app := workflowApp(t, ModeNotebook, map[string]string{date: original})
	app.nb.theme.SetStatus("No page for today; opened the newest earlier page.", 0)
	view := ansi.Strip(app.View())
	lines := strings.Split(view, "\n")
	if strings.Trim(lines[2], "─") != "" || !strings.HasPrefix(lines[0], "  sp / ") || !strings.HasPrefix(lines[1], "  No page") || !strings.HasPrefix(lines[3], "  Synthetic project") {
		t.Fatalf("startup chrome merges into writing: %q", view)
	}
	before := app.nb.viewport.Height
	app.nb.theme.SetStatus("", 0)
	app.nb.View()
	if app.nb.viewport.Height != before || app.nb.contents[date] != original {
		t.Fatal("notice changed the document budget or stored text")
	}
}

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
