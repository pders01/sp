package embeddedvim

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/pders01/sp/internal/editorcore"
	"github.com/rivo/uniseg"
)

// Viewport owns display offsets only. It never changes a snapshot, runs a guest,
// or accesses a terminal. Like a Session, its owner must serialize its use.
// Offsets are display cells, not bytes or UTF-16 positions.
type Viewport struct {
	top, left, promptLeft int
	application           bool // Legacy synthetic callers retain their small demo marker.
}

type viewportCell struct {
	text  string
	width int
	ascii bool // Parent-owned ASCII runs can be clipped at any cell boundary.
}

// Each boundary pairs a source UTF-16 grapheme boundary with its projected
// display edge. Unsafe clusters can expand into several ASCII cells, but remain
// a single selection unit. The final CR of CRLF is not a displayed grapheme.
type viewportBoundary struct {
	source, cell int
}

type viewportLine struct {
	cells      []viewportCell
	boundaries []viewportBoundary
	units      int
	width      int
	cursor     int
}

// Render creates a parent-owned, monochrome frame. There is no soft wrapping.
// One row is reserved for mode/status text, and a second for an active
// prompt. Geometries without at least one document row fail explicitly.
// UTF-16 positions inside a surrogate pair are rejected; scalar boundaries
// inside a grapheme display at that grapheme's start. Tabs use four-cell stops.
func (v *Viewport) Render(snapshot Snapshot, width, height int) (editorcore.Frame, error) {
	unsupported := func() (editorcore.Frame, error) {
		return editorcore.Frame{}, editorcore.ErrUnsupported
	}
	if v == nil || !editorcore.ValidGeometry(width, height) || !validText(snapshot.Text) || !validSelectionSnapshot(snapshot) {
		return unsupported()
	}
	var mode string
	switch snapshot.Mode {
	case "normal":
		mode = "NORMAL"
	case "insert":
		mode = "INSERT"
	case "replace":
		mode = "REPLACE"
	case "visual":
		mode = "VISUAL"
		if snapshot.SelectionMode == "line" {
			mode += " LINE"
		}
		if snapshot.SelectionMode == "block" {
			mode += " BLOCK"
		}
	default:
		return unsupported()
	}
	if !v.application {
		mode += " [demo]"
	}
	rows := height - 1
	var prompt viewportLine
	if p := snapshot.Prompt; p != nil {
		rows--
		var prefix string
		switch p.Kind {
		case "search":
			prefix = "/"
			mode += " SEARCH"
		case "ex":
			prefix = ":"
			mode += " EX"
		case "confirm":
			prefix = "?"
			mode += " CONFIRM"
		default:
			return unsupported()
		}
		if len(p.Value) > 4096 || !utf8.ValidString(p.Value) {
			return unsupported()
		}
		if p.Prefix != "" {
			prefix = p.Prefix
		}
		var ok bool
		prompt, ok = projectViewportLine(prefix+p.Value, p.Cursor+1, false)
		if p.Cursor < 0 || !ok {
			return unsupported()
		}
	}
	if rows < 1 {
		return unsupported()
	}
	lines := strings.Split(snapshot.Text, "\n")
	if snapshot.Cursor.Line < 0 || snapshot.Cursor.Line >= len(lines) {
		return unsupported()
	}
	current, ok := projectViewportLine(lines[snapshot.Cursor.Line], snapshot.Cursor.Ch, snapshot.Cursor.Line < len(lines)-1)
	if !ok {
		return unsupported()
	}
	// Commit offsets only after the complete frame passes the shared validator.
	next := *v
	if snapshot.Viewport != nil {
		next.top = snapshot.Viewport.Top
		// Search previews may show a match away from the source caret while
		// input stays in the visible prompt. Otherwise keep the caret visible.
		if snapshot.Prompt == nil {
			next.top = viewportFollow(next.top, snapshot.Cursor.Line, rows)
		}
	} else {
		next.top = viewportFollow(next.top, snapshot.Cursor.Line, rows)
		if limit := max(0, len(lines)-rows); next.top > limit {
			next.top = limit
		}
	}
	gutter := viewportGutter(len(lines), width)
	textWidth := width - gutter
	next.left = viewportFollow(next.left, current.cursor, textWidth)
	next.left = min(next.left, max(0, current.width-textWidth+1))
	// Do not place the viewport edge inside the cursor's grapheme. In a
	// one-column viewport a two-cell grapheme becomes a fixed ASCII '?'.
	next.left = viewportAlignLeft(current, next.left, current.cursor, textWidth)
	frame := editorcore.Frame{Width: width, Height: height, Lines: make([]string, height),
		CursorRow: snapshot.Cursor.Line - next.top, CursorColumn: gutter + current.cursor - next.left}
	matching := snapshot
	matching.Mode, matching.SelectionMode, matching.Selections = "visual", "char", snapshot.Matches
	for row := 0; row < rows; row++ {
		index := next.top + row
		if index >= len(lines) {
			frame.Lines[row] = viewportASCII("~", width)
			continue
		}
		line := current
		if index != snapshot.Cursor.Line {
			line, _ = projectViewportLine(lines[index], 0, index < len(lines)-1)
		}
		if gutter > 0 {
			frame.Lines[row] = fmt.Sprintf("%*d ", gutter-1, index+1)
		}
		frame.Lines[row] += clipViewportLine(line, next.left, textWidth)
		frame.Selections = append(frame.Selections, viewportSelections(snapshot, index, row, line, next.left, gutter, textWidth)...)
		frame.Matches = append(frame.Matches, viewportSelections(matching, index, row, line, next.left, gutter, textWidth)...)
	}
	if len(snapshot.Notices) != 0 {
		// Only exact, parent-owned messages may be displayed. Unknown guest
		// strings could contain private regex/register data or terminal controls.
		message := "NOTICE"
		switch snapshot.Notices[len(snapshot.Notices)-1] {
		case "Use :wq or F2 to save and close":
			message = "Use :wq or F2 to save and close"
		case "Unsupported character boundary":
			message = "Boundary rejected; use undo"
		case "Unsupported substitute flags or count":
			message = "Unsupported substitute flags/count"
		case "Unsupported command arguments":
			message = "Command arguments unavailable"
		case "Unsupported replace character boundary":
			message = "Replace character boundary unavailable"
		case "Unsupported replace mode":
			message = "Replace mode unavailable"
		case "Unsupported headless editor API":
			message = "Command unavailable"
		case "Vim notification":
			message = "Vim command notice"
		case "Prompt input rejected (4 KiB limit)":
			message = "Prompt input exceeds 4 KiB"
		case "Paste cannot answer confirmation":
			message = "Use physical confirmation keys"
		}
		mode += " | " + message
	}
	status := viewportASCII(mode, width)
	if len(snapshot.Notices) == 0 {
		// Coordinates describe the source cursor in projected display cells,
		// independent of horizontal scrolling, the gutter, or a prompt caret.
		position := fmt.Sprintf("%d:%d %d%%", snapshot.Cursor.Line+1, current.cursor+1,
			(snapshot.Cursor.Line+1)*100/len(lines))
		if len(mode)+1+len(position) <= width {
			status = mode + strings.Repeat(" ", width-len(mode)-len(position)) + position
		}
	}
	frame.Lines[rows] = status
	frame.StatusRow = &rows
	if snapshot.Prompt != nil {
		next.promptLeft = viewportFollow(next.promptLeft, prompt.cursor, width)
		next.promptLeft = min(next.promptLeft, max(0, prompt.width-width+1))
		if snapshot.Prompt.Cursor == 0 && width > 1 {
			next.promptLeft = 0 // Keep the fixed prompt prefix when it fits.
		}
		next.promptLeft = viewportAlignLeft(prompt, next.promptLeft, prompt.cursor, width)
		frame.Lines[rows+1] = clipViewportLine(prompt, next.promptLeft, width)
		frame.CursorRow, frame.CursorColumn = rows+1, prompt.cursor-next.promptLeft
	} else {
		next.promptLeft = 0
	}
	if frame.ANSI() == "" {
		return unsupported()
	}
	*v = next
	return frame, nil
}

