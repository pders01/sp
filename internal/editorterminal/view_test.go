package editorterminal

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pders01/sp/internal/editorcore"
)

func assertCanvas(t *testing.T, view string, width, height int) []string {
	t.Helper()
	rows := strings.Split(view, "\n")
	if len(rows) != height {
		t.Fatal("canvas row count changed")
	}
	for _, row := range rows {
		if ansi.StringWidth(row) != width {
			t.Fatal("canvas did not overwrite the complete row")
		}
	}
	return rows
}

func TestModalViewHasNoPersistentChromeOrPerKeyBusyFlash(t *testing.T) {
	m := fixtureModel()
	_, previousHeight := m.geometry()
	m.forwardEditorControls = true
	w, h := m.geometry()
	if w != 79 || h != previousHeight+4 {
		t.Fatal("modal chrome still reserves document rows")
	}
	idle := m.View()
	rows := assertCanvas(t, idle, 79, 24)
	if !strings.Contains(ansi.Strip(rows[0]), testPrivate) || strings.TrimSpace(rows[23]) != "" || strings.Contains(idle, "Vim demo") || strings.Contains(idle, "F2") || strings.Contains(idle, "Private files/normal") {
		t.Fatal("persistent header/help survived or document no longer starts at the top")
	}
	m.busy = true // A normal input operation stays in editing phase.
	if m.View() != idle {
		t.Fatal("ordinary keystroke flashed a busy footer")
	}
	m.phase = "working"
	if !strings.Contains(m.View(), "draft remains owned") {
		t.Fatal("lifecycle work hid ownership feedback")
	}
}

func TestMinimalNoticeUsesModeRowAndPreservesActivePrompt(t *testing.T) {
	m := fixtureModel()
	m.forwardEditorControls = true
	m.width, m.height = 40, 10
	lines := make([]string, 10)
	lines[0], lines[8], lines[9] = testPrivate, "NORMAL [demo]", ":unfinished"
	statusRow := 8
	m.editor = &selectionFrameEditor{fakeEditor: fixtureEditor(), frame: editorcore.Frame{
		Width: 39, Height: 10, Lines: lines, StatusRow: &statusRow, CursorRow: 9, CursorColumn: 11,
	}}
	m.status = "Operation failed; draft remains owned. R retry/resolve, C encrypted cancel, K known copies."
	rows := assertCanvas(t, m.View(), 39, 10)
	prose := strings.Join(strings.Fields(strings.Join(rows[6:9], " ")), " ")
	if !strings.Contains(prose, "K known copies.") || !strings.HasPrefix(ansi.Strip(rows[9]), ":unfinished") || !strings.Contains(rows[0], testPrivate) {
		t.Fatal("on-demand notice hid choices, prompt or unrelated document rows")
	}
	m.status = ""
	rows = assertCanvas(t, m.View(), 39, 10)
	if !strings.HasPrefix(rows[8], "NORMAL [demo]") || !strings.HasPrefix(ansi.Strip(rows[9]), ":unfinished") {
		t.Fatal("mode/prompt did not return without new chrome")
	}
}

func TestSharedViewsDoNotClaimApplicationNotesAreSynthetic(t *testing.T) {
	m := fixtureModel()
	for _, modal := range []bool{false, true} {
		m.forwardEditorControls = modal
		for _, phase := range []string{"editing", "confirm", "known-confirm"} {
			m.phase = phase
			view := strings.ToLower(m.View())
			if strings.Contains(view, "synthetic") || strings.Contains(view, "no real notes") || strings.Contains(view, "no notebook updated") {
				t.Fatal("shared application view falsely claimed disposable data")
			}
		}
		m.width, m.height = 20, 5
		if strings.Contains(strings.ToLower(m.View()), "synthetic") {
			t.Fatal("small application view inherited a demo claim")
		}
		m.width, m.height = 80, 24
	}
}

func TestCanvasOverwritesShorterFooterWithoutRelyingOnEraseControls(t *testing.T) {
	m := fixtureModel()
	m.forwardEditorControls = true
	m.status = strings.Repeat("L", 120)
	before := assertCanvas(t, m.View(), 79, 24)
	m.status = "Ready"
	after := assertCanvas(t, m.View(), 79, 24)
	// Simulate prefix-only line writes, with no terminal erase-to-end support.
	old := []byte(ansi.Strip(before[23]))
	copy(old, ansi.Strip(after[23]))
	if strings.TrimRight(string(old), " ") != "Ready" {
		t.Fatal("old footer tail survived a shortened update")
	}
}

func TestCanvasModalAndTinyTransitionsClearPriorContentWithinBounds(t *testing.T) {
	m := fixtureModel()
	m.forwardEditorControls = true
	for _, phase := range []string{"confirm", "known-confirm"} {
		m.phase = phase
		view := m.View()
		assertCanvas(t, view, 79, 24)
		if strings.Contains(view, testPrivate) || !strings.Contains(view, "Physical Y") {
			t.Fatal("modal exposed stale document or hid physical consent")
		}
	}
	m.width, m.height = 20, 5
	view := m.View()
	assertCanvas(t, view, 19, 5)
	if strings.Contains(view, testPrivate) {
		t.Fatal("tiny view exposed prior document")
	}
	m.phase, m.width, m.height = "editing", 10000, 10000
	assertCanvas(t, m.View(), editorcore.MaxColumns, editorcore.MaxRows+4)
}

