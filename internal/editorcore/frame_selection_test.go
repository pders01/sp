package editorcore

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rivo/uniseg"
)

func TestFrameRejectsInvalidSelections(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spans []CellSpan
	}{
		{"negative row", []CellSpan{{-1, 0, 1}}},
		{"past row", []CellSpan{{2, 0, 1}}},
		{"negative start", []CellSpan{{0, -1, 1}}},
		{"empty", []CellSpan{{0, 1, 1}}},
		{"reversed", []CellSpan{{0, 2, 1}}},
		{"past geometry", []CellSpan{{0, 0, 9}}},
		{"past actual line", []CellSpan{{0, 0, 5}}},
		{"empty line", []CellSpan{{1, 0, 1}}},
		{"overlap", []CellSpan{{0, 0, 3}, {0, 2, 4}}},
		{"duplicate", []CellSpan{{0, 0, 1}, {0, 0, 1}}},
		{"unsorted columns", []CellSpan{{0, 3, 4}, {0, 0, 1}}},
		{"unsorted rows", []CellSpan{{1, 0, 1}, {0, 0, 1}}},
		{"too many", make([]CellSpan, 17)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := Frame{Width: 8, Height: 2, Lines: []string{"abcd", ""}, Selections: tc.spans}
			if f.StyledLines(false) != nil || f.StyledLines(true) != nil || f.ANSI() != "" {
				t.Fatal("invalid selection did not reject the whole frame")
			}
		})
	}
}

func TestFrameSelectionEndpointsAreWholeDisplayGraphemes(t *testing.T) {
	for _, glyph := range []string{"界", "🙂", "👩\u200d💻", "🇬🇧", "e\u0301"} {
		t.Run(glyph, func(t *testing.T) {
			width := uniseg.StringWidth(glyph)
			f := Frame{Width: 8, Height: 1, Lines: []string{"a" + glyph + "b"}}
			for start := 0; start < width+2; start++ {
				for end := start + 1; end <= width+2; end++ {
					f.Selections = []CellSpan{{0, start, end}}
					valid := (start <= 1 || start >= 1+width) && (end <= 1 || end >= 1+width)
					if (f.StyledLines(false) != nil) != valid || (f.ANSI() != "") != valid {
						t.Fatalf("endpoint validation disagrees at [%d,%d)", start, end)
					}
				}
			}
			f.Selections = []CellSpan{{0, 1, 1 + width}}
			for column := 1; column < 1+width; column++ {
				f.CursorColumn = column
				want := "a\x1b[4;7m" + glyph + "\x1b[0mb"
				if got := f.StyledLines(true); len(got) != 1 || got[0] != want {
					t.Fatal("selected cursor split a grapheme")
				}
			}
		})
	}
}

func TestFrameSelectionRejectsUnsafeTextAndStructure(t *testing.T) {
	for _, text := range []string{"a\x1b[7mb", "a\nb", "a\tb", "a\x00b", "a\u009bb", "a\u2028b", "a\u2029b", "a\u202eb", "a\ufdd0b", string([]byte{0xff}), strings.Repeat("a", 9), strings.Repeat("\u0301", 8*MaxCellBytes)} {
		f := Frame{Width: 8, Height: 1, Lines: []string{text}, Selections: []CellSpan{{0, 0, 1}}}
		if f.StyledLines(false) != nil || f.StyledLines(true) != nil || f.ANSI() != "" {
			t.Fatal("unsafe or oversized text was accepted with a selection")
		}
	}
	for _, f := range []Frame{
		{},
		{Width: MaxColumns + 1, Height: 1, Lines: []string{"a"}},
		{Width: 8, Height: MaxRows + 1},
		{Width: 8, Height: 1, Lines: []string{"a", "b"}},
		{Width: 8, Height: 1, Lines: []string{"a"}, CursorRow: -1},
		{Width: 8, Height: 1, Lines: []string{"a"}, CursorRow: 1},
		{Width: 8, Height: 1, Lines: []string{"a"}, CursorColumn: -1},
		{Width: 8, Height: 1, Lines: []string{"a"}, CursorColumn: 8},
	} {
		if f.StyledLines(false) != nil || f.StyledLines(true) != nil || f.ANSI() != "" {
			t.Fatal("invalid frame structure was accepted")
		}
	}
}

