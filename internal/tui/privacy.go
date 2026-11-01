package tui

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pders01/sp/internal/scratchpad"
	"golang.org/x/term"
)

// PrivatePage separates display-only shared content from editable private text.
// Locked pages must never contain private plaintext.
type PrivatePage struct {
	Private    string
	Shared     string
	Locked     bool
	HasPrivate bool
	LoadError  string   // A failed load is neither an empty note nor a locked note.
	Applied    []string // Private template metadata, populated only after unlock.
}

// PublicPage contains only deliberately shared text and public presence metadata.
// Inspecting it must never decrypt or return private writing.
type PublicPage struct {
	Shared     string
	HasPrivate bool
}

// PrivacyHooks keeps key handling and persistence outside the UI. Inspect can
// populate public projections; Load reads only the active private page.
type PrivacyHooks struct {
	Inspect    func(string) (PublicPage, error)
	IsUnlocked func() bool
	Unlock     func([]byte) error
	Lock       func()
	// AsyncUnlock runs the expensive KDF off the UI thread. Hooks that rely
	// on synchronous updates can leave this false.
	AsyncUnlock bool
	Load        func(string) (PrivatePage, error)
	Save        Saver
	Recover     func(date, content string) (string, error)
}

type privacyState struct {
	hooks           PrivacyHooks
	prompt          *privatePassphraseInput
	promptErr       string
	unlockTitle     string // Optional standalone context; never contains a secret.
	unlockExitLabel string
	promptInvalid   bool // Sticky until submission/cancellation; never sanitize a secret.
	editAfter       string
	templateAfter   string
	job             *privateUnlockJob
	closed          bool
	editor          *privateEditor
	keyHelp         *keyHelp
}

// SetPrivacy switches editing to the in-memory editor. No external editor or
// plaintext temporary Markdown is involved in this mode.
func (a *App) SetPrivacy(hooks PrivacyHooks) {
	// A failed reconfiguration must leave the running editor usable, not turn
	// it into a closed session with the only unsealed copy trapped in memory.
	if a.privacy != nil && !a.privacy.closed {
		if err := a.sealPrivacyDraft(); err != nil {
			a.setPrivacyStatus(err.Error())
			return
		}
	}
	if err := a.closePrivacy(); err != nil {
		a.setPrivacyStatus(err.Error())
		return
	}
	a.privacy = &privacyState{hooks: hooks}
	a.installPrivacyIndicators()
	a.nb.privacyEnabled = true
	a.cal.privacyEnabled = true
	// Defense in depth: do not leave an external editing path wired.
	a.nb.editor, a.nb.save, a.nb.loader = nil, nil, nil
	a.cal.editor, a.cal.save, a.cal.loader = nil, nil, nil
	a.nb.editorProvider, a.cal.editorProvider = nil, nil
	for date := range a.cal.hasData {
		// Without an inspector, treat presence conservatively rather than
		// claiming a known date has no entry. Production hooks inspect it.
		a.cal.privateDates[date] = true
		a.refreshPublicPage(date)
	}
	if a.mode == ModeNotebook {
		a.refreshPrivatePage()
	}
}

func (a *App) refreshPublicPage(date string) {
	if a.privacy.hooks.Inspect == nil {
		return
	}
	page, err := a.privacy.hooks.Inspect(date)
	if err != nil {
		a.cal.loadErrors[date] = err.Error()
		return
	}
	delete(a.cal.loadErrors, date)
	a.cal.privateDates[date] = page.HasPrivate
	a.cal.MarkDate(date, page.Shared)
	a.nb.contents[date] = page.Shared
	a.nb.updateViewportContent()
}

func (a *App) privateLoadFailed(date string, err error) {
	a.cal.loadErrors[date] = err.Error()
	if a.nb.GetCurrentPage() == date {
		a.nb.privatePage = &PrivatePage{LoadError: err.Error()}
		a.nb.privateDate = date
		a.nb.updateViewportContent()
	}
	a.setPrivacyStatus(fmt.Sprintf("load: %v", err))
}

func (a *App) refreshPrivatePage() {
	if a.privacy == nil || a.mode != ModeNotebook || a.privacy.hooks.Load == nil {
		return
	}
	date := a.nb.GetCurrentPage()
	if date == "" {
		return
	}
	page, err := a.privacy.hooks.Load(date)
	if err != nil {
		// Never leave the previously decrypted page visible after a load error.
		a.privateLoadFailed(date, err)
		return
	}
	delete(a.cal.loadErrors, date)
	a.cal.privateDates[date] = page.HasPrivate || page.Private != ""
	a.nb.privatePage, a.nb.privateDate = &page, date
	if !page.Locked {
		a.setPrivateApplied(date, page.Applied)
	}
	a.nb.updateViewportContent()
}

