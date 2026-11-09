package tui

import (
	"errors"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pders01/sp/internal/scratchpad"
	"golang.org/x/term"
)

// SecretOptions supplies the only terminal descriptors used by ReadSecret.
// Title, Label and Details are public UI text and must never contain secrets.
// Label names the field (without a trailing colon); collection is not an unlock.
type SecretOptions struct {
	Input, Output                *os.File
	Theme, Title, Label, Details string
}

const secretValidationMessage = "Passphrase must contain 1 to 4096 bytes of single-line UTF-8 without controls; invalid input cleared."

// secretScreen owns only collection, never a session, store, or key callback.
type secretScreen struct {
	prompt         *privatePassphraseInput
	palette        Palette
	title, details string
	width, height  int
	keyHelp        *keyHelp
	invalid        bool // Sticky: rejected input must not submit an older prefix.
	notice         string
	done           bool
	secret         []byte // Owned here until takeResult transfers it to the caller.
}

func newSecretScreen(options SecretOptions) *secretScreen {
	title, label := options.Title, options.Label
	if title == "" {
		title = "Collect passphrase"
	}
	if label == "" {
		label = "Passphrase"
	}
	input := newPrivatePassphraseInput()
	input.Prompt = oneLine(safeMarkdownDisplay(label)) + ": "
	input.EchoCharacter = '•'
	input.CharLimit = 4097
	input.Focus()
	return &secretScreen{
		prompt: &input, palette: paletteFor(resolveGlamourStyle(options.Theme)),
		title: oneLine(safeMarkdownDisplay(title)), details: displayText(safeMarkdownDisplay(options.Details)),
	}
}

func (s *secretScreen) Init() tea.Cmd { return textinput.Blink }

func (s *secretScreen) clear() {
	if s.prompt != nil {
		s.prompt.SetValue("")
		s.prompt = nil
	}
	clear(s.secret)
	s.secret = nil
	s.keyHelp = nil
	s.invalid, s.notice = false, ""
}

func (s *secretScreen) reject() {
	s.invalid = true
	s.notice = "Input rejected; press Enter to clear invalid input and re-enter the complete passphrase."
}

func (s *secretScreen) insert(text string) {
	if text == "" {
		return
	}
	// Bound before constructing the combined candidate. No trimming, Unicode
	// normalization, sanitizer, clipboard command, or password echo toggle.
	if len(text) > 4096 || !validPrivatePassphrase(text) {
		s.reject()
		return
	}
	value := []rune(s.prompt.Value())
	defer clear(value)
	pos := s.prompt.Position()
	candidate := []byte(string(value[:pos]) + text + string(value[pos:]))
	defer clear(candidate)
	if scratchpad.ValidatePassphrase(candidate) != nil {
		s.reject()
		return
	}
	s.prompt.SetValue(string(candidate))
	s.prompt.SetCursor(pos + utf8.RuneCountInString(text))
}

func (s *secretScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s.done {
		return s, nil
	}
	if _, quit := msg.(tea.QuitMsg); quit {
		// Tea normally consumes graceful signal QuitMsg before Update. This
		// also makes explicit/model-level shutdown have the same semantics.
		s.clear()
		s.done = true
		return s, tea.Quit
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		s.width, s.height = max(0, size.Width), max(0, size.Height)
		if s.keyHelp != nil {
			s.keyHelp.resize(s.width, s.height)
		}
	}
	// Help owns every key and paste; even rejected terminal input must not
	// poison or mutate the hidden candidate while the reference is visible.
	if s.keyHelp != nil {
		if _, paste := privatePasteText(msg); paste {
			return s, nil
		}
		if key, ok := msg.(tea.KeyMsg); ok && !key.Paste && s.keyHelp.update(key) {
			s.keyHelp = nil
			return s, textinput.Blink
		}
		return s, nil
	}
	if text, paste := privatePasteText(msg); paste {
		s.insert(text)
		return s, nil
	}
	if _, rejected := msg.(terminalRejectedMsg); rejected {
		s.reject() // Never render a transport payload/reason as secret UI text.
		return s, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		if key.Paste || key.Alt {
			s.reject()
			return s, nil
		}
		switch key.Type {
		case tea.KeyF1:
			s.keyHelp = newKeyHelp(s.title, secretBindings(), s.details+"\nCollection only: no unlock or save. Do not enter passphrases into agent chats.", s.width, s.height)
			return s, nil
		case tea.KeyEsc, tea.KeyCtrlC:
			s.clear()
			s.done = true
			return s, tea.Quit
		case tea.KeyEnter:
			_, _, minimum := s.layout()
			if s.width < s.minimumWidth() || s.height < minimum {
				return s, nil
			}
			if s.invalid {
				s.prompt.SetValue("")
				s.invalid, s.notice = false, secretValidationMessage
				return s, nil
			}
			candidate := []byte(s.prompt.Value())
			if len(candidate) == 0 {
				s.clear()
				s.done = true
				return s, tea.Quit
			}
			if scratchpad.ValidatePassphrase(candidate) != nil {
				clear(candidate)
				s.prompt.SetValue("")
				s.notice = secretValidationMessage
				return s, nil
			}
			s.clear()
			s.secret, s.done = candidate, true
			return s, tea.Quit
		case tea.KeySpace:
			s.insert(" ")
			return s, nil
		case tea.KeyRunes:
			for _, r := range key.Runes {
				if !utf8.ValidRune(r) {
					s.reject()
					return s, nil
				}
			}
			s.insert(string(key.Runes))
			return s, nil
		}
	}
	var cmd tea.Cmd
	*s.prompt, cmd = s.prompt.Update(msg)
	return s, cmd
}

