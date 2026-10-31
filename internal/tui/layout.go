package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// truncate bounds one line by terminal cells without splitting ANSI sequences,
// wide glyphs, or grapheme clusters. No columns means no visible content.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

// fitView is a final rendering boundary, not a mutation of the underlying text.
func fitView(view string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(displayText(view), "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	return strings.Join(lines, "\n")
}

// Normalize terminal controls only in the display projection, never in a note
// or passphrase. Raw tabs otherwise evade display-width accounting.
func displayText(text string) string {
	return strings.NewReplacer("\r\n", "\n", "\r", "␍", "\t", "    ").Replace(text)
}

func oneLine(text string) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(text)
}

func viewHeader(title, status string, palette Palette, width int) string {
	// Reserve the status row so transient messages do not move the document.
	return palette.Header.Render(truncate(oneLine(title), width)) + "\n" +
		palette.MutedText.Render(truncate(oneLine(status), width))
}

func smallTerminal(title string, width, height, minWidth, minHeight int, controls string) string {
	return fitView(fmt.Sprintf("%s\nResize terminal: need %d×%d.\n%s\nCurrent: %d×%d.", title, minWidth, minHeight, controls, width, height), width, height)
}

func wrappedHelp(entries []helpEntry, width int) string {
	if width <= 0 {
		return ""
	}
	var lines []string
	line := ""
	for _, entry := range entries {
		if !entry.visible {
			continue
		}
		part := oneLine(entry.keys) + ": " + oneLine(entry.label)
		if line != "" && ansi.StringWidth(line)+3+ansi.StringWidth(part) > width {
			lines = append(lines, line)
			line = ""
		}
		if ansi.StringWidth(part) > width {
			lines = append(lines, strings.Split(ansi.Wrap(part, width, ""), "\n")...)
		} else if line == "" {
			line = part
		} else {
			line += " • " + part
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func viewHeight(view string) int {
	if view == "" {
		return 0
	}
	return lipgloss.Height(view)
}
