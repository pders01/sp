package embeddedvim

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/pders01/sp/internal/editorcore"
	"github.com/rivo/uniseg"
)

func viewportSnapshot(text string, line, ch int) Snapshot {
	return Snapshot{Text: text, Cursor: Cursor{Line: line, Ch: ch}, Mode: "normal"}
}

func viewportRender(t *testing.T, v *Viewport, snapshot Snapshot, width, height int) editorcore.Frame {
	t.Helper()
	frame, err := v.Render(snapshot, width, height)
	if err != nil {
		t.Fatal("synthetic viewport unexpectedly rejected")
	}
	if frame.ANSI() == "" || frame.Width != width || frame.Height != height || len(frame.Lines) != height {
		t.Fatal("invalid synthetic frame")
	}
	statusRow := height - 1
	if snapshot.Prompt != nil {
		statusRow--
	}
	if frame.StatusRow == nil || *frame.StatusRow != statusRow {
		t.Fatal("status slot missing or points to document/prompt")
	}
	for _, line := range frame.Lines {
		if !editorcore.SafeDisplayText(line) || uniseg.StringWidth(line) != width {
			t.Fatal("unsafe or incorrectly padded synthetic line")
		}
	}
	return frame
}

func TestViewportUnicodeAndUTF16(t *testing.T) {
	text := "a\u0301界👩‍💻\tZ"
	for _, tc := range []struct{ ch, column int }{
		{0, 0}, {1, 0}, // Combining scalar boundary snaps to the grapheme start.
		{2, 1}, {3, 3}, {5, 3}, {6, 3}, {8, 5}, {9, 8}, {10, 9},
	} {
		var v Viewport
		frame := viewportRender(t, &v, viewportSnapshot(text, 0, tc.ch), 20, 4)
		if frame.CursorRow != 0 || frame.CursorColumn != tc.column+2 ||
			frame.Lines[0] != "1 a\u0301界👩‍💻   Z"+strings.Repeat(" ", 9) {
			t.Fatal("Unicode projection or UTF-16 cursor mismatch")
		}
	}
}

func TestViewportControlsBidiAndCRLF(t *testing.T) {
	text := "\x1b[31mA\u202eB\rC\nX\r\n\t\x00\x7f\u0085\u2028\u2029\ufeff\ufdd0\U0010ffff\u200d"
	snapshot := viewportSnapshot(text, 1, 2)
	snapshot.Notices = []string{"untrusted private notice\x1b[2J"}
	before := viewportSnapshot(text, 1, 2)
	before.Notices = append([]string(nil), snapshot.Notices...)
	var v Viewport
	frame := viewportRender(t, &v, snapshot, 150, 5)
	if !strings.HasPrefix(frame.Lines[0], "1 <U+001B>[31mA<U+202E>B<U+000D>C") ||
		!strings.HasPrefix(frame.Lines[1], "2 X ") || frame.CursorColumn != 3 ||
		!strings.HasPrefix(frame.Lines[2], "3     <U+0000><U+007F><U+0085><U+2028><U+2029><U+FEFF><U+FDD0><U+10FFFF><U+200D>") ||
		!strings.HasPrefix(frame.Lines[4], "NORMAL [demo] | NOTICE") {
		t.Fatal("unsafe text was not visibly projected")
	}
	if strings.Contains(frame.ANSI(), "untrusted private notice") || !reflect.DeepEqual(before, snapshot) {
		t.Fatal("private notice rendered or source mutated")
	}
	// A trailing CR with no LF is data, not a terminator.
	frame = viewportRender(t, &v, viewportSnapshot("X\r", 0, 2), 20, 2)
	if !strings.HasPrefix(frame.Lines[0], "1 X<U+000D>") || frame.CursorColumn != 11 {
		t.Fatal("lone CR was hidden")
	}
}

func TestViewportOnlyShowsFixedActionableNotices(t *testing.T) {
	var v Viewport
	s := viewportSnapshot("synthetic", 0, 0)
	s.Notices = []string{"Use :wq or F2 to save and close"}
	frame := viewportRender(t, &v, s, 80, 3)
	if !strings.Contains(frame.Lines[2], "Use :wq or F2") {
		t.Fatal("fixed lifecycle guidance hidden")
	}
	s.Notices = []string{"private untrusted command detail"}
	frame = viewportRender(t, &v, s, 80, 3)
	if strings.Contains(frame.ANSI(), "private untrusted command detail") || !strings.Contains(frame.Lines[2], "NOTICE") {
		t.Fatal("untrusted notice was not suppressed")
	}
}

