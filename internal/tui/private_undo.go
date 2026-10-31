package tui

import (
	"strings"
	"unicode/utf8"
)

// History is private, memory-only, and bounded by both payload and entry count.
// Deltas avoid retaining a full 8 MiB document for every typed character. Clone
// each payload so a tiny deletion cannot retain a large backing document.
const (
	privateUndoBytes = 16 << 20
	privateUndoSteps = 256
)

type privateEdit struct {
	from           int
	removed, added string
	before, after  int
}

type privateHistory struct {
	edits []privateEdit
	index int // number of edits currently applied
	bytes int
}

func (m *privateInput) remember(edit privateEdit) {
	// Copy metadata because Update receives its model by value. In particular,
	// branching after undo must not overwrite a previous model's redo entries.
	h := privateHistory{edits: append([]privateEdit(nil), m.history.edits[:m.history.index]...), index: m.history.index}
	for _, e := range h.edits {
		h.bytes += len(e.removed) + len(e.added)
	}
	edit.removed, edit.added = strings.Clone(edit.removed), strings.Clone(edit.added)
	h.edits = append(h.edits, edit)
	h.bytes += len(edit.removed) + len(edit.added)
	for len(h.edits) > privateUndoSteps || h.bytes > privateUndoBytes {
		e := h.edits[0]
		h.bytes -= len(e.removed) + len(e.added)
		h.edits[0] = privateEdit{}
		h.edits = h.edits[1:]
	}
	h.index = len(h.edits)
	m.history = h
}

func (m *privateInput) undo() {
	if m.history.index == 0 {
		return
	}
	e := m.history.edits[m.history.index-1]
	m.text = m.text[:e.from] + e.removed + m.text[e.from+len(e.added):]
	m.pos = e.before
	m.history.index--
	m.err, m.findNotice = "", ""
}

func (m *privateInput) redo() {
	if m.history.index == len(m.history.edits) {
		return
	}
	e := m.history.edits[m.history.index]
	m.text = m.text[:e.from] + e.added + m.text[e.from+len(e.removed):]
	m.pos = e.after
	m.history.index++
	m.err, m.findNotice = "", ""
}

// replace validates before changing either the draft or its history. Rejected
// and empty edits also leave the redo branch intact.
func (m *privateInput) replace(from, to int, s string) {
	if from < 0 || to < from || to > len(m.text) || !m.boundary(from) || !m.boundary(to) {
		m.err = "Invalid private Markdown edit; input rejected."
		return
	}
	if len(s) > privateInputLimit-(len(m.text)-(to-from)) {
		m.err = "Private Markdown must not exceed 8 MiB; input rejected."
		return
	}
	// Validate only the inserted bytes: existing text and edit boundaries are
	// already valid, and assembling a rejected large draft is unnecessary.
	if !utf8.ValidString(s) {
		m.err = "Private Markdown must be valid UTF-8; input rejected."
		return
	}
	if m.text[from:to] == s {
		return
	}
	e := privateEdit{from: from, removed: m.text[from:to], added: s, before: m.pos}
	m.text = m.text[:from] + s + m.text[to:]
	m.pos = from + len(s)
	m.snapCursor()
	e.after = m.pos
	m.remember(e)
	m.err, m.findNotice = "", ""
}