func secretBindings() []helpEntry {
	return []helpEntry{
		{keys: "Enter", label: "accept exact passphrase; empty input cancels", visible: true},
		{keys: "Esc/Ctrl+C", label: "cancel collection", visible: true},
		{keys: "←/→ Home/End", label: "move within masked input", visible: true},
		{keys: "Backspace/Delete", label: "delete previous/next character", visible: true},
		{keys: "Ctrl+U/Ctrl+K", label: "delete before/after cursor", visible: true},
		{keys: "F1", label: "key reference; paste is literal text, never an action", visible: true},
	}
}

// Submission and rendering share geometry, including the full label, details,
// validation notice and footer. A clipped/invisible form cannot be accepted.
func (s *secretScreen) layout() (body, footer string, minimum int) {
	wrap := func(text string) string { return ansi.Wrap(text, max(1, s.width-4), "") }
	s.prompt.Width = max(1, min(48, s.width-4)-lipgloss.Width(s.prompt.Prompt)-1)
	lines := []string{"", s.palette.Title.Render("Enter your passphrase"), "", s.prompt.View()}
	if s.notice != "" {
		lines = append(lines, "", s.palette.ErrorMessage.Render(wrap(s.notice)))
	}
	if s.details != "" {
		lines = append(lines, "", s.palette.MutedText.Render(wrap(s.details)))
	}
	lines = append(lines, "", s.palette.MutedText.Render(wrap("Collection only; nothing is unlocked or saved.")))
	body = lipgloss.NewStyle().PaddingLeft(2).Render(strings.Join(lines, "\n"))
	footer = screenFooter([]helpEntry{
		{keys: "Enter", label: "accept", visible: true},
		{keys: "Esc", label: "cancel", visible: true},
		{keys: "F1", label: "help", visible: true},
	}, s.palette, s.width)
	minimum = viewHeaderRows + viewHeight(body) + viewHeight(footer)
	return
}

func (s *secretScreen) minimumWidth() int {
	return max(24, lipgloss.Width(s.prompt.Prompt)+6)
}

func (s *secretScreen) View() string {
	if s.done {
		return ""
	}
	if s.keyHelp != nil {
		return s.keyHelp.view(s.palette)
	}
	body, footer, minimum := s.layout()
	if s.width < s.minimumWidth() || s.height < minimum {
		return smallTerminal(s.title, s.width, s.height, s.minimumWidth(), minimum, "F1: help · Esc: cancel")
	}
	return screenView(s.title, "", body, footer, s.palette, s.width, s.height)
}

func (s *secretScreen) takeResult(runErr error) ([]byte, error) {
	defer func() {
		s.clear()
		s.done = true
	}()
	if runErr != nil {
		return nil, runErr
	}
	if !s.done || len(s.secret) == 0 {
		return nil, ErrUnlockCancelled
	}
	secret := s.secret
	s.secret = nil
	return secret, nil
}

// ReadSecret collects one exact, validated passphrase without unlocking,
// loading, saving, deriving a key, or launching an editor. Empty physical Enter,
// Escape, Ctrl+C and graceful signal shutdown cancel with ErrUnlockCancelled
// (test with errors.Is). It refuses redirected descriptors and unsafe decoder
// fallbacks. The caller owns the returned bytes and should clear them promptly.
// Internal buffers/references are cleared on every exit, but Go strings, runtime
// copies and terminal infrastructure prevent any guarantee of memory erasure.
func ReadSecret(options SecretOptions) ([]byte, error) {
	if options.Input == nil || options.Output == nil || !term.IsTerminal(int(options.Input.Fd())) || !term.IsTerminal(int(options.Output.Fd())) {
		return nil, errors.New("secret collection requires an interactive terminal")
	}
	screen := newSecretScreen(options)
	defer screen.clear()
	result, err := RunTerminalProgram(screen, options.Input, tea.WithAltScreen(), tea.WithOutput(options.Output))
	if err == nil && result != screen {
		err = errors.New("secret collection ended unexpectedly")
	}
	return screen.takeResult(err)
}