func (a *App) setPrivateApplied(date string, ids []string) {
	if a.appliedTemplates == nil {
		a.appliedTemplates = make(map[string]map[string]bool)
	}
	a.appliedTemplates[date] = make(map[string]bool, len(ids))
	for _, id := range ids {
		a.appliedTemplates[date][id] = true
	}
}

// RecoverPrivateEdit preserves changed text after an abnormal TUI shutdown. The
// returned message includes the encrypted path; nil means no unsaved private text.
func (a *App) RecoverPrivateEdit() error {
	if a.privacy == nil || a.privacy.editor == nil {
		return nil
	}
	e := a.privacy.editor
	if e.done || !e.needsRecovery() {
		return nil
	}
	path, err := e.recovery()
	if err != nil {
		return privateRecoveryError(path, err)
	}
	return fmt.Errorf("unsaved private edit preserved in encrypted recovery: %s", path)
}

func (a *App) privateDate() string {
	if a.mode == ModeCalendar {
		return a.cal.CursorDate()
	}
	return a.nb.GetCurrentPage()
}

func (a *App) startPrivateEdit(date string) tea.Cmd {
	if a.privacy.closed || a.privacy.job != nil || date == "" || a.privacy.hooks.Load == nil {
		return nil
	}
	page, err := a.privacy.hooks.Load(date)
	if err != nil {
		a.privateLoadFailed(date, err)
		return nil
	}
	if a.mode == ModeNotebook && a.nb.GetCurrentPage() == date {
		a.nb.privatePage, a.nb.privateDate = &page, date
		a.nb.updateViewportContent()
	}
	if page.Locked {
		a.privacy.editAfter = date
		return a.startUnlock()
	}
	a.privacy.editor = newPrivateEditor(date, page.Private, a.privacy.hooks.Save, a.privacy.hooks.Recover)
	a.privacy.editor.onRecovery = a.trackRecovery
	a.privacy.editor.theme = a.nb.theme
	a.privacy.editor.savePolicy = "Saving encrypted"
	a.privacy.editor.canRelock = a.privacy.hooks.Lock != nil
	a.privacy.editor.load = func(date string) (string, error) {
		page, err := a.privacy.hooks.Load(date)
		if err != nil {
			return "", err
		}
		if page.Locked {
			return "", scratchpad.ErrLocked
		}
		return page.Private, nil
	}
	a.privacy.editor.resize(a.nb.width, a.nb.height)
	return a.privacy.editor.Init()
}

func (a *App) startUnlock() tea.Cmd {
	if a.privacy.closed || a.privacy.job != nil {
		return nil
	}
	if a.privacy.hooks.IsUnlocked != nil && a.privacy.hooks.IsUnlocked() {
		a.setPrivacyStatus("Private notes are unlocked for this SP process.")
		return nil
	}
	input := newPrivatePassphraseInput()
	input.Prompt = "Passphrase: "
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '•'
	// Raw input is validated and byte-bounded before reaching textinput's
	// sanitizer. Clipboard commands are deliberately disabled (bracketed
	// terminal paste still arrives as literal runes).
	input.CharLimit = 4097
	input.KeyMap.Paste.SetEnabled(false)
	input.Width = max(1, a.nb.width-lipgloss.Width(input.Prompt)-1)
	input.Focus()
	a.privacy.prompt = &input
	a.privacy.keyHelp = nil
	a.privacy.promptErr = ""
	a.privacy.promptInvalid = false
	return textinput.Blink
}

