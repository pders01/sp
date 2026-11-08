package embeddedvim

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/editorcore"
)

func viewportVisual(text, mode string, ranges ...SelectionRange) Snapshot {
	s := viewportSnapshot(text, 0, 0)
	s.Mode, s.SelectionMode, s.Selections = "visual", mode, ranges
	return s
}

func viewportRange(al, ac, bl, bc int) SelectionRange {
	return SelectionRange{Anchor: Cursor{Line: al, Ch: ac}, Head: Cursor{Line: bl, Ch: bc}}
}

func viewportWantSelections(t *testing.T, f editorcore.Frame, want ...editorcore.CellSpan) {
	t.Helper()
	if len(f.Selections) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(f.Selections, want) {
		t.Fatalf("selection spans = %+v, want %+v", f.Selections, want)
	}
}

func TestViewportSelectionCharacterProjection(t *testing.T) {
	for _, tc := range []struct {
		name, text       string
		a, b, start, end int
	}{
		{"ascii", "abcdef", 1, 4, 1, 4},
		{"emoji", "a😀b", 1, 3, 1, 3},
		{"combining mark only", "a\u0301b", 1, 2, 0, 1},
		{"combining base only", "a\u0301b", 0, 1, 0, 1},
		{"emoji zwj interior", "a👩‍💻b", 3, 4, 1, 3},
		{"tab", "a\tb", 1, 2, 1, 4},
		{"control", "a\x1bb", 1, 2, 1, 9},
		{"bidi", "a\u202eb", 1, 2, 1, 9},
		{"lone CR", "a\rb", 1, 2, 1, 9},
		{"zero width", "\u0301b", 0, 1, 0, 8},
		{"oversized cluster", "a" + strings.Repeat("\u0301", editorcore.MaxCellBytes), 1, 2, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, reverse := range []bool{false, true} {
				r := viewportRange(0, tc.a, 0, tc.b)
				if reverse {
					r.Anchor, r.Head = r.Head, r.Anchor
				}
				var v Viewport
				f := viewportRender(t, &v, viewportVisual(tc.text, "char", r), 30, 3)
				viewportWantSelections(t, f, editorcore.CellSpan{Row: 0, Start: 2 + tc.start, End: 2 + tc.end})
			}
		})
	}
}

func TestViewportSelectionMultilineAndCRLF(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		r := viewportRange(0, 1, 2, 1)
		if reverse {
			r.Anchor, r.Head = r.Head, r.Anchor
		}
		var v Viewport
		f := viewportRender(t, &v, viewportVisual("ab\r\n\r\nxy", "char", r), 12, 5)
		viewportWantSelections(t, f,
			editorcore.CellSpan{Row: 0, Start: 3, End: 12},
			editorcore.CellSpan{Row: 1, Start: 2, End: 12},
			editorcore.CellSpan{Row: 2, Start: 2, End: 3})
	}
	for _, text := range []string{"ab\n\nxy", "ab\r\n\r\nxy"} {
		var v Viewport
		f := viewportRender(t, &v, viewportVisual(text, "char", viewportRange(0, 1, 1, 0)), 12, 5)
		viewportWantSelections(t, f, editorcore.CellSpan{Row: 0, Start: 3, End: 12})
	}
	var v Viewport
	// Selecting only the CR of CRLF selects its line-ending padding, not an escape.
	f := viewportRender(t, &v, viewportVisual("ab\r\nx", "char", viewportRange(0, 2, 0, 3)), 12, 4)
	viewportWantSelections(t, f, editorcore.CellSpan{Row: 0, Start: 4, End: 12})
}

func TestViewportSelectionLineBoundsAndChrome(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		r := viewportRange(0, 0, 1, 0)
		if reverse {
			r.Anchor, r.Head = r.Head, r.Anchor
		}
		var v Viewport
		s := viewportVisual("a\n\nlast", "line", r)
		s.Prompt = &Prompt{Kind: "ex", Value: "synthetic", Cursor: 0}
		f := viewportRender(t, &v, s, 12, 7)
		viewportWantSelections(t, f,
			editorcore.CellSpan{Row: 0, Start: 2, End: 12},
			editorcore.CellSpan{Row: 1, Start: 2, End: 12})
		if !strings.HasPrefix(f.Lines[3], "~") || !strings.HasPrefix(f.Lines[5], "VISUAL") || f.CursorRow != 6 {
			t.Fatal("line selection changed EOF, status or prompt placement")
		}
	}
	// Valid columns do not narrow inclusive source line selection. Coordinates
	// must still pass the bridge's UTF-16 bounds checks in every mode.
	var v Viewport
	f := viewportRender(t, &v, viewportVisual("abc\ndef", "line", viewportRange(0, 1, 1, 2)), 12, 4)
	viewportWantSelections(t, f, editorcore.CellSpan{Row: 0, Start: 2, End: 12}, editorcore.CellSpan{Row: 1, Start: 2, End: 12})
}

