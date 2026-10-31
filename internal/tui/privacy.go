package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pders01/sp/internal/scratchpad"
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
	Load       func(string) (PrivatePage, error)
	Save       Saver
	Recover    func(date, content string) (string, error)
}

type privacyState struct {
	hooks         PrivacyHooks
	prompt        *privatePassphraseInput
	promptErr     string
	promptInvalid bool // Sticky until submission/cancellation; never sanitize a secret.
	editAfter     string
	editor        *privateEditor
	keyHelp       *keyHelp
}

// SetPrivacy switches editing to the in-memory editor. No external editor or
// plaintext temporary Markdown is involved in this mode.
func (a *App) SetPrivacy(hooks PrivacyHooks) {
	a.privacy = &privacyState{hooks: hooks}
	a.nb.privacyEnabled = true
	a.cal.privacyEnabled = true
	// Defense in depth: do not leave an external editing path wired.
	a.nb.editor, a.nb.save, a.nb.loader = nil, nil, nil
	a.cal.editor, a.cal.save, a.cal.loader = nil, nil, nil
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
	if date == "" || a.privacy.hooks.Load == nil {
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

// updatePrivacy handles only privacy-specific keys and modal UI. Returning
// handled=false delegates normal navigation to the existing router.
func (a *App) updatePrivacy(msg tea.Msg) (handled bool, cmd tea.Cmd) {
	p := a.privacy
	if p == nil {
		return false, nil
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
				p.keyHelp = newKeyHelp("Unlock private notes", unlockBindings(), p.promptErr+"\nUnlock lasts for this process. Do not enter the passphrase into an agent chat.", a.nb.width, a.nb.height)
				return true, nil
			case tea.KeyEsc, tea.KeyCtrlC:
				p.prompt.SetValue("")
				p.prompt = nil
				p.editAfter = ""
				return true, nil
			case tea.KeySpace:
				p.insertPassphrase(" ")
				return true, nil
			case tea.KeyEnter:
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
				// Unlock deliberately stays on the UI thread: no background key
				// operation can outlive cancellation or a later process lock.
				err := p.hooks.Unlock(secret)
				clear(secret)
				if err != nil {
					p.promptErr = fmt.Sprintf("Unlock failed: %v", err)
					return true, nil
				}
				p.prompt = nil
				a.refreshPrivatePage()
				date := p.editAfter
				p.editAfter = ""
				if date != "" {
					return true, a.startPrivateEdit(date)
				}
				a.setPrivacyStatus("Private notes unlocked for this SP process.")
				return true, nil
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
				a.setPrivacyStatus("Unlock private notes with u before applying templates.")
				return true, nil
			}
			if p.hooks.Load != nil {
				page, err := p.hooks.Load(a.privateDate())
				if err != nil || page.Locked {
					a.setPrivacyStatus("Cannot load private template metadata; unlock or reload the page.")
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
	if a.privacy.editor != nil {
		return a.privacy.editor.View(), true
	}
	if a.privacy.prompt != nil {
		p := a.privacy
		width, height := a.nb.width, a.nb.height
		palette := a.nb.theme.Palette()
		if p.keyHelp != nil {
			return p.keyHelp.view(palette), true
		}
		if width < 24 || height < 8 {
			return smallTerminal("Unlock private notes", width, height, 24, 8, "F1: help · Esc: cancel"), true
		}
		p.prompt.Width = max(1, width-lipgloss.Width(p.prompt.Prompt)-1)
		controls := wrappedHelp([]helpEntry{
			{keys: "Enter", label: "unlock", visible: true},
			{keys: "Esc", label: "cancel", visible: true},
			{keys: "F1", label: "help", visible: true},
		}, width)
		view := strings.Join([]string{
			palette.Header.Render("Unlock private notes"), "", p.prompt.View(),
			truncate(oneLine(p.promptErr), width), controls,
			truncate("Process-scoped unlock. Never share this passphrase with an agent.", width),
		}, "\n")
		return fitView(view, width, height), true
	}
	return "", false
}

func renderPrivatePage(page PrivatePage) string {
	if page.LoadError != "" {
		return "Private note unavailable: " + page.LoadError
	}
	if !page.HasPrivate && page.Private == "" {
		if strings.TrimSpace(page.Shared) != "" {
			return "# Agent contributions · shared\n\n" + page.Shared
		}
		return "No entry for this day. Press e to create a private note."
	}
	private := page.Private
	switch {
	case page.Locked:
		private = "Locked. Press u to unlock private notes."
	case private == "":
		private = "(Private note is empty.)"
	}
	content := "# Private notes\n\n" + private
	if strings.TrimSpace(page.Shared) != "" {
		content += "\n\n---\n\n# Agent contributions · shared\n\n" + page.Shared
	}
	return content
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
	conflicted    bool
	review        *draftReview
}

func newPrivateEditor(date, original string, save Saver, sealRecovery func(string, string) (string, error)) *privateEditor {
	input := newPrivateInput()
	input.SetValue(original)
	input.Focus()
	input.SetWidth(80)
	input.SetHeight(18)
	e := &privateEditor{date: date, original: original, input: input, save: save, recover: sealRecovery}
	e.resize(80, 24)
	return e
}

func (e *privateEditor) Init() tea.Cmd { return nil }

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
	e.input.SetWidth(max(width-2, 1))
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
	if key, ok := msg.(tea.KeyMsg); ok {
		if helpRequested(key, false) {
			e.confirm = false
			details := e.input.err + "\n" + e.status + "\nNo plaintext temporary editor file is created. Saving follows the store's current privacy policy."
			e.keyHelp = newKeyHelp("Private editor", privateEditorBindings(), details, e.width, e.height)
			return e, nil
		}
		switch key.Type {
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

func (e *privateEditor) View() string {
	if e.done {
		return ""
	}
	if e.review != nil {
		return e.review.viewer.view(Palette{})
	}
	if e.keyHelp != nil {
		return e.keyHelp.view(Palette{})
	}
	controls := wrappedHelp([]helpEntry{
		{keys: "Ctrl+S", label: "save/return", visible: true},
		{keys: "F2", label: "review conflict", visible: e.conflicted},
		{keys: "Esc", label: "cancel", visible: true},
		{keys: "F1", label: "help", visible: true},
	}, e.width)
	var statuses []string
	for _, status := range []string{e.input.err, e.status} {
		if status != "" {
			statuses = append(statuses, truncate(oneLine(status), e.width))
		}
	}
	minHeight := max(8, 3+len(statuses)+viewHeight(controls))
	if e.width < 24 || e.height < minHeight {
		return smallTerminal("Private editor · draft retained", e.width, e.height, 24, minHeight, "F1: help · Esc: cancel")
	}
	canvasHeight := e.height - 2 - len(statuses) - viewHeight(controls)
	e.input.SetHeight(canvasHeight)
	lines := []string{
		truncate("Private Markdown · "+e.date, e.width),
		truncate("Built-in editor · no plaintext editor file", e.width),
		lipgloss.NewStyle().Height(canvasHeight).Render(e.input.View()),
	}
	lines = append(lines, statuses...)
	lines = append(lines, controls)
	return fitView(strings.Join(lines, "\n"), e.width, e.height)
}

// EditPrivate opens a standalone in-memory editor for the bare sp flow.
func EditPrivate(date, original string, save Saver, sealRecovery func(date, content string) (string, error), load ...func(string) (string, error)) error {
	model := newPrivateEditor(date, original, save, sealRecovery)
	if len(load) > 0 {
		model.load = load[0]
	}
	return runPrivateEditor(model)
}

// EditRecoveredPrivate keeps the current note as the conflict baseline while
// presenting recovered text for deliberate review and replacement.
func EditRecoveredPrivate(date, current, recovered string, save Saver, sealRecovery func(string, string) (string, error), load ...func(string) (string, error)) error {
	model := newPrivateEditor(date, current, save, sealRecovery)
	if len(load) > 0 {
		model.load = load[0]
	}
	model.input.SetValue(recovered)
	model.status = "Recovery: saving replaces the current private Markdown; shared contributions are retained."
	return runPrivateEditor(model)
}

func runPrivateEditor(model *privateEditor) error {
	result, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		err = model.recoverStopped(err)
	} else {
		err = result.(*privateEditor).err
	}
	WriteRecoveryReport(os.Stderr, model.recoveries)
	return err
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