func validPrivatePassphrase(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// Insert literal text ourselves: Bubbles' string-based key bindings and rune
// sanitizer must never reinterpret batched runes or rewrite a passphrase.
func (p *privacyState) insertPassphrase(text string) {
	p.prompt.EchoMode = textinput.EchoPassword
	if len(text) > 4096 || !validPrivatePassphrase(text) || len(p.prompt.Value())+len(text) > 4096 {
		p.promptInvalid = true
		p.promptErr = "Passphrase must contain 1 to 4096 bytes of single-line UTF-8 without controls; press Enter to clear invalid input."
		return
	}
	value := []rune(p.prompt.Value())
	pos := p.prompt.Position()
	p.prompt.SetValue(string(value[:pos]) + text + string(value[pos:]))
	p.prompt.SetCursor(pos + utf8.RuneCountInString(text))
}

func (a *App) setPrivacyStatus(text string) {
	a.cal.theme.SetStatus(text, 0)
	a.nb.theme.SetStatus(text, 0)
}

// A job is owned from acceptance, not from execution of a returned tea.Cmd.
// result is buffered so shutdown can wait without running a command or draining
// the UI message queue. Only the UI goroutine accesses cancellation/frame state.
type privateUnlockJob struct {
	result          chan privateUnlockedMsg
	done            chan struct{}
	cancelRequested bool
	frame           int
}

type privateUnlockedMsg struct {
	job *privateUnlockJob
	err error
}

type privateUnlockTickMsg struct{ job *privateUnlockJob }

func privateUnlockTick(job *privateUnlockJob) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return privateUnlockTickMsg{job: job}
	})
}

func (p *privacyState) clearIntent() {
	p.editAfter, p.templateAfter = "", ""
}

func (a *App) launchPrivateUnlock(secret []byte) tea.Cmd {
	p := a.privacy
	job := &privateUnlockJob{result: make(chan privateUnlockedMsg, 1), done: make(chan struct{})}
	p.job, p.promptErr = job, ""
	unlock := p.hooks.Unlock
	go func() {
		err := unlock(secret)
		clear(secret)
		job.result <- privateUnlockedMsg{job: job, err: err}
		close(job.done)
	}()
	wait := func() tea.Msg {
		<-job.done
		return <-job.result
	}
	return tea.Batch(wait, privateUnlockTick(job))
}

func (a *App) finishPrivateUnlock(err error) tea.Cmd {
	p := a.privacy
	if err != nil {
		p.promptErr = fmt.Sprintf("Unlock failed: %v", err)
		return nil
	}
	p.prompt, p.keyHelp = nil, nil
	p.promptErr, p.promptInvalid = "", false
	a.refreshPrivatePage()
	date, templateDate := p.editAfter, p.templateAfter
	p.clearIntent()
	if date != "" {
		return a.startPrivateEdit(date)
	}
	if templateDate != "" {
		if p.hooks.Load == nil {
			a.setPrivacyStatus("Private template metadata is unavailable; retry templates after reloading.")
			return nil
		}
		page, err := p.hooks.Load(templateDate)
		if err != nil {
			a.setPrivacyStatus("Cannot load private template metadata: " + err.Error())
			return nil
		}
		if page.Locked {
			a.setPrivacyStatus("Private template metadata is locked; unlock and retry templates.")
			return nil
		}
		a.setPrivateApplied(templateDate, page.Applied)
		// Resolution stays lazy, but the date belongs to the original action,
		// not whichever page may now be active.
		a.openTemplateChooser()
		if a.templateChooser != nil {
			a.templateChooser.date = templateDate
		}
		return nil
	}
	a.setPrivacyStatus("Private notes unlocked for this SP process.")
	return nil
}

