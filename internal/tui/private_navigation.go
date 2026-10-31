package tui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rivo/uniseg"
)

// MoveStart is opt-in: SetValue retains its established end-cursor contract.
func (m *privateInput) MoveStart() { m.pos = 0 }
func (m *privateInput) OpenValue(s string) {
	m.SetValue(s)
	if m.err == "" {
		m.MoveStart()
	}
}

func (m privateInput) boundary(pos int) bool {
	if pos == 0 || pos == len(m.text) {
		return true
	}
	if m.text[pos-1] == '\r' && m.text[pos] == '\n' {
		return false
	}
	start := m.lineStart(pos)
	if start == pos {
		return true
	}
	g := uniseg.NewGraphemes(m.text[start:])
	for g.Next() {
		_, to := g.Positions()
		if start+to >= pos {
			return start+to == pos
		}
	}
	return false
}

func (m *privateInput) page(up bool) {
	column := privateDisplayWidth(m.text[m.lineStart(m.pos):m.pos])
	for n := 0; n < max(1, m.height); n++ {
		before := m.pos
		m.verticalColumn(up, column)
		if m.pos == before {
			break
		}
	}
}

// Words are runs of letter/number/mark/underscore graphemes. Punctuation and
// whitespace are separators; movement always uses whole graphemes and CRLF.
func privateWordCluster(cluster string) bool {
	for _, r := range cluster {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) || r == '_' {
			return true
		}
	}
	return false
}
func (m *privateInput) wordRight() {
	start := m.pos
	g := uniseg.NewGraphemes(m.text[start:])
	separators := false
	for g.Next() {
		from, _ := g.Positions()
		if !privateWordCluster(g.Str()) {
			separators = true
		} else if separators {
			m.pos = start + from
			return
		}
	}
	m.pos = len(m.text)
}
func (m *privateInput) wordLeft() {
	// Scan once rather than calling previous for every cluster, which would
	// repeatedly rescan a long physical line and make word motion quadratic.
	g := uniseg.NewGraphemes(m.text[:m.pos])
	wordStart, inWord := 0, false
	for g.Next() {
		word := privateWordCluster(g.Str())
		if word && !inWord {
			wordStart, _ = g.Positions()
		}
		inWord = word
	}
	m.pos = wordStart
}

// PositionStatus uses one-based display columns (tabs, escaped controls, and
// wide graphemes included), and describes the same horizontal window as View.
// It adds no rows to View; the parent chooses where/how to budget this text.
func (m privateInput) PositionStatus() string {
	start := m.lineStart(m.pos)
	column := privateDisplayWidth(m.text[start:m.pos])
	width := max(1, m.width)
	offset := max(0, column-width+1)
	s := fmt.Sprintf("Ln %d/%d Col %d", strings.Count(m.text[:m.pos], "\n")+1, strings.Count(m.text, "\n")+1, column+1)
	left := offset > 0
	right := privateDisplayWidth(m.text[start:m.lineEnd(m.pos)]) > offset+width
	if left || right {
		s += fmt.Sprintf(" | Cols %d-%d", offset+1, offset+width)
		if left {
			s += " | left hidden"
		}
		if right {
			s += " | right hidden"
		}
	}
	return s
}

// Finding lets the owner route keys here before its global Ctrl+S/Esc handlers.
func (m privateInput) Finding() bool { return m.findActive }
func (m privateInput) Status() string {
	s := m.PositionStatus()
	if m.findActive || m.findQuery != "" {
		// Render queries with the same control escaping and cell limits as the
		// document, never interpolating document-originated terminal controls.
		query := strings.TrimRight(privateRenderLine(m.findQuery, -1, min(40, max(1, m.width)), 0), " ")
		if m.findActive {
			s += " | Find: " + query + " (Enter accept, Esc close)"
		} else {
			s += " | Find: " + query
		}
	}
	if m.findNotice != "" {
		s += " | " + m.findNotice
	}
	if m.err != "" {
		s += " | " + m.err
	}
	return s
}

const privateFindLimit = 4096

func (m *privateInput) findAppend(s string) {
	if !utf8.ValidString(s) || len(s) > privateFindLimit-len(m.findQuery) {
		m.findNotice = "Find query rejected (valid UTF-8, at most 4 KiB)."
		return
	}
	m.findQuery += s
	m.findNext(true)
}

func (m *privateInput) findNext(includeCurrent bool) {
	if m.findQuery == "" {
		m.findNotice = ""
		return
	}
	from := m.pos
	if !includeCurrent {
		from = m.next(from)
	}
	at := strings.Index(m.text[from:], m.findQuery)
	wrapped := false
	if at >= 0 {
		at += from
	} else {
		at = strings.Index(m.text, m.findQuery)
		wrapped = true
	}
	if at < 0 {
		m.findNotice = "No match in current draft."
		return
	}
	// Literal matches may begin inside a cluster (e.g. a combining mark).
	// Place the cursor at that cluster's start, never inside UTF-8 or CRLF.
	m.pos = m.lineStart(at)
	for m.pos < at {
		next := m.next(m.pos)
		if next > at {
			break
		}
		m.pos = next
	}
	m.findNotice = "Match in current draft."
	if wrapped {
		m.findNotice = "Match in current draft (wrapped)."
	}
}

func (m *privateInput) updateFind(msg tea.Msg) {
	if text, ok := privatePasteText(msg); ok {
		m.findAppend(text)
		return
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return
	}
	switch key.Type {
	case tea.KeyRunes:
		for _, r := range key.Runes {
			if !utf8.ValidRune(r) {
				m.findNotice = "Find query rejected (invalid UTF-8)."
				return
			}
		}
		m.findAppend(string(key.Runes))
	case tea.KeySpace:
		m.findAppend(" ")
	case tea.KeyTab:
		m.findAppend("\t")
	case tea.KeyBackspace, tea.KeyCtrlH:
		g := uniseg.NewGraphemes(m.findQuery)
		last := 0
		for g.Next() {
			last, _ = g.Positions()
		}
		m.findQuery = m.findQuery[:last]
		m.findNext(true)
	case tea.KeyCtrlU:
		m.findQuery, m.findNotice = "", ""
	case tea.KeyF3:
		m.findNext(false)
	case tea.KeyEnter:
		m.findNext(true)
		m.findActive = false
	case tea.KeyEsc:
		m.findActive = false
	}
}
