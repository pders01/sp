package editorcore

import "testing"

func TestFrameStatusRowIsOptionalBoundedMetadata(t *testing.T) {
	row := 1
	f := Frame{Width: 4, Height: 2, Lines: []string{"text", "mode"}, StatusRow: &row}
	if f.StyledLines(false) == nil || f.ANSI() == "" {
		t.Fatal("valid status slot rejected")
	}
	for _, bad := range []int{-1, 2, 10000} {
		row = bad
		if f.StyledLines(false) != nil || f.ANSI() != "" {
			t.Fatal("out-of-frame status slot accepted")
		}
	}
	f.StatusRow = nil
	if f.StyledLines(false) == nil {
		t.Fatal("legacy frame requires status metadata")
	}
}