// updatePrivacySession can also be called by the parent router before chooser
// routing. Read-only help/review and the editor's find field own their keys.
func (a *App) updatePrivacySession(msg tea.Msg) (bool, tea.Cmd) {
	p := a.privacy
	if p == nil {
		return false, nil
	}
	if result, ok := msg.(privateUnlockedMsg); ok {
		if p.closed || p.job != result.job || result.job == nil {
			return true, nil
		}
		cancelled := p.job.cancelRequested
		p.job = nil
		if cancelled {
			return true, a.lockPrivacy()
		}
		return true, a.finishPrivateUnlock(result.err)
	}
	if tick, ok := msg.(privateUnlockTickMsg); ok {
		if !p.closed && p.job != nil && p.job == tick.job {
			p.job.frame++
			return true, privateUnlockTick(p.job)
		}
		return true, nil
	}
	if p.closed {
		return true, nil
	}
	if p.job != nil {
		if key, ok := msg.(tea.KeyMsg); ok && (key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC || key.Type == tea.KeyCtrlL) {
			p.job.cancelRequested = true
			p.clearIntent()
		}
		return true, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok || key.Type != tea.KeyCtrlL || key.Alt {
		return false, nil
	}
	if p.keyHelp != nil || (p.editor != nil && (p.editor.keyHelp != nil || p.editor.review != nil || p.editor.input.Finding())) ||
		a.cal.keyHelp != nil || a.cal.review != nil || a.nb.keyHelp != nil || a.nb.review != nil ||
		(a.nb.sharedActive() && a.nb.shared.help != nil) ||
		(a.templateChooser != nil && a.templateChooser.keyHelp != nil) {
		return false, nil
	}
	return true, a.lockPrivacy()
}

func (a *App) sealPrivacyDraft() error {
	p := a.privacy
	if p.editor == nil || p.editor.done || !p.editor.needsRecovery() {
		return nil
	}
	path, err := p.editor.recovery()
	if err != nil {
		return privateRecoveryError(path, err)
	}
	return nil
}

// clearPrivateProjections drops references; it does not promise secure erasure
// of Go strings, terminal scrollback, or memory previously held by callers.
func (a *App) clearPrivateProjections(discardEditor bool) {
	p := a.privacy
	if p.prompt != nil {
		p.prompt.SetValue("")
	}
	p.prompt, p.keyHelp = nil, nil
	p.promptErr, p.promptInvalid = "", false
	p.clearIntent()
	if discardEditor {
		p.editor = nil
	}
	a.appliedTemplates = make(map[string]map[string]bool)
	a.templateChooser = nil
	a.cal.keyHelp, a.cal.review = nil, nil
	a.nb.keyHelp, a.nb.review = nil, nil
	date := a.nb.GetCurrentPage()
	page := &PrivatePage{Locked: true, Shared: a.nb.contents[date], HasPrivate: a.cal.privateDates[date]}
	if a.nb.privatePage != nil && a.nb.privateDate == date {
		page.Shared = a.nb.privatePage.Shared
		page.HasPrivate = page.HasPrivate || a.nb.privatePage.HasPrivate || a.nb.privatePage.Private != ""
	}
	a.nb.privatePage, a.nb.privateDate = page, date
	a.nb.updateViewportContent()
}

func (a *App) lockPrivacy() tea.Cmd {
	p := a.privacy
	if p.job != nil {
		p.job.cancelRequested = true
		p.clearIntent()
		return nil
	}
	if a.templateChooser != nil && a.templateChooser.applying {
		message := "Wait for template application to finish or cancel it before locking."
		a.templateChooser.feedback = message
		a.setPrivacyStatus(message)
		return nil
	}
	if p.hooks.Lock == nil {
		a.setPrivacyStatus("Locking is unavailable for this session.")
		return nil
	}
	if err := a.sealPrivacyDraft(); err != nil {
		message := fmt.Sprintf("Cannot lock: %v. Draft retained in the editor.", err)
		p.editor.status = message
		a.setPrivacyStatus(message)
		return nil
	}
	p.hooks.Lock()
	a.clearPrivateProjections(true)
	a.setPrivacyStatus("Private notes locked. Press u to unlock.")
	return nil
}

// closePrivacy waits even if the returned unlock command was never started.
// Failed recovery retains the editor object for the caller's recovery/reporting
// path, but keys are always locked before shutdown can complete.
func (a *App) closePrivacy() error {
	p := a.privacy
	if p == nil || p.closed && p.job == nil && p.editor == nil {
		return nil
	}
	p.closed = true
	p.clearIntent()
	if p.job != nil {
		p.job.cancelRequested = true
		<-p.job.done
		p.job = nil
	}
	err := a.sealPrivacyDraft()
	if p.hooks.Lock != nil {
		p.hooks.Lock()
	}
	a.clearPrivateProjections(err == nil)
	return err
}

// updatePrivacy handles only privacy-specific keys and modal UI. Returning
// handled=false delegates normal navigation to the existing router.
func (a *App) updatePrivacy(msg tea.Msg) (handled bool, cmd tea.Cmd) {
	p := a.privacy
	if p == nil {
		return false, nil
	}
	if handled, cmd := a.updatePrivacySession(msg); handled {
		return true, cmd
	}
	if p.editor != nil {
		e := p.editor
		_, cmd = e.Update(msg)
		if e.published {
			a.noteChanged(e.date, "")
		}
		if e.done {
			switch {
			case e.err != nil:
				a.setPrivacyStatus(e.err.Error())
			case e.outcome == editSaved:
				a.setPrivacyStatus("Private note saved.")
			case e.outcome == editCancelled:
				a.setPrivacyStatus("Edit cancelled. No changes.")
			default:
				a.setPrivacyStatus("No changes to save.")
			}
			p.editor = nil
			if !e.published {
				a.refreshPrivatePage()
			}
			return true, nil
		}
		return true, cmd
	}
	if p.prompt != nil {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			p.prompt.Width = max(1, size.Width-lipgloss.Width(p.prompt.Prompt)-1)
			p.prompt.SetCursor(p.prompt.Position())
			if p.keyHelp != nil {
				p.keyHelp.resize(size.Width, size.Height)
			}
		}
		if p.keyHelp != nil {
			if key, ok := msg.(tea.KeyMsg); ok && p.keyHelp.update(key) {
				p.keyHelp = nil
				return true, textinput.Blink
			}
			return true, nil
		}
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.Type {
			case tea.KeyF1:
				details := p.promptErr + "\nUnlock lasts for this process. Do not enter the passphrase into an agent chat."
				if p.unlockExitLabel == "cancel" {
					details += "\nEscape or an empty submission cancels this operation without loading a note."
				}
				p.keyHelp = newKeyHelp(a.unlockTitle(), unlockBindings(), details, a.nb.width, a.nb.height)
				return true, nil
			case tea.KeyEsc, tea.KeyCtrlC:
				p.prompt.SetValue("")
				p.prompt = nil
				p.clearIntent()
				p.promptErr, p.promptInvalid = "", false
				return true, nil
			case tea.KeySpace:
				p.insertPassphrase(" ")
				return true, nil
			case tea.KeyEnter:
				_, _, minimum := a.unlockLayout()
				if a.nb.width < 24 || a.nb.height < minimum {
					return true, nil
				}
				secret := []byte(p.prompt.Value())
				p.prompt.SetValue("")
				invalid := p.promptInvalid
				p.promptInvalid = false
				if invalid || len(secret) == 0 || len(secret) > 4096 || !validPrivatePassphrase(string(secret)) {
					clear(secret)
					p.promptErr = "Passphrase must contain 1 to 4096 bytes of single-line UTF-8 without controls; invalid input cleared."
					return true, nil
				}
				if p.hooks.Unlock == nil {
					clear(secret)
					p.promptErr = "Unlock is unavailable."
					return true, nil
				}
				if p.hooks.AsyncUnlock {
					if p.hooks.Lock == nil {
						clear(secret)
						p.promptErr = "Async unlock requires a session lock hook."
						return true, nil
					}
					return true, a.launchPrivateUnlock(secret)
				}
				err := p.hooks.Unlock(secret)
				clear(secret)
				return true, a.finishPrivateUnlock(err)
			}
			if key.Type == tea.KeyRunes {
				for _, r := range key.Runes {
					if !utf8.ValidRune(r) {
						p.promptInvalid = true
						return true, nil
					}
				}
				p.insertPassphrase(string(key.Runes))
				return true, nil
			}
		}
		if text, ok := privatePasteText(msg); ok {
			p.insertPassphrase(text)
			return true, nil
		}
		// Editing keys must never change password echo mode.
		p.prompt.EchoMode = textinput.EchoPassword
		*p.prompt, cmd = p.prompt.Update(msg)
		p.prompt.EchoMode = textinput.EchoPassword
		return true, cmd
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		// Only single-rune navigation shortcuts and a physical Enter belong
		// to this router; batched text spelling a key name is not a key.
		shortcut := ""
		if key.Type == tea.KeyEnter {
			shortcut = "enter"
		} else if key.Type == tea.KeyRunes && len(key.Runes) == 1 && !key.Alt {
			shortcut = string(key.Runes)
		}
		switch shortcut {
		case "u":
			return true, a.startUnlock()
		case "enter", "e", "i":
			if a.mode == ModeNotebook || (shortcut != "enter" && a.cal.view == ViewMonth) {
				date := a.privateDate()
				if a.mode == ModeNotebook {
					date = a.nb.editDate()
				}
				return true, a.startPrivateEdit(date)
			}
		case "a":
			if p.hooks.IsUnlocked == nil || !p.hooks.IsUnlocked() {
				p.templateAfter = a.privateDate()
				return true, a.startUnlock()
			}
			if p.hooks.Load != nil {
				page, err := p.hooks.Load(a.privateDate())
				if err != nil {
					a.setPrivacyStatus("Cannot load private template metadata: " + err.Error())
					return true, nil
				}
				if page.Locked {
					a.setPrivacyStatus("Private template metadata is locked; unlock and retry templates.")
					return true, nil
				}
				a.setPrivateApplied(a.privateDate(), page.Applied)
			}
		}
	}
	return false, nil
}

