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
	// A fixed two-row shell keeps feedback from moving the document. The
	// quiet rule becomes an outcome row when there is something to report.
	heading := palette.Header.Render("sp") + palette.MutedText.Render(" / ") + palette.Title.Render(oneLine(title))
	second := palette.Separator.Render(strings.Repeat("─", max(0, width)))
	if status != "" {
		second = statusStyle(status, palette).Render(truncate(oneLine(safeMarkdownDisplay(status)), width))
	}
	return truncate(heading, width) + "\n" + second
}

// screenFooter keeps secondary shortcuts in help instead of filling the page
// with instructions. Callers still own which actions are available in context.
func screenFooter(entries []helpEntry, palette Palette, width int) string {
	return palette.Separator.Render(strings.Repeat("─", max(0, width))) + "\n" + palette.Help.Render(wrappedHelp(entries, width))
}

// screenView is the common, bounded shell for forms and secondary views.
// The body is already projected for display; no stored/input bytes change here.
func screenView(title, status, body, footer string, palette Palette, width, height int) string {
	header := viewHeader(title, status, palette, width)
	minimum := viewHeight(header) + viewHeight(footer) + 1
	if width < 24 || height < minimum {
		return smallTerminal(title, width, height, 24, minimum, "F1: help · Esc: return")
	}
	bodyHeight := height - viewHeight(header) - viewHeight(footer)
	return fitView(lipgloss.JoinVertical(lipgloss.Left, header,
		lipgloss.NewStyle().Height(bodyHeight).Render(fitView(body, width, bodyHeight)), footer), width, height)
}

// Metadata is not note content. Error words reinforce color so outcomes remain
// understandable in monochrome terminals and without color perception.
func statusStyle(status string, palette Palette) lipgloss.Style {
	lower := strings.ToLower(status)
	switch {
	case strings.Contains(lower, "failed"), strings.Contains(lower, "unavailable"), strings.Contains(lower, "cannot "), strings.Contains(lower, "not saved"), strings.Contains(lower, "rejected"):
		return palette.ErrorMessage
	case strings.Contains(lower, "unconfirmed"), strings.Contains(lower, "unsaved"), strings.Contains(lower, "plaintext"), strings.Contains(lower, "cancel"):
		return lipgloss.NewStyle().Foreground(palette.Highlight)
	default:
		return palette.MutedText
	}
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
