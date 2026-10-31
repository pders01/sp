package tui

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rivo/uniseg"
)

// Match the private store's plaintext limit. Rejected edits leave the previous
// buffer intact; there is no line-count limit or newline/tab normalization.
const privateInputLimit = 8 << 20

// privateInput is a literal, horizontally scrolling Markdown editor. Positions
// are byte offsets into the original UTF-8, never into a normalized projection.
// CRLF and grapheme clusters are single navigation/deletion units.
// It intentionally has no system clipboard, file, or external process commands.
type privateInput struct {
	text          string
	pos           int
	width, height int
	focused       bool
	err           string
	history       privateHistory
	findActive    bool
	findQuery     string
	findNotice    string
}

func newPrivateInput() privateInput  { return privateInput{width: 80, height: 18} }
func (m privateInput) Value() string { return m.text }
func (m *privateInput) SetValue(s string) {
	if !m.accept(s) {
		return
	}
	m.text, m.pos = s, len(s)
	m.history = privateHistory{}
	m.findActive, m.findQuery, m.findNotice = false, "", ""
}
func (m *privateInput) SetWidth(w int)  { m.width = max(1, w) }
func (m *privateInput) SetHeight(h int) { m.height = max(1, h) }
func (m *privateInput) Focus() tea.Cmd  { m.focused = true; return nil }

func (m *privateInput) accept(s string) bool {
	switch {
	case len(s) > privateInputLimit:
		m.err = "Private Markdown must not exceed 8 MiB; input rejected."
	case !utf8.ValidString(s):
		m.err = "Private Markdown must be valid UTF-8; input rejected."
	default:
		m.err = ""
		return true
	}
	return false
}

func (m privateInput) lineStart(pos int) int {
	return strings.LastIndexByte(m.text[:pos], '\n') + 1
}
func (m privateInput) lineEnd(pos int) int {
	end := strings.IndexByte(m.text[pos:], '\n')
	if end < 0 {
		return len(m.text)
	}
	end += pos
	if end > 0 && m.text[end-1] == '\r' {
		end--
	}
	return end
}
func (m privateInput) previous(pos int) int {
	start := m.lineStart(pos)
	if pos == start {
		if start > 1 && m.text[start-2] == '\r' {
			return start - 2
		}
		return max(0, start-1)
	}
	g := uniseg.NewGraphemes(m.text[start:pos])
	previous := start
	for g.Next() {
		from, _ := g.Positions()
		previous = start + from
	}
	return previous
}
func (m privateInput) next(pos int) int {
	if pos == len(m.text) {
		return pos
	}
	if strings.HasPrefix(m.text[pos:], "\r\n") {
		return pos + 2
	}
	g := uniseg.NewGraphemes(m.text[pos:])
	g.Next()
	_, to := g.Positions()
	return pos + to
}
func (m *privateInput) insert(s string) { m.replace(m.pos, m.pos, s) }

// An insertion/deletion can join adjacent graphemes (or form a CRLF). Keep
// subsequent operations from landing inside the newly joined unit.
func (m *privateInput) snapCursor() {
	if m.pos > 0 && m.pos < len(m.text) && m.text[m.pos-1] == '\r' && m.text[m.pos] == '\n' {
		m.pos++
		return
	}
	start := m.lineStart(m.pos)
	if start == m.pos {
		return
	}
	g := uniseg.NewGraphemes(m.text[start:])
	for g.Next() {
		_, to := g.Positions()
		if start+to >= m.pos {
			m.pos = start + to
			return
		}
	}
}
func (m *privateInput) erase(from, to int) { m.replace(from, to, "") }

func (m *privateInput) vertical(up bool) {
	m.verticalColumn(up, privateDisplayWidth(m.text[m.lineStart(m.pos):m.pos]))
}

func (m *privateInput) verticalColumn(up bool, column int) {
	start := m.lineStart(m.pos)
	var target int
	if up {
		if start == 0 {
			return
		}
		target = m.lineStart(start - 1)
	} else {
		end := strings.IndexByte(m.text[m.pos:], '\n')
		if end < 0 {
			return
		}
		target = m.pos + end + 1
	}
	end := m.lineEnd(target)
	m.pos = target
	col := 0
	g := uniseg.NewGraphemes(m.text[target:end])
	for g.Next() {
		_, to := g.Positions()
		_, w := privateDisplayCluster(g.Str(), col)
		if col+w > column {
			break
		}
		col += w
		m.pos = target + to
	}
}