func (a *App) privacyView() (string, bool) {
	if a.privacy == nil {
		return "", false
	}
	if a.privacy.closed {
		p := a.nb.theme.Palette()
		footer := p.MutedText.Render("Keep recovery information before closing this terminal.")
		return screenView("Session closed", "Decrypted views are hidden.", "\n  Review reported recovery errors before terminating the process.", footer, p, a.nb.width, a.nb.height), true
	}
	if a.privacy.editor != nil {
		return a.privacy.editor.View(), true
	}
	if a.privacy.prompt != nil {
		return a.unlockView(), true
	}
	return "", false
}

// privateEditor never creates plaintext files. Unsaved cancellation and save
// failures use the caller's encrypted recovery mechanism; failed recovery keeps
// the text in the running editor rather than discarding the only copy.
type privateEditor struct {
	date          string
	original      string
	input         privateInput
	save          Saver
	recover       func(string, string) (string, error)
	status        string
	confirm       bool
	done          bool
	outcome       editOutcome
	published     bool // This Update published a save, even if durability is uncertain.
	err           error
	unconfirmed   bool // Save published this baseline, but durability needs retry.
	width, height int
	keyHelp       *keyHelp
	recoveries    []scratchpad.RecoveryReference
	onRecovery    func(scratchpad.RecoveryReference)
	load          func(string) (string, error)
	canRelock     bool
	savePolicy    string
	theme         *themeWatcher  // Shared appearance only; the writer starts no watchers.
	signals       chan os.Signal // Standalone only; graceful shutdown may need a retry.
	signalDone    chan struct{}
	conflicted    bool
	review        *draftReview
}