func TestViewportInvalidSnapshots(t *testing.T) {
	base := viewportSnapshot("😀\nx", 0, 0)
	cases := []Snapshot{
		viewportSnapshot("😀", 0, -1), viewportSnapshot("😀", 0, 1),
		viewportSnapshot("😀", 0, 3), viewportSnapshot("x", -1, 0),
		viewportSnapshot("x", 1, 0), viewportSnapshot("\xff", 0, 0),
		viewportSnapshot(strings.Repeat("x", MaxTextBytes+1), 0, 0),
	}
	badMode := base
	badMode.Mode = "private\x1b[31m"
	cases = append(cases, badMode)
	for _, p := range []*Prompt{
		{Kind: "private", Value: "x"}, {Kind: "ex", Value: "\xff"},
		{Kind: "search", Value: "😀", Cursor: 1}, {Kind: "ex", Value: "x", Cursor: -1},
		{Kind: "confirm", Value: "x", Cursor: 2},
		{Kind: "ex", Value: strings.Repeat("x", 4097)},
	} {
		s := base
		s.Prompt = p
		cases = append(cases, s)
	}
	for _, snapshot := range cases {
		v := Viewport{top: 7, left: 3, promptLeft: 8}
		before := v
		frame, err := v.Render(snapshot, 20, 5)
		if !errors.Is(err, editorcore.ErrUnsupported) || frame.ANSI() != "" || v != before {
			t.Fatal("invalid synthetic snapshot accepted or offsets changed")
		}
	}
}

func TestViewportScrollResizeAndEOF(t *testing.T) {
	var v Viewport
	snapshot := viewportSnapshot("0123456789", 0, 8)
	frame := viewportRender(t, &v, snapshot, 4, 3)
	if frame.Lines[0] != "5678" || frame.CursorColumn != 3 {
		t.Fatal("horizontal cursor follow failed")
	}
	snapshot.Cursor.Ch = 10
	frame = viewportRender(t, &v, snapshot, 4, 3)
	if frame.Lines[0] != "789 " || frame.CursorColumn != 3 {
		t.Fatal("EOF blank cursor cell missing")
	}
	frame = viewportRender(t, &v, snapshot, 14, 3)
	if frame.Lines[0] != "1 0123456789  " || frame.CursorColumn != 12 {
		t.Fatal("horizontal resize did not reveal available content")
	}
	snapshot.Cursor.Ch = 0
	frame = viewportRender(t, &v, snapshot, 4, 3)
	if frame.Lines[0] != "0123" || frame.CursorColumn != 0 {
		t.Fatal("leftward cursor follow failed")
	}
	snapshot = viewportSnapshot("0\n1\n2\n3\n4\n5\n6\n7", 7, 0)
	frame = viewportRender(t, &v, snapshot, 4, 4)
	if frame.Lines[0] != "5   " || frame.Lines[2] != "7   " || frame.CursorRow != 2 {
		t.Fatal("vertical cursor follow failed")
	}
	frame = viewportRender(t, &v, snapshot, 8, 8)
	if frame.Lines[0] != "1       " || frame.CursorRow != 6 {
		t.Fatal("resize did not clamp vertical offset")
	}
	snapshot.Cursor.Line = 0
	frame = viewportRender(t, &v, snapshot, 8, 8)
	if frame.CursorRow != 0 || frame.Lines[0] != "0       " {
		t.Fatal("upward cursor follow failed")
	}
}

func TestViewportWideCellClipping(t *testing.T) {
	for _, tc := range []struct {
		text      string
		ch, width int
		line      string
		column    int
	}{
		{"界x", 0, 1, "?", 0},
		{"a界x", 1, 2, "界", 0},
		{"界a界x", 2, 4, "a界x", 1},
		{"👩‍💻x", 0, 1, "?", 0},
		{"a\u0301界", 2, 2, "界", 0},
	} {
		var v Viewport
		frame := viewportRender(t, &v, viewportSnapshot(tc.text, 0, tc.ch), tc.width, 2)
		if frame.Lines[0] != tc.line || frame.CursorColumn != tc.column {
			t.Fatal("wide cursor grapheme was clipped or split")
		}
	}
	// Other rows can have different cluster boundaries at the shared offset.
	var v Viewport
	frame := viewportRender(t, &v, viewportSnapshot("012345\n界abcd", 0, 4), 4, 3)
	if frame.Lines[1] != " abc" {
		t.Fatal("partial wide cell was not replaced with a blank")
	}
}