func (m privateInput) Update(msg tea.Msg) (privateInput, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	if m.findActive {
		m.updateFind(msg)
		return m, nil
	}
	if text, ok := privatePasteText(msg); ok {
		m.insert(text)
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.Type {
	case tea.KeyCtrlZ:
		m.undo()
	case tea.KeyCtrlY:
		m.redo()
	case tea.KeyCtrlF:
		m.findActive = true
		m.findNext(true)
	case tea.KeyF3:
		m.findNext(false)
	case tea.KeyCtrlHome:
		m.MoveStart()
	case tea.KeyCtrlEnd:
		m.pos = len(m.text)
	case tea.KeyCtrlLeft:
		m.wordLeft()
	case tea.KeyCtrlRight:
		m.wordRight()
	case tea.KeyPgUp:
		m.page(true)
	case tea.KeyPgDown:
		m.page(false)
	case tea.KeyRunes:
		for _, r := range key.Runes {
			if !utf8.ValidRune(r) {
				m.err = "Private Markdown must be valid UTF-8; input rejected."
				return m, nil
			}
		}
		m.insert(string(key.Runes))
	case tea.KeySpace:
		m.insert(" ")
	case tea.KeyTab:
		m.insert("\t")
	case tea.KeyEnter:
		newline := "\n"
		// Use the current line's ending, or the document's first ending on
		// the final line. Existing mixed endings are never rewritten.
		end := m.lineEnd(m.pos)
		if strings.HasPrefix(m.text[end:], "\r\n") || (end == len(m.text) && strings.IndexByte(m.text, '\n') > 0 && m.text[strings.IndexByte(m.text, '\n')-1] == '\r') {
			newline = "\r\n"
		}
		m.insert(newline)
	case tea.KeyLeft:
		m.pos = m.previous(m.pos)
	case tea.KeyRight:
		m.pos = m.next(m.pos)
	case tea.KeyHome, tea.KeyCtrlA:
		m.pos = m.lineStart(m.pos)
	case tea.KeyEnd, tea.KeyCtrlE:
		m.pos = m.lineEnd(m.pos)
	case tea.KeyUp:
		m.vertical(true)
	case tea.KeyDown:
		m.vertical(false)
	case tea.KeyBackspace, tea.KeyCtrlH:
		m.erase(m.previous(m.pos), m.pos)
	case tea.KeyDelete, tea.KeyCtrlD:
		m.erase(m.pos, m.next(m.pos))
	case tea.KeyCtrlK:
		end := m.lineEnd(m.pos)
		if end == m.pos {
			end = m.next(end)
		}
		m.erase(m.pos, end)
	case tea.KeyCtrlU:
		m.erase(m.lineStart(m.pos), m.pos)
	}
	return m, nil
}

// privatePassphraseInput uses Bubbles only to render/blink a masked projection.
// Even valid U+FFFD is dropped by older Bubbles sanitizers, so the actual secret
// must never pass through SetValue or Update on textinput.Model. Rune positions
// in this single-line prompt are independent of document byte positions.
type privatePassphraseInput struct {
	textinput.Model
	secret []rune
}

func newPrivatePassphraseInput() privatePassphraseInput {
	m := privatePassphraseInput{Model: textinput.New()}
	m.EchoMode = textinput.EchoPassword
	m.KeyMap.Paste.SetEnabled(false)
	return m
}
func (m privatePassphraseInput) Value() string { return string(m.secret) }
func (m *privatePassphraseInput) SetValue(s string) {
	pos := m.Position()
	wasEmpty := len(m.secret) == 0
	clear(m.secret)
	m.secret = []rune(s)
	if wasEmpty || pos > len(m.secret) {
		pos = len(m.secret)
	}
	m.syncMask(pos)
}
func (m *privatePassphraseInput) syncMask(pos int) {
	m.EchoMode = textinput.EchoPassword
	m.Model.SetValue(strings.Repeat("x", len(m.secret)))
	m.SetCursor(pos)
}
func (m privatePassphraseInput) View() string {
	m.EchoMode = textinput.EchoPassword
	return m.Model.View()
}
func (m privatePassphraseInput) Update(msg tea.Msg) (privatePassphraseInput, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		// Only blink/focus messages reach Bubbles. Async paste is consumed
		// and validated by updatePrivacy before this method is called.
		if _, paste := privatePasteText(msg); !paste {
			var cmd tea.Cmd
			m.Model, cmd = m.Model.Update(msg)
			m.EchoMode = textinput.EchoPassword
			return m, cmd
		}
		return m, nil
	}
	pos := m.Position()
	from, to := pos, pos
	switch key.Type {
	case tea.KeyHome, tea.KeyCtrlA:
		pos = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		pos = len(m.secret)
	case tea.KeyLeft, tea.KeyCtrlB:
		pos = max(0, pos-1)
		if key.Alt {
			pos = 0
		}
	case tea.KeyRight, tea.KeyCtrlF:
		pos = min(len(m.secret), pos+1)
		if key.Alt {
			pos = len(m.secret)
		}
	case tea.KeyBackspace, tea.KeyCtrlH:
		from = max(0, pos-1)
		if key.Alt {
			from = 0
		}
	case tea.KeyDelete, tea.KeyCtrlD:
		to = min(len(m.secret), pos+1)
		if key.Alt {
			to = len(m.secret)
		}
	case tea.KeyCtrlU, tea.KeyCtrlW:
		from = 0
	case tea.KeyCtrlK:
		to = len(m.secret)
	}
	if from != to {
		copy(m.secret[from:], m.secret[to:])
		clear(m.secret[len(m.secret)-(to-from):])
		m.secret = m.secret[:len(m.secret)-(to-from)]
		pos = from
	}
	m.syncMask(pos)
	return m, nil
}

