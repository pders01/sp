package editorcore

import (
	"errors"
	"strings"
	"testing"
)

func TestAdapterErrorPreservesFixedDiagnosticAndCommonClass(t *testing.T) {
	err := AdapterError("synthetic adapter: operation pending", ErrPending)
	if err.Error() != "synthetic adapter: operation pending" || !errors.Is(err, ErrPending) || errors.Is(err, ErrProtocol) {
		t.Fatal("adapter diagnostic/class changed")
	}
}

func TestSharedFrameRejectsControlAndGeometryWithoutChangingText(t *testing.T) {
	for _, text := range []string{"\x1b[31m", "\n", "\r", "\x00", "\u202e", "\ufeff", "\uffff", "\xff"} {
		frame := Frame{Width: 8, Height: 1, Lines: []string{text}}
		if SafeDisplayText(text) || frame.ANSI() != "" {
			t.Fatal("unsafe display text escaped generic frame validation")
		}
	}
	line := "A界e\u0301"
	frame := Frame{Width: 8, Height: 1, Lines: []string{line}}
	if got := frame.ANSI(); got == "" || !strings.Contains(got, line) || frame.Lines[0] != line {
		t.Fatal("generic frame normalized valid text")
	}
	for _, size := range [][2]int{{0, 1}, {241, 1}, {1, 101}} {
		if ValidGeometry(size[0], size[1]) {
			t.Fatal("unbounded geometry accepted")
		}
	}
	frame.CursorColumn = frame.Width
	if frame.ANSI() != "" {
		t.Fatal("invalid cursor emitted ANSI")
	}
}