func TestViewportPromptCursorAndPrivateText(t *testing.T) {
	for _, kind := range []string{"search", "ex", "confirm"} {
		var v Viewport
		snapshot := viewportSnapshot("unchanged", 0, 2)
		snapshot.Mode = "insert"
		snapshot.Prompt = &Prompt{Kind: kind, Value: "a\u0301界😀\t\x1b\u202e", Cursor: 2}
		p := *snapshot.Prompt
		frame := viewportRender(t, &v, snapshot, 30, 4)
		if frame.CursorRow != 3 || frame.CursorColumn != 2 || !strings.HasPrefix(frame.Lines[2], "INSERT [demo]") ||
			!strings.Contains(frame.Lines[3], "<U+001B><U+202E>") {
			t.Fatal("prompt cursor, indicator or projection mismatch")
		}
		if *snapshot.Prompt != p || snapshot.Text != "unchanged" {
			t.Fatal("prompt rendering changed stored text")
		}
		snapshot.Prompt.Cursor = len(utf16.Encode([]rune(snapshot.Prompt.Value)))
		frame = viewportRender(t, &v, snapshot, 5, 3)
		if frame.CursorRow != 2 || frame.CursorColumn != 4 || frame.Lines[2][4] != ' ' {
			t.Fatal("prompt did not scroll to its blank EOF cursor")
		}
		snapshot.Prompt.Cursor = 0
		frame = viewportRender(t, &v, snapshot, 5, 3)
		if frame.CursorColumn != 1 || !strings.HasPrefix(frame.Lines[2], map[string]string{"search": "/", "ex": ":", "confirm": "?"}[kind]) {
			t.Fatal("prompt did not scroll back to its prefix")
		}
	}
}

func TestViewportTinyGeometryAndFixedModes(t *testing.T) {
	for _, mode := range []string{"normal", "insert", "visual"} {
		for width := 1; width <= 4; width++ {
			var v Viewport
			s := viewportSnapshot("", 0, 0)
			s.Mode = mode
			frame := viewportRender(t, &v, s, width, 2)
			if frame.Lines[0] != strings.Repeat(" ", width) || frame.Lines[1][0] != strings.ToUpper(mode)[0] {
				t.Fatal("tiny mode/EOF display mismatch")
			}
			s.Prompt = &Prompt{Kind: "ex", Value: "界", Cursor: 1}
			viewportRender(t, &v, s, width, 3)
		}
	}
	for _, geometry := range [][2]int{{0, 2}, {1, 0}, {241, 2}, {2, 101}, {1, 1}, {-1, 3}} {
		var v Viewport
		if _, err := v.Render(viewportSnapshot("", 0, 0), geometry[0], geometry[1]); !errors.Is(err, editorcore.ErrUnsupported) {
			t.Fatal("unsupported geometry accepted")
		}
	}
	var v Viewport
	s := viewportSnapshot("", 0, 0)
	s.Prompt = &Prompt{Kind: "ex"}
	if _, err := v.Render(s, 10, 2); !errors.Is(err, editorcore.ErrUnsupported) {
		t.Fatal("prompt accepted without enough rows")
	}
	var nilViewport *Viewport
	if _, err := nilViewport.Render(s, 10, 3); !errors.Is(err, editorcore.ErrUnsupported) {
		t.Fatal("nil viewport accepted")
	}
}

func TestViewportSyntheticGeometryMatrix(t *testing.T) {
	// Model checks use only synthetic source strings and scalar UTF-16
	// boundaries, not guest execution or a native terminal.
	for _, text := range []string{"", "界界ab", "a\u0301👩‍💻🇩🇪界", "\t\x1b\u202e\r", "\u0301\u200dZ"} {
		boundaries := []int{0}
		units := 0
		for _, r := range text {
			units++
			if r > 0xffff {
				units++
			}
			boundaries = append(boundaries, units)
		}
		var v Viewport
		for _, width := range []int{1, 2, 3, 4, 5, 12, editorcore.MaxColumns} {
			for _, height := range []int{2, 3, 5, editorcore.MaxRows} {
				for _, cursor := range boundaries {
					snapshot := viewportSnapshot(text+"\n界abc", 0, cursor)
					for _, withPrompt := range []bool{false, true} {
						if withPrompt {
							if height < 3 {
								continue
							}
							snapshot.Prompt = &Prompt{Kind: "search", Value: text, Cursor: cursor}
						}
						frame := viewportRender(t, &v, snapshot, width, height)
						column, boundary := 0, false
						g := uniseg.NewGraphemes(frame.Lines[frame.CursorRow])
						for g.Next() {
							if column == frame.CursorColumn {
								boundary = true
							}
							column += g.Width()
						}
						if !boundary {
							t.Fatal("cursor landed inside a displayed grapheme")
						}
					}
				}
			}
		}
	}
}