// The validated 1 MiB source bounds the gutter to seven digits plus a space.
// Keep at least eight document cells; tiny geometries spend no cells on chrome.
func viewportGutter(lines, width int) int {
	gutter := len(fmt.Sprint(lines)) + 1
	if width-gutter < 8 {
		return 0
	}
	return gutter
}

func viewportFollow(offset, cursor, size int) int {
	if offset < 0 || cursor < offset {
		return cursor
	}
	if cursor >= offset+size {
		return cursor - size + 1
	}
	return offset
}

// Align only the cursor line: other lines may have different wide-cell edges,
// which clip to blanks rather than emitting half a grapheme.
func viewportAlignLeft(line viewportLine, left, cursor, width int) int {
	column, cursorEnd := 0, cursor+1
	for _, cell := range line.cells {
		end := column + cell.width
		if column == cursor && !cell.ascii && cell.width <= width {
			cursorEnd = end
			break
		}
		column = end
	}
	if cursorEnd > left+width {
		left = cursorEnd - width
	}
	column = 0
	for _, cell := range line.cells {
		end := column + cell.width
		if !cell.ascii && column < left && left < end {
			// Moving to this cell's start is safe only if the cursor still fits.
			if cursorEnd-column <= width {
				left = column
			} else {
				left = end
			}
			break
		}
		column = end
	}
	return left
}