// privatePasteMsg is the internal raw asynchronous paste event, also useful to
// supply synthetic payloads without invoking a system clipboard command.
type privatePasteMsg string

// Bubbles 0.18 calls its asynchronous clipboard result pasteMsg; newer versions
// export PasteMsg. Recognize only that package's string message, before its
// sanitizer can turn an invalid secret into a different one. No clipboard
// command is ever invoked by this editor or by the unlock prompt.
func privatePasteText(msg tea.Msg) (string, bool) {
	if text, ok := msg.(privatePasteMsg); ok {
		return string(text), true
	}
	v := reflect.ValueOf(msg)
	if !v.IsValid() || v.Kind() != reflect.String {
		return "", false
	}
	t := v.Type()
	if t.PkgPath() == "github.com/charmbracelet/bubbles/textinput" && (t.Name() == "pasteMsg" || t.Name() == "PasteMsg") {
		return v.String(), true
	}
	return "", false
}

// All document-originated terminal controls (including bidi format controls)
// are escaped. Only fixed cursor styling below can introduce ANSI sequences.
func privateDisplayCluster(cluster string, column int) (display string, width int) {
	if cluster == "\t" {
		w := 4 - column%4
		return strings.Repeat(" ", w), w
	}
	var safe strings.Builder
	for _, r := range cluster {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			fmt.Fprintf(&safe, "\\u%04x", r)
		} else {
			safe.WriteRune(r)
		}
	}
	s := safe.String()
	w := uniseg.StringWidth(s)
	if w == 0 {
		s = "◌" + s
		w = max(1, uniseg.StringWidth(s))
	}
	return s, w
}
func privateDisplayWidth(s string) int {
	col := 0
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		_, w := privateDisplayCluster(g.Str(), col)
		col += w
	}
	return col
}

func privateRenderLine(line string, cursor, width, offset int) string {
	var b strings.Builder
	col, used := 0, 0
	g := uniseg.NewGraphemes(line)
	for g.Next() {
		from, to := g.Positions()
		s, w := privateDisplayCluster(g.Str(), col)
		left, right := max(col, offset), min(col+w, offset+width)
		if right > left {
			// Never emit half a wide grapheme or control escape.
			if left != col || right != col+w {
				s = strings.Repeat(" ", right-left)
			}
			if cursor >= from && cursor < to {
				b.WriteString("\x1b[7m" + s + "\x1b[0m")
			} else {
				b.WriteString(s)
			}
			used += right - left
		}
		col += w
		if col >= offset+width {
			break
		}
	}
	if cursor == len(line) && used < width {
		b.WriteString("\x1b[7m \x1b[0m")
		used++
	}
	b.WriteString(strings.Repeat(" ", max(0, width-used)))
	return b.String()
}

func (m privateInput) View() string {
	width, height := max(1, m.width), max(1, m.height)
	start := m.lineStart(m.pos)
	column := privateDisplayWidth(m.text[start:m.pos])
	offset := max(0, column-width+1)
	for n := 1; n < height && start > 0; n++ {
		start = m.lineStart(start - 1)
	}
	var rows []string
	for n := 0; n < height; n++ {
		end := m.lineEnd(start)
		cursor := -1
		if m.pos >= start && m.pos <= end {
			cursor = m.pos - start
		}
		rows = append(rows, privateRenderLine(m.text[start:end], cursor, width, offset))
		next := strings.IndexByte(m.text[start:], '\n')
		if next < 0 {
			break
		}
		start += next + 1
	}
	return strings.Join(rows, "\n")
}
