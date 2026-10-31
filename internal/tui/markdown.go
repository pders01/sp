package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
)

// documentStyle is a value copy: never mutate Glamour's process-wide presets.
// Heading levels remain meaningful without importing an unrelated purple/blue
// visual identity into the notebook. Rendering never changes stored Markdown.
func documentStyle(style string) glamouransi.StyleConfig {
	config := styles.DarkStyleConfig
	if style == styles.LightStyle {
		config = styles.LightStyleConfig
	}
	if style == styles.NoTTYStyle {
		config = styles.NoTTYStyleConfig
		margin := uint(2)
		config.Document.Margin = &margin
		config.Document.BlockPrefix, config.Document.BlockSuffix = "", ""
		for _, heading := range []*glamouransi.StyleBlock{&config.H1, &config.H2, &config.H3, &config.H4, &config.H5, &config.H6} {
			heading.Prefix, heading.Suffix = "", ""
		}
		return config // Keep plain text genuinely ANSI-free outside a terminal.
	}
	palette := paletteFor(style)
	text, accent, muted := string(palette.Text), string(palette.Secondary), string(palette.Muted)
	bold, margin := true, uint(2)
	config.Document.Color, config.Document.Margin = &text, &margin
	config.Document.BlockPrefix, config.Document.BlockSuffix = "", ""
	config.Heading.Color, config.Heading.Bold = &accent, &bold
	config.Heading.BackgroundColor = nil
	for _, heading := range []*glamouransi.StyleBlock{&config.H1, &config.H2, &config.H3, &config.H4, &config.H5, &config.H6} {
		heading.Prefix, heading.Suffix = "", ""
		heading.Color, heading.BackgroundColor, heading.Bold = &accent, nil, &bold
	}
	config.Link.Color, config.LinkText.Color = &accent, &accent
	config.Code.Color, config.Code.BackgroundColor = &accent, nil
	config.Code.Prefix, config.Code.Suffix = "`", "`"
	config.CodeBlock.Chroma = nil
	config.CodeBlock.Color = &text
	config.HorizontalRule.Color = &muted
	return config
}

// renderDocument returns one independently rendered, terminal-safe document.
// Never concatenate private and shared source before calling it.
func renderDocument(text, style string, width int) string {
	text = safeMarkdownDisplay(text)
	renderer, err := glamour.NewTermRenderer(glamour.WithStyles(documentStyle(style)), glamour.WithWordWrap(max(width-4, 1)))
	if err == nil {
		if rendered, renderErr := renderer.Render(text); renderErr == nil {
			text = rendered
		}
	}
	return strings.TrimRight(ansi.Wrap(displayText(text), max(width, 1), ""), "\n")
}