func newPrivateEditor(date, original string, save Saver, sealRecovery func(string, string) (string, error)) *privateEditor {
	input := newPrivateInput()
	input.OpenValue(original)
	input.Focus()
	input.SetWidth(80)
	input.SetHeight(18)
	e := &privateEditor{date: date, original: original, input: input, save: save, recover: sealRecovery, theme: newThemeWatcher(ThemePrefAuto), savePolicy: "Saving encrypted"}
	e.resize(80, 24)
	return e
}

func (e *privateEditor) Init() tea.Cmd { return e.waitSignal() }

func (e *privateEditor) waitSignal() tea.Cmd {
	if e.signals == nil || e.signalDone == nil {
		return nil
	}
	input, done := e.signals, e.signalDone
	return func() tea.Msg {
		select {
		case <-input:
			return privateEditorInterruptedMsg{}
		case <-done:
			return nil
		}
	}
}

func (e *privateEditor) needsRecovery() bool {
	return e.unconfirmed || e.input.Value() != e.original
}

// A recovery can be published even when its directory sync fails. Never hide
// that path, and preserve the wrapped error for callers using errors.Is.
func privateRecoveryError(path string, err error) error {
	if path != "" {
		return fmt.Errorf("encrypted recovery: %s; recovery failed: %w", path, err)
	}
	return fmt.Errorf("encrypted recovery failed: %w", err)
}

func (e *privateEditor) resize(width, height int) {
	e.width, e.height = max(0, width), max(0, height)
	e.input.SetWidth(max(width-4, 1))
	e.input.SetHeight(max(height-6, 1))
	if e.keyHelp != nil {
		e.keyHelp.resize(width, height)
	}
	if e.review != nil {
		e.review.viewer.resize(width, height)
	}
}

func (e *privateEditor) recovery() (string, error) {
	if e.recover == nil {
		return "", errors.New("encrypted recovery is unavailable; keep this editor open")
	}
	path, err := e.recover(e.date, e.input.Value())
	if path == "" && err == nil {
		err = errors.New("recovery returned no artifact path; keep this editor open")
	}
	if path != "" {
		ref := scratchpad.RecoveryReference{Date: e.date, Path: path, Encrypted: true}
		found := false
		for _, existing := range e.recoveries {
			found = found || existing == ref
		}
		if !found {
			e.recoveries = append(e.recoveries, ref)
		}
		if e.onRecovery != nil {
			e.onRecovery(ref)
		}
	}
	return path, err
}

