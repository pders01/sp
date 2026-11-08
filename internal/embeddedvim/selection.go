package embeddedvim

import "strings"

const MaxSelections = 4096

func validActionKind(kind string) bool { return kind == "save" || kind == "finish" || kind == "cancel" }

// Validate bounded source/display metadata, never interpret guest regexes in Go.
// Each requested source line is scanned once for scalar UTF-16 boundaries.
func validSelectionSnapshot(s Snapshot) bool {
	if len(s.Selections) > MaxSelections || len(s.Matches) > MaxSelections {
		return false
	}
	switch s.SelectionMode {
	case "":
		if len(s.Selections) != 0 {
			return false
		}
	case "char", "line", "block":
		if s.Mode != "visual" || len(s.Selections) == 0 || (s.SelectionMode == "line" && len(s.Selections) != 1) {
			return false
		}
	default:
		return false
	}
	if p := s.Prompt; p != nil && p.Prefix != "" {
		switch p.Kind {
		case "search":
			if p.Prefix != "/" && p.Prefix != "?" {
				return false
			}
		case "ex":
			if p.Prefix != ":" {
				return false
			}
		case "confirm":
			if p.Prefix != "?" {
				return false
			}
		default:
			return false
		}
	}
	if len(s.Selections) == 0 && len(s.Matches) == 0 && s.Viewport == nil {
		return true
	} // Pure legacy frames.
	if !validText(s.Text) {
		return false
	}
	lines := strings.Split(s.Text, "\n")
	if v := s.Viewport; v != nil && (v.Top < 0 || v.Top >= len(lines)) {
		return false
	}
	boundaries := make(map[int][]bool)
	valid := func(p Cursor) bool {
		if p.Line < 0 || p.Line >= len(lines) || p.Ch < 0 {
			return false
		}
		points, ok := boundaries[p.Line]
		if !ok {
			points = make([]bool, 2*len(lines[p.Line])+1)
			points[0] = true
			units := 0
			for _, r := range lines[p.Line] {
				units++
				if r > 0xffff {
					units++
				}
				points[units] = true
			}
			boundaries[p.Line] = points
		}
		return p.Ch < len(points) && points[p.Ch]
	}
	if !valid(s.Cursor) {
		return false
	}
	for _, ranges := range [][]SelectionRange{s.Selections, s.Matches} {
		for _, r := range ranges {
			if !valid(r.Anchor) || !valid(r.Head) {
				return false
			}
		}
	}
	return true
}