func TestViewportGutterGeometryAndAlignment(t *testing.T) {
	for _, tc := range []struct{ count, width, gutter int }{
		{1, 9, 0}, {1, 10, 2}, {10, 10, 0}, {10, 11, 3},
		{1000, 12, 0}, {1000, 13, 5}, {1000, editorcore.MaxColumns, 5},
	} {
		var v Viewport
		s := viewportSnapshot(strings.Repeat("x\n", tc.count-1)+"x", tc.count-1, 0)
		frame := viewportRender(t, &v, s, tc.width, 4)
		for row := 0; row < min(3, tc.count); row++ {
			prefix := ""
			if tc.gutter > 0 {
				prefix = fmt.Sprintf("%*d ", tc.gutter-1, v.top+row+1)
			}
			if frame.Lines[row] != viewportASCII(prefix+"x", tc.width) {
				t.Fatalf("gutter alignment: count=%d width=%d row=%d", tc.count, tc.width, row)
			}
		}
		if frame.CursorColumn != tc.gutter {
			t.Fatal("gutter did not offset document cursor")
		}
	}
	if viewportGutter(MaxTextBytes+1, editorcore.MaxColumns) != 8 {
		t.Fatal("maximum source gutter is not bounded to eight cells")
	}
}

func TestViewportGutterScrollAndResize(t *testing.T) {
	var v Viewport
	s := viewportSnapshot(strings.TrimSuffix(strings.Repeat("0123456789abcdef\n", 12), "\n"), 8, 14)
	frame := viewportRender(t, &v, s, 12, 4)
	if frame.Lines[0] != " 7 6789abcde" || frame.Lines[2] != " 9 6789abcde" || frame.CursorColumn != 11 {
		t.Fatal("gutter moved with horizontal scroll or cursor clipped")
	}
	s.Cursor.Line = 9
	s.Cursor.Ch = 16
	frame = viewportRender(t, &v, s, 12, 4)
	if frame.Lines[0] != " 8 89abcdef " || frame.Lines[2] != "10 89abcdef " || frame.CursorRow != 2 || frame.CursorColumn != 11 {
		t.Fatal("multi-digit line number or EOF cursor misaligned after scrolling")
	}
	frame = viewportRender(t, &v, s, 9, 4)
	if frame.Lines[2] != "89abcdef " || frame.CursorColumn != 8 {
		t.Fatal("narrow resize did not remove gutter")
	}
	frame = viewportRender(t, &v, s, 24, 4)
	if frame.Lines[2] != "10 0123456789abcdef     " || frame.CursorColumn != 19 {
		t.Fatal("large resize did not restore gutter and reveal content")
	}
	s.Cursor.Line, s.Cursor.Ch = 0, 0
	frame = viewportRender(t, &v, s, 12, 4)
	if frame.Lines[0] != " 1 012345678" || frame.CursorRow != 0 || frame.CursorColumn != 3 {
		t.Fatal("gutter or content did not follow cursor back to origin")
	}
}

func TestViewportGutterWideCellClipping(t *testing.T) {
	var v Viewport
	s := viewportSnapshot("0123456界Z\n界abcdefghi", 0, 7)
	frame := viewportRender(t, &v, s, 10, 3)
	if frame.Lines[0] != "1 123456界" || frame.CursorColumn != 8 || frame.Lines[1] != "2  abcdefg" {
		t.Fatal("gutter split cursor grapheme or exposed a partial wide cell")
	}
	s.Cursor.Ch = 9
	frame = viewportRender(t, &v, s, 10, 3)
	if frame.Lines[0] != "1 3456界Z " || frame.CursorColumn != 9 {
		t.Fatal("gutter lost the blank EOF cursor after a wide grapheme")
	}
}

