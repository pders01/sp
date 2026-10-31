package tui

import (
	"errors"
	"fmt"
	"os"

	"filippo.io/age"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

var ErrUnlockCancelled = errors.New("unlock cancelled")

// UnlockOptions supplies terminal descriptors explicitly: the unlock screen must
// never fall back to /dev/tty or accept a redirected passphrase. On success the
// caller owns the unlocked key and must arrange Lock when its operation ends.
type UnlockOptions struct {
	Input, Output *os.File
	Theme, Title  string
	IsUnlocked    func() bool
	Unlock        func([]byte) error
	Lock          func()
}

type unlockScreen struct {
	app       *App
	accepted  bool // UI-owned handoff, only after the accepted job has completed.
	done      bool
	err       error
	output    *os.File
	bracketed bool
}

func newUnlockScreen(options UnlockOptions) *unlockScreen {
	cal, nb := NewCalendar(nil), NewNotebook(nil)
	cal.SetThemePref(options.Theme)
	screen := &unlockScreen{app: NewApp(cal, nb, ModeNotebook), output: options.Output}
	screen.app.SetPrivacy(PrivacyHooks{
		IsUnlocked: options.IsUnlocked, Unlock: options.Unlock, AsyncUnlock: true,
		Lock: func() {
			// Cancellation/error/shutdown always locks. A successful, completed
			// unlock deliberately transfers key ownership to the calling action.
			if !screen.accepted {
				options.Lock()
			}
		},
	})
	screen.app.privacy.unlockTitle = options.Title
	screen.app.privacy.unlockExitLabel = "cancel"
	return screen
}

func (s *unlockScreen) Init() tea.Cmd {
	if s.output != nil {
		if _, err := fmt.Fprint(s.output, "\x1b[?2004h"); err != nil {
			s.err = err
			s.done = true
			return tea.Quit
		}
		s.bracketed = true
	}
	cmd := s.app.startUnlock()
	if s.app.privacy.prompt == nil {
		s.done = true
		s.accepted = s.app.privacy.hooks.IsUnlocked()
		if !s.accepted {
			s.err = ErrUnlockCancelled
		}
		return tea.Quit
	}
	return cmd
}

func (s *unlockScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s.done {
		return s, nil
	}
	p := s.app.privacy
	if _, rejected := msg.(unlockRejectedInputMsg); rejected {
		if p.prompt != nil && p.job == nil && p.keyHelp == nil {
			p.promptInvalid = true
			p.promptErr = "Input rejected: use single-line UTF-8 without controls. Re-enter the passphrase."
		}
		return s, nil
	}
	if _, interrupted := msg.(unlockInterruptedMsg); interrupted {
		p.keyHelp = nil
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyEnter && p.prompt != nil && p.keyHelp == nil && p.job == nil && !p.promptInvalid && p.prompt.Value() == "" {
		// Preserve the CLI's empty-input cancellation; never reinterpret a
		// rejected paste as an empty secret or bypass the busy-job wait.
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	}
	var fatal error
	completed := false
	if result, ok := msg.(privateUnlockedMsg); ok && result.job != nil && result.job == p.job && !p.job.cancelRequested {
		completed = result.err == nil
		if result.err != nil {
			var mismatch *age.NoIdentityMatchError
			if !errors.As(result.err, &mismatch) {
				fatal = result.err
			}
		}
	}
	_, cmd := s.app.Update(msg)
	if fatal != nil {
		s.err = fmt.Errorf("unlock notes: %w", fatal)
		s.done = true
		return s, tea.Quit
	}
	if p.prompt == nil && p.job == nil {
		s.done = true
		if completed && p.hooks.IsUnlocked != nil && p.hooks.IsUnlocked() {
			s.accepted = true
		} else if completed {
			s.err = errors.New("unlocked session is no longer available; retry the operation")
		} else {
			s.err = ErrUnlockCancelled
		}
		return s, tea.Quit
	}
	return s, cmd
}

func (s *unlockScreen) View() string {
	if s.done {
		return ""
	}
	return s.app.View()
}

// UnlockNotes reuses the viewer's masked form, help, progress, validation and
// owned cancellation lifecycle without loading any note or starting watchers.
func UnlockNotes(options UnlockOptions) error {
	if options.Input == nil || options.Output == nil || !term.IsTerminal(int(options.Input.Fd())) || !term.IsTerminal(int(options.Output.Fd())) {
		return errors.New("private unlock requires an interactive terminal")
	}
	if options.IsUnlocked == nil || options.Unlock == nil || options.Lock == nil {
		return errors.New("private unlock requires session hooks")
	}
	if options.IsUnlocked() {
		return nil
	}
	screen := newUnlockScreen(options)
	defer screen.app.Close()
	filter := &unlockInputFilter{}
	defer filter.clear()
	defer func() {
		if screen.bracketed {
			fmt.Fprint(options.Output, "\x1b[?2004l")
		}
	}()
	result, err := tea.NewProgram(screen, tea.WithAltScreen(), tea.WithInput(options.Input), tea.WithOutput(options.Output), tea.WithFilter(filter.filter)).Run()
	if err != nil {
		screen.accepted = false
		return err
	}
	outcome := result.(*unlockScreen)
	if outcome.err != nil {
		return outcome.err
	}
	if !outcome.accepted {
		return ErrUnlockCancelled
	}
	return nil
}