func TestViewportSelectionBlockUsesActualRangesAndMerges(t *testing.T) {
	var v Viewport
	s := viewportVisual("abcdef\na\tb\n\nuvwxyz", "block",
		viewportRange(3, 5, 3, 4), // Deliberately unsorted, reversed, disjoint ranges.
		viewportRange(0, 3, 0, 5), viewportRange(0, 1, 0, 3), viewportRange(0, 2, 0, 4),
		viewportRange(1, 1, 1, 2), viewportRange(3, 1, 3, 2))
	f := viewportRender(t, &v, s, 14, 6)
	viewportWantSelections(t, f,
		editorcore.CellSpan{Row: 0, Start: 3, End: 7},
		editorcore.CellSpan{Row: 1, Start: 3, End: 6},
		editorcore.CellSpan{Row: 3, Start: 3, End: 4},
		editorcore.CellSpan{Row: 3, Start: 6, End: 7})
}

func TestViewportSelectionEmptyLinesAndModes(t *testing.T) {
	for _, mode := range []string{"char", "block", "line"} {
		for _, width := range []int{1, 4, 12} {
			var v Viewport
			s := viewportVisual("", mode, viewportRange(0, 0, 0, 0))
			s.Prompt = &Prompt{Kind: "search"}
			f := viewportRender(t, &v, s, width, 4)
			gutter, end := viewportGutter(1, width), viewportGutter(1, width)+1
			if mode == "line" {
				end = width
			}
			viewportWantSelections(t, f, editorcore.CellSpan{Row: 0, Start: gutter, End: end})
		}
	}
	for _, mode := range []string{"normal", "insert", "visual"} {
		var v Viewport
		s := viewportSnapshot("abc", 0, 0)
		s.Mode = mode
		viewportWantSelections(t, viewportRender(t, &v, s, 12, 3))
	}
	var v Viewport
	viewportWantSelections(t, viewportRender(t, &v, viewportVisual("abc", "char", viewportRange(0, 1, 0, 1)), 12, 3))
}

func TestViewportSelectionScrollResizeAndWideClipping(t *testing.T) {
	var v Viewport
	s := viewportVisual("0123456789abcdef\n界abcdefghijklm", "char", viewportRange(0, 3, 1, 4))
	s.Cursor = Cursor{Line: 0, Ch: 14}
	f := viewportRender(t, &v, s, 12, 4)
	viewportWantSelections(t, f, editorcore.CellSpan{Row: 0, Start: 2, End: 12})
	f = viewportRender(t, &v, s, 24, 4)
	viewportWantSelections(t, f, editorcore.CellSpan{Row: 0, Start: 5, End: 24}, editorcore.CellSpan{Row: 1, Start: 2, End: 7})
	f = viewportRender(t, &v, s, 4, 4)
	viewportWantSelections(t, f, editorcore.CellSpan{Row: 0, Start: 0, End: 4})

	for _, tc := range []struct {
		text      string
		ch, width int
	}{
		{"界x", 0, 1}, {"界界x", 1, 2}, {"a界x", 1, 2},
	} {
		var tiny Viewport
		s := viewportVisual(tc.text, "char", viewportRange(0, 0, 0, 2))
		s.Cursor.Ch = tc.ch
		f := viewportRender(t, &tiny, s, tc.width, 2)
		viewportWantSelections(t, f, editorcore.CellSpan{Row: 0, Start: 0, End: tc.width})
	}
	var other Viewport
	s = viewportVisual("012345\n界abcd", "char", viewportRange(1, 0, 1, 1))
	s.Cursor.Ch = 4
	f = viewportRender(t, &other, s, 4, 3)
	viewportWantSelections(t, f, editorcore.CellSpan{Row: 1, Start: 0, End: 1})
	if f.Lines[1] != " abc" {
		t.Fatal("partial wide grapheme leaked")
	}

	var vertical Viewport
	s = viewportVisual("0\n1\n2\n3\n4\n5\n6\n7", "line", viewportRange(1, 0, 6, 0))
	s.Cursor.Line = 7
	f = viewportRender(t, &vertical, s, 12, 4)
	viewportWantSelections(t, f, editorcore.CellSpan{Row: 0, Start: 2, End: 12}, editorcore.CellSpan{Row: 1, Start: 2, End: 12})
}