func TestViewportEOFMarkers(t *testing.T) {
	for _, width := range []int{1, 9, 12, editorcore.MaxColumns} {
		var v Viewport
		s := viewportSnapshot("x\n", 1, 0)
		frame := viewportRender(t, &v, s, width, 5)
		gutter := viewportGutter(2, width)
		empty := ""
		if gutter > 0 {
			empty = "2 "
		}
		if frame.Lines[1] != viewportASCII(empty, width) || frame.CursorRow != 1 || frame.CursorColumn != gutter {
			t.Fatal("real empty final line confused with EOF marker")
		}
		for _, row := range []int{2, 3} {
			if frame.Lines[row] != viewportASCII("~", width) {
				t.Fatal("missing or numbered EOF marker")
			}
		}
		s.Prompt = &Prompt{Kind: "ex", Value: "x", Cursor: 1}
		frame = viewportRender(t, &v, s, width, 5)
		if frame.Lines[2] != viewportASCII("~", width) || frame.CursorRow != 4 {
			t.Fatal("EOF marker encroached on status or prompt")
		}
		s = viewportSnapshot("", 0, 0)
		frame = viewportRender(t, &v, s, width, 3)
		if strings.Contains(frame.Lines[0], "~") || frame.Lines[1] != viewportASCII("~", width) {
			t.Fatal("empty document must retain a real editable line")
		}
	}
}

func TestViewportStatusCoordinatesAndProgress(t *testing.T) {
	var v Viewport
	s := viewportSnapshot("a\u0301界\tZ\n"+strings.Repeat("x\n", 8)+"x", 0, 4)
	frame := viewportRender(t, &v, s, 30, 4)
	if frame.Lines[3] != "NORMAL [demo]"+strings.Repeat(" ", 10)+"1:5 10%" {
		t.Fatal("status must use one-based source line and projected cell column, right aligned")
	}
	s.Cursor = Cursor{Line: 9, Ch: 1}
	s.Mode = "visual"
	frame = viewportRender(t, &v, s, 30, 4)
	if frame.Lines[3] != "VISUAL [demo]"+strings.Repeat(" ", 8)+"10:2 100%" {
		t.Fatal("last-line progress or visual status mismatch")
	}
	s = viewportSnapshot(strings.Repeat("x", 50), 0, 49)
	frame = viewportRender(t, &v, s, 30, 3)
	if !strings.HasSuffix(frame.Lines[2], "1:50 100%") || frame.CursorColumn == 49 {
		t.Fatal("status column must be independent of horizontal scroll and gutter")
	}
	for _, width := range []int{22, 23, 30} {
		frame = viewportRender(t, &v, s, width, 3)
		want := viewportASCII("NORMAL [demo]", width)
		if width >= 23 {
			want = "NORMAL [demo]" + strings.Repeat(" ", width-22) + "1:50 100%"
		}
		if frame.Lines[2] != want {
			t.Fatal("coordinates should appear only if complete status fits")
		}
	}
	s.Prompt = &Prompt{Kind: "search", Value: "😀", Cursor: 2}
	frame = viewportRender(t, &v, s, 40, 4)
	if !strings.HasPrefix(frame.Lines[2], "NORMAL [demo] SEARCH") || !strings.HasSuffix(frame.Lines[2], "1:50 100%") || frame.CursorColumn != 3 {
		t.Fatal("prompt caret replaced document status coordinates")
	}
	for _, notice := range []string{"Use :wq or F2 to save and close", "private untrusted details\x1b"} {
		s.Notices = []string{notice}
		for _, width := range []int{12, 40, editorcore.MaxColumns} {
			frame = viewportRender(t, &v, s, width, 4)
			message := "NOTICE"
			if notice == "Use :wq or F2 to save and close" {
				message = notice
			}
			if frame.Lines[2] != viewportASCII("NORMAL [demo] SEARCH | "+message, width) || strings.Contains(frame.ANSI(), "private untrusted") {
				t.Fatal("notice lost priority to coordinates or leaked guest text")
			}
		}
	}
}

func TestViewportOversizedAndZeroWidthGraphemes(t *testing.T) {
	var v Viewport
	text := "\u0301" + "a" + strings.Repeat("\u0301", editorcore.MaxCellBytes)
	frame := viewportRender(t, &v, viewportSnapshot(text, 0, 1), 20, 2)
	if !strings.HasPrefix(frame.Lines[0], "1 <U+0301>?") || frame.CursorColumn != 10 {
		t.Fatal("pathological grapheme was not safely projected")
	}
}
