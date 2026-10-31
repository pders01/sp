package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rivo/uniseg"
)

// All content is synthetic and stays in memory. No returned command is run.
func uxKey(kind tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: kind} }
func uxText(s string) tea.KeyMsg        { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func uxUpdate(m privateInput, msg tea.Msg) privateInput {
	m, _ = m.Update(msg)
	return m
}
func uxInput(s string) privateInput {
	m := newPrivateInput()
	m.Focus()
	m.SetValue(s)
	return m
}
func uxCursor(t *testing.T, m privateInput) {
	t.Helper()
	if m.pos < 0 || m.pos > len(m.text) || !utf8.ValidString(m.text[:m.pos]) || !m.boundary(m.pos) {
		t.Fatalf("invalid cursor %d in %q", m.pos, m.text)
	}
}

func TestPrivateInputUXLosslessUndoRedo(t *testing.T) {
	original := "\talpha e\u0301👩‍💻\r\nsecond\nlast\t  \r\n\r\n"
	m := uxInput(original)
	type snapshot struct {
		text string
		pos  int
	}
	var before, after []snapshot
	for _, msg := range []tea.Msg{
		uxKey(tea.KeyCtrlHome), uxText("新增🙂"), uxKey(tea.KeyBackspace),
		uxKey(tea.KeyTab), uxKey(tea.KeyEnter), uxText("e"), uxText("\u0301"),
		uxKey(tea.KeyBackspace), uxKey(tea.KeyCtrlEnd), uxKey(tea.KeyBackspace),
		privatePasteMsg("\tctrl+z\r\ntrailing  \n\n"),
	} {
		b := snapshot{m.text, m.pos}
		m = uxUpdate(m, msg)
		uxCursor(t, m)
		if b.text != m.text {
			before = append(before, b)
			after = append(after, snapshot{m.text, m.pos})
		}
	}
	for i := len(before) - 1; i >= 0; i-- {
		m = uxUpdate(m, uxKey(tea.KeyCtrlZ))
		if m.text != before[i].text || m.pos != before[i].pos {
			t.Fatalf("undo %d changed bytes/cursor: %q @ %d", i, m.text, m.pos)
		}
		uxCursor(t, m)
	}
	if m.Value() != original {
		t.Fatal("undo did not recover original bytes")
	}
	for i := range after {
		m = uxUpdate(m, uxKey(tea.KeyCtrlY))
		if m.text != after[i].text || m.pos != after[i].pos {
			t.Fatalf("redo %d changed bytes/cursor", i)
		}
		uxCursor(t, m)
	}
}

func TestPrivateInputUXJoinedUnitsUndoRedo(t *testing.T) {
	for _, tc := range []struct {
		original string
		pos      int
		insert   string
	}{
		{"eX", 1, "\u0301"}, {"\rX", 1, "\n"}, {"\nX", 0, "\r"},
		{"👩💻X", len("👩"), "\u200d"},
	} {
		m := uxInput(tc.original)
		m.pos = tc.pos
		m = uxUpdate(m, uxText(tc.insert))
		uxCursor(t, m)
		text, pos := m.text, m.pos
		m = uxUpdate(m, uxKey(tea.KeyCtrlZ))
		if m.text != tc.original || m.pos != tc.pos {
			t.Fatalf("join undo failed: %q @ %d", m.text, m.pos)
		}
		m = uxUpdate(m, uxKey(tea.KeyCtrlY))
		if m.text != text || m.pos != pos {
			t.Fatal("join redo failed")
		}
		uxCursor(t, m)
	}
}

func TestPrivateInputUXRejectedEditsKeepHistory(t *testing.T) {
	m := uxInput("e\u0301\r\n🙂\t  ")
	m = uxUpdate(m, uxText("edit"))
	m = uxUpdate(m, uxKey(tea.KeyCtrlZ))
	text, pos, history := m.text, m.pos, m.history
	for _, attempt := range []func(){
		func() { m.insert("\xff") },
		func() { m.insert(strings.Repeat("x", privateInputLimit)) },
		func() { m.SetValue("bad\xff") },
		func() { m.OpenValue("bad\xff") },
		func() { m.erase(-1, 0) },
		func() { m.erase(1, 2) }, // inside a combining grapheme
		func() { m.erase(len("e\u0301\r"), len("e\u0301\r\n")) },
		func() { m.erase(0, len(m.text)+1) },
	} {
		attempt()
		if m.err == "" || m.text != text || m.pos != pos || !reflect.DeepEqual(m.history, history) {
			t.Fatal("rejected edit changed cursor, bytes, or history")
		}
	}
	m = uxUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x', -1}})
	if m.text != text || !reflect.DeepEqual(m.history, history) {
		t.Fatal("invalid rune batch changed history")
	}
	m = uxUpdate(m, uxKey(tea.KeyCtrlY))
	if m.text != text+"edit" || m.err != "" {
		t.Fatal("rejected edit destroyed redo")
	}
	m = uxUpdate(m, uxKey(tea.KeyCtrlZ))
	m = uxUpdate(m, uxText("branch"))
	branched := m.text
	m = uxUpdate(m, uxKey(tea.KeyCtrlY))
	if m.text != branched {
		t.Fatal("new edit failed to drop redo branch")
	}
}

func TestPrivateInputUXNoOpAndIndependentBranches(t *testing.T) {
	m := uxInput("base")
	m.insert("first")
	m.insert("second")
	m.undo()
	old := m
	history := m.history
	m.insert("")
	m.erase(m.pos, m.pos)
	if !reflect.DeepEqual(m.history, history) {
		t.Fatal("no-op edit changed undo/redo history")
	}
	m.insert("branch")
	m.redo()
	if m.text != "basefirstbranch" {
		t.Fatal("redo survived a branched edit")
	}
	old.redo()
	if old.text != "basefirstsecond" {
		t.Fatal("branching mutated a prior value model's history")
	}
}

func TestPrivateInputUXBoundedHistory(t *testing.T) {
	m := uxInput("")
	for n := 0; n < privateUndoSteps+25; n++ {
		m = uxUpdate(m, uxText("x"))
	}
	if len(m.history.edits) != privateUndoSteps || m.history.bytes > privateUndoBytes {
		t.Fatal("history entry limit not enforced")
	}
	for n := 0; n < privateUndoSteps+10; n++ {
		m = uxUpdate(m, uxKey(tea.KeyCtrlZ))
	}
	if m.text != strings.Repeat("x", 25) {
		t.Fatal("bounded undo did not stop at oldest retained edit")
	}
	m.SetValue("")
	large := strings.Repeat("x", privateInputLimit)
	m.insert(large)
	m.erase(0, len(m.text))
	m.insert(large)
	if m.history.bytes > privateUndoBytes || len(m.history.edits) != 2 {
		t.Fatal("history byte limit not enforced")
	}
	m.undo()
	if m.text != "" {
		t.Fatal("bounded large undo was lossy")
	}
	m.undo()
	if m.text != large {
		t.Fatal("bounded large deletion was lossy")
	}
}

func TestPrivateInputUXSetValueAndOpening(t *testing.T) {
	const raw = "\t笔🙂\r\n\r\n  \t"
	m := uxInput(raw)
	if m.pos != len(raw) {
		t.Fatal("SetValue lost established end-cursor contract")
	}
	m.MoveStart()
	if m.pos != 0 || m.Value() != raw {
		t.Fatal("MoveStart changed bytes")
	}
	m.insert("edit")
	m.OpenValue(raw)
	if m.pos != 0 || m.Value() != raw || len(m.history.edits) != 0 {
		t.Fatal("OpenValue did not open a fresh, exact draft at start")
	}
	m.undo()
	if m.Value() != raw {
		t.Fatal("history leaked across SetValue/OpenValue")
	}
}

func TestPrivateInputUXPhysicalNavigation(t *testing.T) {
	m := uxInput("one,\te\u0301_二🙂\r\nlast  \r\n")
	original := m.Value()
	m = uxUpdate(m, uxKey(tea.KeyCtrlHome))
	for _, want := range []int{len("one,\t"), len("one,\te\u0301_二🙂\r\n"), len(original)} {
		m = uxUpdate(m, uxKey(tea.KeyCtrlRight))
		if m.pos != want {
			t.Fatalf("word right: got %d want %d", m.pos, want)
		}
		uxCursor(t, m)
	}
	for _, want := range []int{len("one,\te\u0301_二🙂\r\n"), len("one,\t"), 0} {
		m = uxUpdate(m, uxKey(tea.KeyCtrlLeft))
		if m.pos != want {
			t.Fatalf("word left: got %d want %d", m.pos, want)
		}
		uxCursor(t, m)
	}
	m = uxUpdate(m, uxKey(tea.KeyEnd))
	if m.pos != len("one,\te\u0301_二🙂") {
		t.Fatal("End lost line semantics")
	}
	m = uxUpdate(m, uxKey(tea.KeyHome))
	if m.pos != 0 {
		t.Fatal("Home lost line semantics")
	}
	m = uxUpdate(m, uxKey(tea.KeyCtrlEnd))
	if m.pos != len(original) || m.Value() != original || len(m.history.edits) != 0 {
		t.Fatal("document navigation changed bytes/history")
	}
	m.SetValue("12345678\r\nx\r\n\t笔🙂z\r\nlast")
	m.SetHeight(2)
	m.MoveStart()
	m.pos = 8
	m = uxUpdate(m, uxKey(tea.KeyPgDown))
	if m.pos != len("12345678\r\nx\r\n\t笔🙂") {
		t.Fatalf("PageDown lost visual column across short line: %d", m.pos)
	}
	m = uxUpdate(m, uxKey(tea.KeyPgUp))
	if m.pos != 8 {
		t.Fatal("PageUp failed to return to original visual column")
	}
	uxCursor(t, m)
}

func TestPrivateInputUXPositionAndBoundedView(t *testing.T) {
	m := uxInput("\t笔🙂e\u0301\x1b[31m\r\n" + strings.Repeat("long 笔🙂\n", 40))
	m.MoveStart()
	m.SetWidth(3)
	if !strings.Contains(m.PositionStatus(), "Ln 1/42 Col 1") || !strings.Contains(m.Status(), "right hidden") {
		t.Fatalf("missing initial orientation: %q", m.Status())
	}
	m = uxUpdate(m, uxKey(tea.KeyRight))
	if !strings.Contains(m.PositionStatus(), "Col 5") || !strings.Contains(m.Status(), "left hidden") {
		t.Fatalf("missing tab/horizontal orientation: %q", m.Status())
	}
	for _, width := range []int{1, 2, 3, 8, 80} {
		for _, height := range []int{1, 2, 5} {
			m.SetWidth(width)
			m.SetHeight(height)
			for _, kind := range []tea.KeyType{tea.KeyCtrlHome, tea.KeyEnd, tea.KeyPgDown, tea.KeyCtrlEnd, tea.KeyPgUp} {
				m = uxUpdate(m, uxKey(kind))
				plain := strings.NewReplacer("\x1b[7m", "", "\x1b[0m", "").Replace(m.View())
				rows := strings.Split(plain, "\n")
				if len(rows) > height || strings.ContainsAny(plain, "\x1b\r") {
					t.Fatalf("unsafe or over-height view at %dx%d", width, height)
				}
				for _, row := range rows {
					if uniseg.StringWidth(row) > width {
						t.Fatalf("over-width view: %q", row)
					}
				}
			}
		}
	}
}

func TestPrivateInputUXDraftOnlyFind(t *testing.T) {
	const raw = "first needle\r\n\t笔 needle\r\nlast  \n"
	m := uxInput(raw)
	m.MoveStart()
	m = uxUpdate(m, uxKey(tea.KeyCtrlF))
	if !m.Finding() || !strings.Contains(m.Status(), "Find:") {
		t.Fatal("Ctrl+F did not expose find prompt")
	}
	m = uxUpdate(m, uxText("needle"))
	if m.pos != len("first ") || m.Value() != raw || len(m.history.edits) != 0 {
		t.Fatal("find changed draft/history or missed first match")
	}
	m = uxUpdate(m, uxKey(tea.KeyCtrlS))
	if !m.Finding() || m.Value() != raw {
		t.Fatal("find did not consume save binding")
	}
	m = uxUpdate(m, uxKey(tea.KeyF3))
	if m.pos != len("first needle\r\n\t笔 ") {
		t.Fatal("F3 missed next literal match")
	}
	m = uxUpdate(m, uxKey(tea.KeyEnter))
	if m.Finding() {
		t.Fatal("Enter did not accept find")
	}
	m = uxUpdate(m, uxKey(tea.KeyF3))
	if m.pos != len("first ") || !strings.Contains(m.Status(), "wrapped") {
		t.Fatal("F3 did not wrap within current draft")
	}
	m = uxUpdate(m, uxKey(tea.KeyCtrlF))
	m = uxUpdate(m, uxKey(tea.KeyCtrlU))
	m = uxUpdate(m, privatePasteMsg("not in this draft"))
	if !strings.Contains(m.Status(), "No match") || m.Value() != raw {
		t.Fatal("missing-match find changed draft")
	}
	m = uxUpdate(m, uxKey(tea.KeyEsc))
	if m.Finding() || m.Value() != raw {
		t.Fatal("Esc did not exclusively close find")
	}
	m.SetValue("replacement needle")
	if m.findQuery != "" || m.Finding() {
		t.Fatal("query leaked into another opened draft")
	}
}

func TestPrivateInputUXFindLiteralAndSafe(t *testing.T) {
	for _, query := range []string{"ctrl+s", "\t", "\r\n", "\u0301", "👩‍💻", "\x1b[31m"} {
		t.Run(fmt.Sprintf("%q", query), func(t *testing.T) {
			m := uxInput("prefix e\u0301\tctrl+s\r\n👩‍💻\x1b[31m suffix")
			m.MoveStart()
			original := m.Value()
			m = uxUpdate(m, uxKey(tea.KeyCtrlF))
			m = uxUpdate(m, privatePasteMsg(query))
			if m.findQuery != query || !strings.Contains(m.Status(), "Match in current draft") || m.Value() != original {
				t.Fatalf("literal query failed: %q", m.Status())
			}
			uxCursor(t, m)
			if strings.ContainsAny(m.Status(), "\x1b\r\n\t") {
				t.Fatal("query injected terminal controls into status")
			}
			m = uxUpdate(m, uxKey(tea.KeyBackspace))
			if query == "👩‍💻" && m.findQuery != "" {
				t.Fatal("find backspace split a grapheme")
			}
		})
	}
	m := uxInput("draft")
	m = uxUpdate(m, uxKey(tea.KeyCtrlF))
	for _, bad := range []tea.Msg{privatePasteMsg("bad\xff"), privatePasteMsg(strings.Repeat("q", privateFindLimit+1)), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{-1}}} {
		m = uxUpdate(m, bad)
		if m.findQuery != "" || m.Value() != "draft" || len(m.history.edits) != 0 {
			t.Fatal("invalid query changed state")
		}
	}
}
