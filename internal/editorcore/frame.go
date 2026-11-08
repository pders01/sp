package editorcore

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

const MaxColumns = 240
const MaxRows = 100
const MaxCellBytes = 256

// CellSpan is half-open DISPLAY CELLS, with whole-grapheme endpoints.
type CellSpan struct{ Row, Start, End int }

// Frame contains defensive display data only, never lifecycle authority.
// Span layers are individually sorted/nonoverlapping. They can overlap each
// other; the parent composes only fixed monochrome styles, never guest escapes.
type Frame struct {
	Width, Height, CursorRow, CursorColumn int
	Lines                                  []string
	Selections                             []CellSpan
	Matches                                []CellSpan
	StatusRow                              *int // Optional parent-notice slot, not a prompt/document row.
}

func ValidGeometry(width, height int) bool {
	return width > 0 && width <= MaxColumns && height > 0 && height <= MaxRows
}

func SafeDisplayText(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) ||
			(unicode.Is(unicode.Cf, r) && r != '\u200c' && r != '\u200d') ||
			(r >= 0xfdd0 && r <= 0xfdef) || r&0xffff == 0xfffe || r&0xffff == 0xffff {
			return false
		}
	}
	return true
}

func (f Frame) validSpans(spans []CellSpan) bool {
	if len(spans) > f.Width*f.Height {
		return false
	}
	for i, s := range spans {
		if s.Row < 0 || s.Row >= f.Height || s.Start < 0 || s.Start >= s.End || s.End > f.Width {
			return false
		}
		if i > 0 {
			p := spans[i-1]
			if s.Row < p.Row || (s.Row == p.Row && s.Start < p.End) {
				return false
			}
		}
	}
	return true
}

// StyledLines rejects the entire frame before exposing any rows. Selection,
// search underline and cursor composition are bounded sorted grapheme sweeps.
func (f Frame) StyledLines(showCursor bool) []string {
	if !ValidGeometry(f.Width, f.Height) || len(f.Lines) != f.Height || f.CursorRow < 0 || f.CursorRow >= f.Height || f.CursorColumn < 0 || f.CursorColumn >= f.Width {
		return nil
	}
	if f.StatusRow != nil && (*f.StatusRow < 0 || *f.StatusRow >= f.Height) {
		return nil
	}
	if !f.validSpans(f.Selections) || !f.validSpans(f.Matches) {
		return nil
	}
	lines := make([]string, f.Height)
	layers := [2][]CellSpan{f.Selections, f.Matches}
	indices := [2]int{}
	for row, line := range f.Lines {
		if len(line) > f.Width*MaxCellBytes || !SafeDisplayText(line) {
			return nil
		}
		first, current := indices, indices
		for layer, spans := range layers {
			for indices[layer] < len(spans) && spans[indices[layer]].Row == row {
				indices[layer]++
			}
		}
		boundaries := make([]bool, f.Width+1)
		boundaries[0] = true
		var out strings.Builder
		style := ""
		setStyle := func(next string) {
			if style == next {
				return
			}
			if style != "" {
				out.WriteString("\x1b[0m")
			}
			out.WriteString(next)
			style = next
		}
		at := 0
		g := uniseg.NewGraphemes(line)
		for g.Next() {
			next := at + g.Width()
			if next > f.Width {
				return nil
			}
			boundaries[next] = true
			on := [2]bool{}
			for layer, spans := range layers {
				for current[layer] < indices[layer] && spans[current[layer]].End <= at {
					current[layer]++
				}
				on[layer] = current[layer] < indices[layer] && spans[current[layer]].Start <= at
			}
			selected, matched := on[0], on[1]
			cursor := showCursor && row == f.CursorRow && f.CursorColumn >= at && f.CursorColumn < next
			switch {
			case (selected && cursor) || (matched && (selected || cursor)):
				setStyle("\x1b[4;7m")
			case selected || cursor:
				setStyle("\x1b[7m")
			case matched:
				setStyle("\x1b[4m")
			default:
				setStyle("")
			}
			out.WriteString(g.Str())
			at = next
		}
		for layer, spans := range layers {
			for _, s := range spans[first[layer]:indices[layer]] {
				if s.End > at || !boundaries[s.Start] || !boundaries[s.End] {
					return nil
				}
			}
		}
		setStyle("")
		if showCursor && row == f.CursorRow && f.CursorColumn >= at {
			out.WriteString(strings.Repeat(" ", f.CursorColumn-at))
			out.WriteString("\x1b[7m \x1b[0m")
		}
		lines[row] = out.String()
	}
	return lines
}

// ANSI emits only fixed controls and validated text. Tea uses StyledLines
// instead, so this absolute CUP protocol never enters its relative renderer.
func (f Frame) ANSI() string {
	lines := f.StyledLines(false)
	if lines == nil {
		return ""
	}
	var out strings.Builder
	out.WriteString("\x1b[0m\x1b[?25l\x1b[?7l")
	for row, line := range lines {
		fmt.Fprintf(&out, "\x1b[%d;1H\x1b[2K", row+1)
		out.WriteString(line)
	}
	fmt.Fprintf(&out, "\x1b[0m\x1b[%d;%dH\x1b[?7h\x1b[?25h", f.CursorRow+1, f.CursorColumn+1)
	return out.String()
}