func TestFrameSelectionComposesCursorAndResetsEachRow(t *testing.T) {
	f := Frame{Width: 8, Height: 3, CursorColumn: 2, Lines: []string{"abcdef", "ghij", "plain"}, Selections: []CellSpan{{0, 1, 5}, {1, 0, 2}, {1, 2, 4}}}
	want := []string{"a\x1b[7mb\x1b[0m\x1b[4;7mc\x1b[0m\x1b[7mde\x1b[0mf", "\x1b[7mghij\x1b[0m", "plain"}
	if got := f.StyledLines(true); !reflect.DeepEqual(got, want) {
		t.Fatalf("cursor erased selection or styles bled across rows: %q", got)
	}
	want[0] = "a\x1b[7mbcde\x1b[0mf"
	if got := f.StyledLines(false); !reflect.DeepEqual(got, want) {
		t.Fatalf("hidden cursor changed selection: %q", got)
	}
	f.CursorColumn = 5
	if got := f.StyledLines(true)[0]; got != "a\x1b[7mbcdef\x1b[0m" {
		t.Fatalf("normal cursor did not remain reverse: %q", got)
	}
	if !reflect.DeepEqual(f.Lines, []string{"abcdef", "ghij", "plain"}) || !reflect.DeepEqual(f.Selections, []CellSpan{{0, 1, 5}, {1, 0, 2}, {1, 2, 4}}) {
		t.Fatal("rendering mutated frame inputs")
	}
}

func TestFrameCursorPadsOnlyWhenShown(t *testing.T) {
	for _, line := range []string{"", "a", "界", "e\u0301"} {
		f := Frame{Width: 8, Height: 2, CursorColumn: 7, Lines: []string{line, "next"}}
		want := line + strings.Repeat(" ", 7-uniseg.StringWidth(line)) + "\x1b[7m \x1b[0m"
		if got := f.StyledLines(true); len(got) != 2 || got[0] != want || got[1] != "next" {
			t.Fatal("short line did not pad to cursor without row bleed")
		}
		if got := f.StyledLines(false); !reflect.DeepEqual(got, f.Lines) {
			t.Fatal("hidden cursor padded the frame")
		}
	}
}

func TestFrameANSICompatibilityAndSelectionWithoutFakeCursor(t *testing.T) {
	f := Frame{Width: 8, Height: 2, CursorRow: 1, CursorColumn: 7, Lines: []string{"abc", ""}}
	prefix := "\x1b[0m\x1b[?25l\x1b[?7l\x1b[1;1H\x1b[2K"
	tail := "\x1b[2;1H\x1b[2K\x1b[0m\x1b[2;8H\x1b[?7h\x1b[?25h"
	if f.ANSI() != prefix+"abc"+tail {
		t.Fatal("legacy no-selection ANSI contract changed")
	}
	f.Selections = []CellSpan{{0, 1, 3}}
	if f.ANSI() != prefix+"a\x1b[7mbc\x1b[0m"+tail {
		t.Fatal("ANSI changed controls or emitted a fake cursor")
	}
}

func TestFrameMaximumSortedSelectionBudget(t *testing.T) {
	f := Frame{Width: MaxColumns, Height: MaxRows, Lines: make([]string, MaxRows)}
	for row := range f.Lines {
		f.Lines[row] = strings.Repeat("x", MaxColumns)
		for col := 0; col < MaxColumns; col++ {
			f.Selections = append(f.Selections, CellSpan{row, col, col + 1})
		}
	}
	got := f.StyledLines(false)
	if len(got) != MaxRows {
		t.Fatal("maximum valid span budget rejected")
	}
	for _, line := range got {
		if line != "\x1b[7m"+strings.Repeat("x", MaxColumns)+"\x1b[0m" {
			t.Fatal("sorted spans did not cover the bounded canvas")
		}
	}
	f.Selections = append(f.Selections, CellSpan{MaxRows - 1, 0, 1})
	if f.StyledLines(false) != nil || f.ANSI() != "" {
		t.Fatal("over-budget selections accepted")
	}
}
