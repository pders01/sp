package embeddedvim

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSelectionMetadataBounds(t *testing.T) {
	base := Snapshot{Text: "a🙂\ne\u0301", Mode: "visual", SelectionMode: "char", Selections: []SelectionRange{{Anchor: Cursor{0, 0}, Head: Cursor{0, 3}}}}
	if !validSelectionSnapshot(base) {
		t.Fatal("valid synthetic selection metadata rejected")
	}
	for _, mutate := range []func(*Snapshot){
		func(s *Snapshot) { s.SelectionMode = "private\x1b" },
		func(s *Snapshot) { s.Cursor.Ch = 2 },
		func(s *Snapshot) { s.Selections = nil },
		func(s *Snapshot) { s.SelectionMode = "line"; s.Selections[0].Head.Ch = -1 },
		func(s *Snapshot) { s.Mode = "normal" },
		func(s *Snapshot) { s.SelectionMode = "" },
		func(s *Snapshot) { s.Selections[0].Head.Ch = 2 }, // Surrogate gap.
		func(s *Snapshot) { s.Selections[0].Head.Ch = 4 },
		func(s *Snapshot) { s.Selections[0].Head.Line = 2 },
		func(s *Snapshot) { s.Selections[0].Anchor.Ch = -1 },
		func(s *Snapshot) { s.Selections = make([]SelectionRange, MaxSelections+1) },
	} {
		s := clone(base)
		mutate(&s)
		if validSelectionSnapshot(s) {
			t.Fatal("invalid synthetic selection metadata accepted")
		}
	}
	line := clone(base)
	line.SelectionMode = "line"
	line.Selections = append(line.Selections, line.Selections[0])
	if validSelectionSnapshot(line) {
		t.Fatal("multiple inclusive line ranges accepted")
	}
	for _, kind := range []string{"char", "line", "block"} {
		s := clone(base)
		s.SelectionMode = kind
		s.Selections[0].Anchor, s.Selections[0].Head = s.Selections[0].Head, s.Selections[0].Anchor
		if !validSelectionSnapshot(s) {
			t.Fatal("reversed selection was rejected")
		}
	}
	copy := clone(base)
	copy.Selections[0].Head.Ch = 0
	if base.Selections[0].Head.Ch != 3 {
		t.Fatal("selection snapshot clone aliases owner")
	}
}

func TestSessionRejectsInvalidSelectionsWithoutPromotingAcknowledgement(t *testing.T) {
	for _, bad := range []SelectionRange{
		{Anchor: Cursor{0, 0}, Head: Cursor{0, 1}}, // Split emoji.
		{Anchor: Cursor{0, 0}, Head: Cursor{1, 0}},
	} {
		s := openProof(t, "🙂", Options{})
		before := s.Snapshot()
		payload := Snapshot{Text: "🙂", Mode: "visual", SelectionMode: "char", Selections: []SelectionRange{bad}}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal("synthetic metadata encoding failed")
		}
		_, err = s.EvaluateForTest(context.Background(), `(()=>{const value=`+string(data)+`;globalThis.proof={keys(){return value}};return true})()`)
		if err != nil {
			t.Fatal("synthetic metadata injection failed")
		}
		if _, err := s.Keys(context.Background()); !errors.Is(err, ErrSnapshot) || !s.Faulted() || !reflect.DeepEqual(before, s.Snapshot()) {
			t.Fatal("invalid selection metadata advanced or escaped acknowledgement")
		}
	}
}

func TestVisualSelectionModeIndicators(t *testing.T) {
	for _, mode := range []string{"char", "line", "block"} {
		s := Snapshot{Text: "abc", Mode: "visual", SelectionMode: mode, Selections: []SelectionRange{{Head: Cursor{0, 1}}}}
		var v Viewport
		f, err := v.Render(s, 40, 3)
		if err != nil {
			t.Fatal("synthetic visual mode frame rejected")
		}
		label := "VISUAL"
		if mode != "char" {
			label += " " + strings.ToUpper(mode)
		}
		if !strings.HasPrefix(f.Lines[2], label) {
			t.Fatal("visual subtype indicator missing")
		}
	}
}

func TestWasmVisualSelectionsReachSafeCopiedFrames(t *testing.T) {
	u := openSyntheticUI(t, "abc\nwxyz\nlast")
	uiType(t, u, "vl")
	uiText(t, u, "abc\nwxyz\nlast")
	s := u.session.Snapshot()
	if s.SelectionMode != "char" || len(s.Selections) != 1 || s.Cursor != (Cursor{0, 1}) {
		t.Fatal("visual caret or metadata differs from engine head")
	}
	f := u.Frame()
	if len(f.Selections) != 1 || f.Selections[0].End-f.Selections[0].Start != 2 || !strings.Contains(f.ANSI(), "\x1b[7m") {
		t.Fatal("visual range did not reach styled safe frame")
	}
	original := f.Selections[0]
	f.Selections[0].Start++
	if u.Frame().Selections[0] != original {
		t.Fatal("mutable frame selections alias UI owner")
	}
	s.Selections[0].Anchor.Ch++
	if u.session.Snapshot().Selections[0].Anchor == s.Selections[0].Anchor {
		t.Fatal("mutable snapshot selections alias session owner")
	}
	uiKey(t, u, "Escape")
	if len(u.Frame().Selections) != 0 {
		t.Fatal("visual Escape left stale highlights")
	}
	uiType(t, u, "gg0Vj")
	f = u.Frame()
	if len(f.Selections) != 2 || f.Selections[0].Row != 0 || f.Selections[1].Row != 1 || f.Selections[0].End != f.Width {
		t.Fatal("linewise visual selection did not cover selected document rows")
	}
	uiKey(t, u, "Cancel")
	a := uiAction(t, u, "cancel")
	if _, err := u.Resume(context.Background(), a.Snapshot); err != nil {
		t.Fatal("visual cancellation could not resume")
	}
	if len(u.Frame().Selections) != 0 {
		t.Fatal("resume retained visual highlights")
	}
	uiType(t, u, "iX")
	uiText(t, u, "abc\nXwxyz\nlast") // No hidden CM selection may replace the old range.
}

func TestWasmBlockSelectionAndHighlightResizePreserveText(t *testing.T) {
	u := openSyntheticUI(t, "abcdef\nghijkl\nmnopqr")
	uiKey(t, u, "CtrlV")
	uiType(t, u, "lj")
	if u.session.Snapshot().SelectionMode != "block" || len(u.Frame().Selections) != 2 {
		t.Fatal("block ranges were not highlighted")
	}
	if err := u.Resize(context.Background(), 12, 4); err != nil {
		t.Fatal("highlight resize failed")
	}
	f := u.Frame()
	if f.ANSI() == "" || len(f.Selections) != 2 {
		t.Fatal("resize discarded or invalidated block highlights")
	}
	for _, span := range f.Selections {
		if span.Start < 0 || span.End > f.Width || span.Row >= f.Height-1 {
			t.Fatal("highlight escaped resized document cells")
		}
	}
	uiText(t, u, "abcdef\nghijkl\nmnopqr")
	uiKey(t, u, "Escape")
	if len(u.Frame().Selections) != 0 {
		t.Fatal("block Escape left stale highlights")
	}
}
