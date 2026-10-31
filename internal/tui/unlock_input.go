package tui

import (
	"reflect"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

type unlockRejectedInputMsg struct{}
type unlockInterruptedMsg struct{}

// The pinned Tea decoder predates bracketed paste and emits valid U+FFFD as
// unknown bytes. Recover those bytes, and keep an entire terminal paste literal
// until validation: pasted CR/Tab must never become submission/editing keys.
// This adapter is scoped to the standalone secret form, not document editing.
type unlockInputFilter struct {
	pasting bool
	invalid bool
	paste   []byte
	pending []byte
}

func (f *unlockInputFilter) clear() {
	clear(f.paste)
	clear(f.pending)
	f.paste, f.pending = nil, nil
	f.pasting, f.invalid = false, false
}

func (f *unlockInputFilter) appendPaste(data []byte) {
	if len(data) > 4096-len(f.paste) {
		f.invalid = true
		return
	}
	f.paste = append(f.paste, data...)
}

func (f *unlockInputFilter) unknownByte(b byte) tea.Msg {
	if f.pasting {
		f.appendPaste([]byte{b})
		return nil
	}
	f.pending = append(f.pending, b)
	if !utf8.FullRune(f.pending) {
		return nil
	}
	valid := utf8.Valid(f.pending)
	text := string(f.pending)
	clear(f.pending)
	f.pending = nil
	if !valid {
		return unlockRejectedInputMsg{}
	}
	return privatePasteMsg(text)
}

func (f *unlockInputFilter) sequence(data []byte) tea.Msg {
	switch string(data) {
	case "\x1b[200~":
		if f.pasting {
			f.invalid = true
			return nil
		}
		incomplete := len(f.pending) > 0
		f.clear()
		f.pasting = true
		f.invalid = incomplete
		return nil
	case "\x1b[201~":
		if !f.pasting {
			return unlockRejectedInputMsg{}
		}
		valid := !f.invalid && utf8.Valid(f.paste) && validPrivatePassphrase(string(f.paste))
		text := string(f.paste)
		f.clear()
		if !valid {
			return unlockRejectedInputMsg{}
		}
		return privatePasteMsg(text)
	default:
		if f.pasting {
			f.invalid = true
			return nil
		}
		return unlockRejectedInputMsg{}
	}
}

func (f *unlockInputFilter) filter(model tea.Model, msg tea.Msg) tea.Msg {
	if _, quit := msg.(tea.QuitMsg); quit {
		if screen, ok := model.(*unlockScreen); ok && !screen.done {
			f.clear()
			return unlockInterruptedMsg{}
		}
		return msg
	}
	v := reflect.ValueOf(msg)
	if v.IsValid() && v.Type().PkgPath() == "github.com/charmbracelet/bubbletea" {
		switch v.Type().Name() {
		case "unknownInputByteMsg":
			if v.Kind() == reflect.Uint8 {
				return f.unknownByte(byte(v.Uint()))
			}
		case "unknownCSISequenceMsg":
			if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
				return f.sequence(v.Bytes())
			}
		}
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return msg
	}
	if f.pasting {
		if key.Type == tea.KeyCtrlC && !key.Alt {
			f.clear()
			return unlockInterruptedMsg{}
		}
		switch {
		case key.Alt:
			f.invalid = true
		case key.Type == tea.KeyRunes:
			f.appendPaste([]byte(string(key.Runes)))
		case key.Type == tea.KeySpace:
			f.appendPaste([]byte{' '})
		case key.Type >= 0 && key.Type <= 31 || key.Type == 127:
			f.appendPaste([]byte{byte(key.Type)})
		default:
			f.invalid = true
		}
		return nil
	}
	if len(f.pending) > 0 {
		f.clear()
		return unlockRejectedInputMsg{}
	}
	return msg
}