func (e *privateEditor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	e.published = false
	if e.rejectTerminalInput(msg) {
		return e, nil
	}
	if _, interrupted := msg.(privateEditorInterruptedMsg); interrupted {
		// Signals own shutdown even while help/find/review owns ordinary keys.
		// Failed recovery must leave the live editor available for a retry.
		e.keyHelp, e.review = nil, nil
		e.input.findActive, e.confirm = false, false
		if e.needsRecovery() {
			path, err := e.recovery()
			if err != nil {
				e.status = fmt.Sprintf("Shutdown recovery failed: %v. Text remains in this editor; keep it open and retry.", privateRecoveryError(path, err))
				return e, e.waitSignal()
			}
			e.err = fmt.Errorf("%w; encrypted recovery: %s", ErrPrivateEditorInterrupted, path)
		} else {
			e.err = ErrPrivateEditorInterrupted
		}
		e.done, e.outcome = true, editCancelled
		return e, tea.Quit
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		e.resize(size.Width, size.Height)
	}
	if e.review != nil {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch e.review.choice(key) {
			case 'x':
				e.review = nil
			case 'd':
				e.original = e.review.current
				e.conflicted, e.unconfirmed = false, false
				e.review = nil
				e.status = "Draft selected as replacement. Ctrl+S replaces the reviewed private Markdown; shared contributions are retained."
			case 'r':
				path, err := e.recovery()
				if err != nil {
					e.review.failure(privateRecoveryError(path, err).Error())
					return e, nil
				}
				e.original = e.review.current
				e.input.SetValue(e.original)
				e.conflicted, e.unconfirmed = false, false
				e.review = nil
				e.status = "Reloaded saved text; previous draft retained in encrypted recovery: " + path
			}
		}
		return e, nil
	}
	if e.keyHelp != nil {
		if key, ok := msg.(tea.KeyMsg); ok && e.keyHelp.update(key) {
			e.keyHelp = nil
		}
		return e, nil
	}
	if e.input.Finding() {
		if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyF1 {
			e.keyHelp = newKeyHelp("Find in current draft", privateFindBindings(), e.input.Status(), e.width, e.height)
			return e, nil
		}
		var cmd tea.Cmd
		e.input, cmd = e.input.Update(msg)
		return e, cmd
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		if helpRequested(key, false) {
			e.confirm = false
			details := e.input.Status() + "\n" + e.status + "\n" + e.savePolicy + "\nNo plaintext temporary editor file is created. Saving follows the store's current privacy policy."
			e.keyHelp = newKeyHelp("Write · "+e.date, privateEditorBindings(e.canRelock), details, e.width, e.height)
			if e.status != "" {
				e.keyHelp.viewport.GotoBottom()
			}
			return e, nil
		}
		switch key.Type {
		case tea.KeyCtrlT:
			if e.theme != nil {
				e.theme.SetPref(nextThemePref(e.theme.Pref()))
			}
			return e, nil
		case tea.KeyF2:
			e.confirm = false
			if e.conflicted {
				e.openConflictReview()
			}
			return e, nil
		case tea.KeyCtrlS:
			e.confirm = false
			if e.conflicted {
				e.status = "Conflict remains. Press F2 to compare saved text and the retained draft before retrying."
				return e, nil
			}
			if e.input.err != "" {
				e.status = e.input.err
				return e, nil
			}
			if !e.needsRecovery() {
				e.done = true
				return e, tea.Quit
			}
			var err error
			if e.save == nil {
				err = errors.New("private saving is unavailable")
			} else {
				err = e.save(e.date, e.original, e.input.Value())
			}
			if err == nil {
				e.done, e.published, e.outcome = true, true, editSaved
				return e, tea.Quit
			}
			if errors.Is(err, scratchpad.ErrDurabilityUnconfirmed) {
				// Publication succeeded: a retry must compare against our own
				// published text, not the superseded on-disk baseline.
				e.original = e.input.Value()
				e.unconfirmed, e.published = true, true
			}
			e.conflicted = errors.Is(err, scratchpad.ErrConflict) && !e.published
			path, recoveryErr := e.recovery()
			e.status = fmt.Sprintf("Save failed: %v", err)
			if e.published {
				e.status = fmt.Sprintf("Saved, but durability is unconfirmed: %v", err)
			}
			if recoveryErr == nil {
				e.status += "; encrypted recovery: " + path
			} else {
				e.status += fmt.Sprintf("; %v. Keep this editor open.", privateRecoveryError(path, recoveryErr))
			}
			if e.conflicted {
				e.status += "; F2: compare/reload before retrying"
			}
			return e, nil
		case tea.KeyEsc, tea.KeyCtrlC:
			if !e.needsRecovery() {
				e.done, e.outcome = true, editCancelled
				return e, tea.Quit
			}
			if !e.confirm {
				e.confirm = true
				e.status = "Unsaved changes. Press Esc/Ctrl+C again to leave with encrypted recovery, or Ctrl+S to save."
				if e.unconfirmed {
					e.status = "Save durability is unconfirmed. Press Esc/Ctrl+C again to leave with encrypted recovery, or Ctrl+S to retry."
				}
				return e, nil
			}
			path, err := e.recovery()
			if err != nil {
				e.status = fmt.Sprintf("%v. Text remains in this editor.", privateRecoveryError(path, err))
				return e, nil
			}
			e.err = fmt.Errorf("edit cancelled; encrypted recovery: %s", path)
			if e.unconfirmed {
				e.err = fmt.Errorf("editor closed with save durability unconfirmed; encrypted recovery: %s", path)
			}
			e.done, e.outcome = true, editCancelled
			return e, tea.Quit
		default:
			e.confirm = false
		}
	}
	if _, pasted := privatePasteText(msg); pasted {
		e.confirm = false
	}
	var cmd tea.Cmd
	e.input, cmd = e.input.Update(msg)
	return e, cmd
}