// projectViewportLine validates the UTF-16 boundary before doing any display
// work. A CR immediately before a source LF is a terminator; a lone CR is data.
// Only fixed ASCII projections of unsafe/zero-width clusters reach a frame.
func projectViewportLine(text string, cursor int, beforeLF bool) (viewportLine, bool) {
	line := viewportLine{}
	if cursor < 0 {
		return line, false
	}
	units, cursorByte := 0, -1
	for index, r := range text {
		if units == cursor {
			cursorByte = index
		}
		units++
		if r > 0xffff {
			units++
		}
	}
	if units == cursor {
		cursorByte = len(text)
	}
	if cursorByte < 0 {
		return line, false
	}
	if beforeLF && strings.HasSuffix(text, "\r") {
		text = text[:len(text)-1]
		cursorByte = min(cursorByte, len(text))
	}
	appendASCII := func(s string) {
		line.cells = append(line.cells, viewportCell{text: s, width: len(s), ascii: true})
		line.width += len(s)
	}
	line.boundaries = []viewportBoundary{{}}
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		start, end := g.Positions()
		if start <= cursorByte && cursorByte < end {
			line.cursor = line.width
		}
		cluster := g.Str()
		w := g.Width()
		switch {
		case cluster == "\t":
			appendASCII(strings.Repeat(" ", 4-line.width%4))
		case !editorcore.SafeDisplayText(cluster) || w == 0:
			for _, r := range cluster {
				appendASCII(fmt.Sprintf("<U+%04X>", r))
			}
		case w > editorcore.MaxColumns || len(cluster) > w*editorcore.MaxCellBytes:
			// Pathological combining clusters have bounded display cost. Exact
			// source text remains in the snapshot, never in display metadata.
			appendASCII("?")
		default:
			line.cells = append(line.cells, viewportCell{text: cluster, width: w})
			line.width += w
		}
		for _, r := range cluster {
			line.units++
			if r > 0xffff {
				line.units++
			}
		}
		line.boundaries = append(line.boundaries, viewportBoundary{source: line.units, cell: line.width})
	}
	if cursorByte == len(text) {
		line.cursor = line.width
	}
	return line, true
}

// viewportSelectionEdge rounds outwards to include a whole projected source
// grapheme, including an entire tab or visible control escape. Binary searches
// keep many bounded ranges on a long source line from repeatedly scanning it.
func viewportSelectionEdge(line viewportLine, source int, end bool) int {
	if source >= line.units {
		return line.width
	}
	i := sort.Search(len(line.boundaries), func(i int) bool {
		return line.boundaries[i].source >= source
	})
	if !end && line.boundaries[i].source > source {
		i--
	}
	return line.boundaries[i].cell
}

func viewportCursorBefore(a, b Cursor) bool {
	return a.Line < b.Line || a.Line == b.Line && a.Ch < b.Ch
}

// Selection metadata is validated before reaching this function. Only real
// source rows call it; chrome, EOF markers and prompt rows cannot be selected.
func viewportSelections(snapshot Snapshot, index, row int, line viewportLine, left, gutter, width int) []editorcore.CellSpan {
	if snapshot.Mode != "visual" || len(snapshot.Selections) == 0 {
		return nil
	}
	var spans []editorcore.CellSpan
	for _, selection := range snapshot.Selections {
		a, b := selection.Anchor, selection.Head
		if viewportCursorBefore(b, a) {
			a, b = b, a
		}
		if index < a.Line || index > b.Line {
			continue
		}
		start, end := 0, left+width
		if snapshot.SelectionMode != "line" {
			// A half-open end at next-line column zero selects none of that
			// row, even if it is empty. Only an actually empty range on an
			// empty line receives the visual-mode blank-cell fallback.
			if index == b.Line && b.Ch == 0 && a.Line != b.Line {
				continue
			}
			if index == a.Line {
				start = viewportSelectionEdge(line, a.Ch, false)
			}
			if index == b.Line {
				end = viewportSelectionEdge(line, b.Ch, true)
				if b.Ch > line.units { // A selected CR in a CRLF terminator.
					end = max(end, left+width)
				}
			}
			if a == b {
				if line.units != 0 {
					continue
				}
				end = start + 1
			}
		}
		start, end = max(start-left, 0), min(end-left, width)
		if start < end {
			spans = append(spans, editorcore.CellSpan{Row: row, Start: gutter + start, End: gutter + end})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].Start < spans[j].Start })
	merged := spans[:0]
	for _, span := range spans {
		if len(merged) > 0 && span.Start <= merged[len(merged)-1].End {
			merged[len(merged)-1].End = max(merged[len(merged)-1].End, span.End)
		} else {
			merged = append(merged, span)
		}
	}
	return merged
}

func clipViewportLine(line viewportLine, left, width int) string {
	var out strings.Builder
	column, used := 0, 0
	for _, cell := range line.cells {
		end := column + cell.width
		if end > left && column < left+width {
			n := min(end, left+width) - max(column, left)
			if cell.ascii {
				out.WriteString(cell.text[max(column, left)-column : min(end, left+width)-column])
			} else if column >= left && end <= left+width {
				out.WriteString(cell.text)
			} else if cell.width > width && column == left && n == width {
				// Tiny geometry cannot fit this grapheme. Do not emit half.
				out.WriteString("?" + strings.Repeat(" ", n-1))
			} else {
				out.WriteString(strings.Repeat(" ", n))
			}
			used += n
		}
		column = end
		if column >= left+width {
			break
		}
	}
	// Padding includes a real blank cell for insertion/EOF cursors.
	out.WriteString(strings.Repeat(" ", width-used))
	return out.String()
}

func viewportASCII(text string, width int) string {
	text = text[:min(len(text), width)]
	return text + strings.Repeat(" ", width-len(text))
}