func TestModalSuccessfulInputDismissesOnlyTransientHint(t *testing.T) {
	for _, paused := range []bool{false, true} {
		m := fixtureModel()
		m.forwardEditorControls = true
		m.status = "Editing resumed; old queued keys discarded."
		m.inputPaused = paused
		m.finished(jobMsg{kind: "input"})
		if (m.status != "") != paused {
			t.Fatal("transient hint remained or unsettled-input warning was dismissed")
		}
	}
}

// Override only the synthetic frame; all lifecycle methods remain the fake's.
type selectionFrameEditor struct {
	*fakeEditor
	frame editorcore.Frame
}

func (e *selectionFrameEditor) Frame() editorcore.Frame { return e.frame }

func TestFrameCursorDoesNotSplitWideOrCombiningGraphemes(t *testing.T) {
	line := "  e\u0301界🙂👩\u200d💻  "
	m := fixtureModel()
	e := &selectionFrameEditor{fakeEditor: fixtureEditor(), frame: editorcore.Frame{Width: 39, Height: 1, Lines: []string{line}}}
	m.editor = e
	for column := 0; column < ansi.StringWidth(line); column++ {
		e.frame.CursorColumn = column
		rows := assertCanvas(t, m.View(), 79, 24)
		if strings.TrimRight(ansi.Strip(rows[2]), " ") != strings.TrimRight(line, " ") || !strings.Contains(rows[2], "\x1b[7m") {
			t.Fatal("cursor painting changed graphemes or omitted the cursor")
		}
	}
}

func TestViewConsumesSelectionStylesWithoutAbsoluteCursorControls(t *testing.T) {
	for _, compact := range []bool{false, true} {
		m := fixtureModel()
		m.forwardEditorControls = compact
		e := &selectionFrameEditor{fakeEditor: fixtureEditor(), frame: editorcore.Frame{
			Width: 39, Height: 2, CursorColumn: 2, Lines: []string{"abcdef", "next"},
			Selections: []editorcore.CellSpan{{Row: 0, Start: 1, End: 5}},
		}}
		m.editor = e
		bodyRow := 2
		if compact {
			bodyRow = 0
		}
		for _, phase := range []string{"editing", "working"} {
			m.phase = phase
			view := m.View()
			rows := assertCanvas(t, view, 79, 24)
			want := e.frame.StyledLines(phase == "editing")[0]
			if !strings.HasPrefix(rows[bodyRow], want) || !strings.HasPrefix(rows[bodyRow+1], "next") || strings.Contains(rows[bodyRow+1], "\x1b[") {
				t.Fatal("view lost selection/cursor composition or leaked styles into the next row")
			}
			if !strings.Contains(rows[bodyRow], "\x1b[7m") || regexp.MustCompile(`\x1b\[\d+;\d+H`).MatchString(view) || strings.Contains(view, "\x1b[2K") || strings.Contains(view, "\x1b[?25") {
				t.Fatal("view omitted selections or embedded Frame.ANSI terminal controls")
			}
			if strings.Contains(rows[bodyRow], "\x1b[4;7m") != (phase == "editing") {
				t.Fatal("view cursor visibility did not follow editing phase")
			}
		}
	}
}

func TestViewRejectsWholeInvalidSelectionFrame(t *testing.T) {
	m := fixtureModel()
	e := &selectionFrameEditor{fakeEditor: fixtureEditor(), frame: editorcore.Frame{
		Width: 39, Height: 2, Lines: []string{testPrivate, "界"},
		Selections: []editorcore.CellSpan{{Row: 1, Start: 0, End: 1}},
	}}
	m.editor = e
	if view := m.View(); strings.Contains(view, testPrivate) || strings.Contains(view, "界") {
		t.Fatal("invalid selection exposed part of the rejected frame")
	}
	e.frame.Selections = []editorcore.CellSpan{{Row: 0, Start: 0, End: 1}}
	e.frame.Lines[1] = "\x1b[31munsafe"
	if view := m.View(); strings.Contains(view, testPrivate) || strings.Contains(view, "unsafe") || strings.Contains(view, "\x1b[31m") {
		t.Fatal("selection bypassed unsafe frame text rejection")
	}
}

func TestViewPadsShortRowCursorOnlyWhileEditing(t *testing.T) {
	m := fixtureModel()
	m.editor = &selectionFrameEditor{fakeEditor: fixtureEditor(), frame: editorcore.Frame{
		Width: 39, Height: 1, CursorColumn: 7, Lines: []string{"a"},
	}}
	if row := assertCanvas(t, m.View(), 79, 24)[2]; !strings.HasPrefix(row, "a      \x1b[7m \x1b[0m") {
		t.Fatal("view omitted cursor on a short row")
	}
	m.phase = "working"
	if row := assertCanvas(t, m.View(), 79, 24)[2]; strings.Contains(row, "\x1b[") {
		t.Fatal("nonediting view retained a fake cursor")
	}
}
