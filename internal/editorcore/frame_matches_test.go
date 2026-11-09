package editorcore

import (
	"strings"
	"testing"
)

func TestSearchMatchesComposeWithSelectionAndCursor(t *testing.T) {
	f := Frame{Width: 5, Height: 2, Lines: []string{"abcde", "next"}, CursorColumn: 2,
		Selections: []CellSpan{{0, 1, 4}}, Matches: []CellSpan{{0, 0, 2}}}
	rows := f.StyledLines(true)
	if len(rows) < 2 || !strings.Contains(rows[0], "\x1b[4ma") || !strings.Contains(rows[0], "\x1b[4;7mbc") || rows[1] != "next" {
		t.Fatal("match/selection/cursor composition or row reset failed")
	}
	f.Matches = []CellSpan{{0, 0, 6}}
	if f.ANSI() != "" {
		t.Fatal("out-of-frame match span accepted")
	}
	f = Frame{Width: 3, Height: 1, Lines: []string{"界x"}, Matches: []CellSpan{{0, 0, 1}}}
	if f.StyledLines(false) != nil {
		t.Fatal("match split a wide glyph")
	}
	f.Matches[0].End = 2
	if rows := f.StyledLines(false); len(rows) < 1 || !strings.HasPrefix(rows[0], "\x1b[4m界\x1b[0m") {
		t.Fatal("whole glyph match rejected")
	}
}