func TestViewportSelectionMetadataRejectedAtomically(t *testing.T) {
	base := viewportVisual("😀\nx", "char", viewportRange(0, 0, 0, 2))
	cases := []Snapshot{}
	for _, r := range []SelectionRange{
		viewportRange(0, 1, 0, 2), viewportRange(-1, 0, 0, 0),
		viewportRange(0, -1, 0, 0), viewportRange(0, 0, 2, 0), viewportRange(0, 0, 1, 2),
	} {
		s := base
		s.Selections = []SelectionRange{r}
		cases = append(cases, s)
	}
	for _, mode := range []string{"", "unknown", "char\x1b"} {
		s := base
		s.SelectionMode = mode
		cases = append(cases, s)
	}
	for _, mode := range []string{"normal", "insert"} {
		s := base
		s.Mode = mode
		cases = append(cases, s)
	}
	s := base
	s.Selections = make([]SelectionRange, MaxSelections+1)
	cases = append(cases, s)
	for _, s := range cases {
		v := Viewport{top: 8, left: 7, promptLeft: 6}
		before := v
		f, err := v.Render(s, 12, 4)
		if !errors.Is(err, editorcore.ErrUnsupported) || f.ANSI() != "" || v != before {
			t.Fatal("invalid selection metadata accepted or offsets changed")
		}
	}
	// The exact bound is accepted, and duplicate projections are merged.
	s = base
	s.Selections = make([]SelectionRange, MaxSelections)
	for i := range s.Selections {
		s.Selections[i] = base.Selections[0]
	}
	var v Viewport
	viewportWantSelections(t, viewportRender(t, &v, s, 12, 4), editorcore.CellSpan{Row: 0, Start: 2, End: 4})
}

func TestViewportSelectionSyntheticGeometryMatrix(t *testing.T) {
	for _, text := range []string{"", "界界ab", "a\u0301👩‍💻🇩🇪界", "\t\x1b\u202e\r", "\u0301\u200dZ"} {
		boundaries, units := []int{0}, 0
		for _, r := range text {
			units++
			if r > 0xffff {
				units++
			}
			boundaries = append(boundaries, units)
		}
		for _, mode := range []string{"char", "block", "line"} {
			var v Viewport
			for _, width := range []int{1, 2, 3, 4, 12, editorcore.MaxColumns} {
				for _, height := range []int{3, 5, editorcore.MaxRows} {
					for _, a := range boundaries {
						for _, b := range boundaries {
							s := viewportVisual(text+"\n界abc", mode, viewportRange(0, a, 0, b))
							s.Cursor.Ch = b
							s.Prompt = &Prompt{Kind: "search", Value: text, Cursor: a}
							f := viewportRender(t, &v, s, width, height)
							for _, span := range f.Selections {
								if span.Row != 0 || span.Start < viewportGutter(2, width) || span.End > width {
									t.Fatal("selection escaped the visible document region")
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestViewportSelectionSourceSnapshotUnchanged(t *testing.T) {
	s := viewportVisual("a\u0301😀\t\x1b\r\n\nlast", "char", viewportRange(2, 2, 0, 1), viewportRange(0, 0, 0, 1))
	s.Prompt = &Prompt{Kind: "ex", Value: "synthetic", Cursor: 3}
	s.Notices = []string{"Vim notification"}
	before := s
	before.Selections = append([]SelectionRange(nil), s.Selections...)
	before.Notices = append([]string(nil), s.Notices...)
	p := *s.Prompt
	before.Prompt = &p
	var v Viewport
	for _, width := range []int{1, 4, 12, 30} {
		viewportRender(t, &v, s, width, 5)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("projection mutated source snapshot")
	}
}