func (e *privateEditor) View() string { return e.writerView() }

// EditPrivate opens a standalone in-memory editor for the bare sp flow.
func EditPrivate(date, original string, save Saver, sealRecovery func(date, content string) (string, error), load ...func(string) (string, error)) error {
	model := newPrivateEditor(date, original, save, sealRecovery)
	if len(load) > 0 {
		model.load = load[0]
	}
	return runPrivateEditor(model)
}

// EditPrivateWithTheme uses the same configured appearance as the viewers. The
// caller still supplies the encryption-pinned saver; this is presentation only.
func EditPrivateWithTheme(date, original string, save Saver, sealRecovery func(string, string) (string, error), load func(string) (string, error), preference string) error {
	model := newPrivateEditor(date, original, save, sealRecovery)
	model.load = load
	model.theme.SetPref(preference)
	return runPrivateEditor(model)
}

// EditRecoveredPrivate keeps the current note as the conflict baseline while
// presenting recovered text for deliberate review and replacement.
func EditRecoveredPrivate(date, current, recovered string, save Saver, sealRecovery func(string, string) (string, error), load ...func(string) (string, error)) error {
	var loader func(string) (string, error)
	if len(load) > 0 {
		loader = load[0]
	}
	return EditRecoveredPrivateWithPolicy(date, current, recovered, save, sealRecovery, loader, "Saving follows the store's current policy")
}

// ErrPrivateEditorInterrupted distinguishes an unexpected program/signal exit
// from a completed save or deliberately confirmed cancellation.
var ErrPrivateEditorInterrupted = errors.New("private editor interrupted before completion")

type privateEditorInterruptedMsg struct{}

func privateEditorSignalFilter(model tea.Model, msg tea.Msg) tea.Msg {
	if _, quit := msg.(tea.QuitMsg); quit {
		if editor, ok := model.(*privateEditor); ok && !editor.done {
			return privateEditorInterruptedMsg{}
		}
	}
	return msg
}

func runPrivateEditor(model *privateEditor) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("private editor requires interactive terminal input and output")
	}
	// Tea's default signal handler stops after the first signal. A failed
	// recovery deliberately keeps this editor running, so retain ownership
	// until the editor actually exits instead of exposing the draft to the
	// default action of a second SIGINT/SIGTERM.
	model.signals = make(chan os.Signal, 2)
	model.signalDone = make(chan struct{})
	signal.Notify(model.signals, os.Interrupt, syscall.SIGTERM)
	defer func() {
		signal.Stop(model.signals)
		close(model.signalDone)
	}()
	_, err := RunTerminalProgram(model, os.Stdin, tea.WithAltScreen(), tea.WithoutSignalHandler())
	err = model.finishRun(err)
	WriteRecoveryReport(os.Stderr, model.recoveries)
	return err
}

func (e *privateEditor) finishRun(runErr error) error {
	if runErr != nil {
		return e.recoverStopped(runErr)
	}
	if !e.done {
		// Defense in depth for other nil-error exits: only the editor can
		// authorize completion. Never silently discard an unfinished draft.
		return e.recoverStopped(ErrPrivateEditorInterrupted)
	}
	return e.err
}

func (e *privateEditor) recoverStopped(err error) error {
	if e.needsRecovery() {
		path, recoveryErr := e.recovery()
		if recoveryErr != nil {
			return fmt.Errorf("editor stopped: %w; %w", err, privateRecoveryError(path, recoveryErr))
		}
		return fmt.Errorf("editor stopped: %w; encrypted recovery: %s", err, path)
	}
	return err
}
