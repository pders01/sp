package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (e *privateEditor) palette() Palette {
	if e.theme != nil {
		return e.theme.Palette()
	}
	return darkPalette()
}

func (a *App) unlockTitle() string {
	if a.privacy.unlockTitle != "" {
		return a.privacy.unlockTitle
	}
	return "Unlock notes"
}

func (a *App) unlockExitLabel() string {
	if a.privacy.unlockExitLabel != "" {
		return a.privacy.unlockExitLabel
	}
	return "return"
}

func (a *App) unlockView() string {
	p := a.privacy
	width, height := a.nb.width, a.nb.height
	palette := a.nb.theme.Palette()
	if p.keyHelp != nil {
		return p.keyHelp.view(palette)
	}
	body, footer, minimum := a.unlockLayout()
	if width < 24 || height < minimum {
		return smallTerminal(a.unlockTitle(), width, height, 24, minimum, "F1: help · Esc: "+a.unlockExitLabel())
	}
	date := a.privateDate()
	caption := "Session only"
	if date != "" {
		caption += " · " + date
	}
	return screenView(a.unlockTitle(), caption, body, footer, palette, width, height)
}

// A form is supported only when its field, current error and actions are all
// visible. Submission uses this same geometry; never accept an invisible field.
func (a *App) unlockLayout() (bodyText, footerText string, minimumHeight int) {
	p := a.privacy
	width := a.nb.width
	palette := a.nb.theme.Palette()
	var body []string
	var actions []helpEntry
	wrap := func(text string) string { return ansi.Wrap(safeMarkdownDisplay(text), max(1, width-4), "") }
	if p.job != nil {
		frames := spinner.Dot.Frames
		status := "Unlocking your notes…"
		if p.job.cancelRequested {
			status = "Cancelling unlock; waiting for key derivation…"
		}
		body = []string{"", lipgloss.NewStyle().PaddingLeft(2).Render(palette.Header.Render(wrap(frames[p.job.frame%len(frames)] + " " + status))), "", lipgloss.NewStyle().PaddingLeft(2).Render(palette.MutedText.Render(wrap("SP waits for key derivation before releasing this operation.")))}
		actions = []helpEntry{{keys: "Esc", label: "cancel and wait", visible: true}}
	} else {
		fieldWidth := max(1, min(48, width-4))
		p.prompt.Width = max(1, fieldWidth-lipgloss.Width(p.prompt.Prompt)-1)
		body = []string{"", "  " + palette.Title.Render("Enter your passphrase"), "", "  " + p.prompt.View()}
		if p.promptErr != "" {
			message := p.promptErr
			if strings.HasPrefix(message, "Unlock failed:") {
				message = "Unlock failed. Try again; F1 shows details."
			}
			body = append(body, "", lipgloss.NewStyle().PaddingLeft(2).Render(palette.ErrorMessage.Render(ansi.Wrap(safeMarkdownDisplay(message), max(1, width-4), ""))))
		} else {
			body = append(body, "", lipgloss.NewStyle().PaddingLeft(2).Render(palette.MutedText.Render(wrap("Unlock lasts until you lock or close SP."))))
		}
		actions = []helpEntry{{keys: "Enter", label: "unlock", visible: true}, {keys: "Esc", label: a.unlockExitLabel(), visible: true}, {keys: "F1", label: "help", visible: true}}
	}
	content := strings.Join(body, "\n")
	footer := screenFooter(actions, palette, width)
	return content, footer, viewHeaderRows + viewHeight(content) + viewHeight(footer)
}

func (e *privateEditor) writerView() string {
	if e.done {
		return ""
	}
	palette := e.palette()
	if e.review != nil {
		return e.review.viewer.view(palette)
	}
	if e.keyHelp != nil {
		return e.keyHelp.view(palette)
	}
	entries := []helpEntry{{keys: "Ctrl+S", label: "save/return", visible: true}, {keys: "F2", label: "compare", visible: e.conflicted}, {keys: "Esc", label: "cancel", visible: true}, {keys: "F1", label: "help", visible: true}}
	if e.input.Finding() {
		entries = []helpEntry{{keys: "Enter", label: "accept find", visible: true}, {keys: "Esc", label: "close find", visible: true}, {keys: "F3", label: "next match", visible: true}, {keys: "F1", label: "help", visible: true}}
	}
	footer := screenFooter(entries, palette, e.width)
	metadata := []string{"  " + palette.MutedText.Render(truncate(oneLine(safeMarkdownDisplay(e.input.Status())), max(e.width-4, 0)))}
	if summary := e.outcomeSummary(); summary != "" {
		projected := ansi.Wrap(oneLine(safeMarkdownDisplay(summary)), max(1, e.width-4), "")
		metadata = append(metadata, lipgloss.NewStyle().PaddingLeft(2).Render(statusStyle(summary, palette).Render(projected)))
	}
	meta := strings.Join(metadata, "\n")
	minimum := max(8, viewHeaderRows+viewHeight(meta)+viewHeight(footer)+1)
	if e.width < 24 || e.height < minimum {
		return smallTerminal("Write · draft retained", e.width, e.height, 24, minimum, "F1: help · Esc: cancel")
	}
	canvasHeight := e.height - viewHeaderRows - viewHeight(meta) - viewHeight(footer)
	e.input.SetWidth(max(1, e.width-4))
	e.input.SetHeight(canvasHeight)
	caption := e.savePolicy
	if caption == "" {
		caption = "Draft in memory"
	}
	if e.unconfirmed {
		if e.input.Value() != e.original {
			caption += " · unsaved changes"
		}
		caption += " · durability unconfirmed"
	} else if e.needsRecovery() {
		caption += " · unsaved"
	}
	body := lipgloss.NewStyle().Foreground(palette.Text).PaddingLeft(2).Height(canvasHeight).Render(e.input.View())
	return fitView(strings.Join([]string{viewHeader("Write · "+e.date, caption, palette, e.width), body, meta, footer}, "\n"), e.width, e.height)
}

// Full details stay on the editor and in F1. The normal page shows a concise
// outcome and next action without pretending failure means rollback or erasure.
func (e *privateEditor) outcomeSummary() string {
	lower := strings.ToLower(e.status)
	newerDraft := e.unconfirmed && e.input.Value() != e.original
	switch {
	case strings.Contains(lower, "recovery failed"), strings.HasPrefix(lower, "cannot lock:"):
		if newerDraft {
			return "Earlier save published; newer draft not saved. Durability unconfirmed; recovery failed. Keep this editor open. F1: details."
		}
		if e.unconfirmed {
			return "Saved, but durability is unconfirmed; recovery failed. Keep this editor open. F1: details."
		}
		return "Recovery failed — keep this editor open. F1: details."
	case e.confirm && newerDraft:
		return "New changes not saved. Earlier durability unconfirmed. Ctrl+S saves; Esc again leaves with encrypted recovery."
	case e.confirm && e.unconfirmed:
		return "Durability unconfirmed. Ctrl+S retries; Esc again leaves with encrypted recovery."
	case e.confirm:
		return "Unsaved changes. Ctrl+S saves; Esc again leaves with encrypted recovery."
	case newerDraft:
		return "Earlier save published; newer draft not saved. Durability unconfirmed. Ctrl+S: save · F1: details."
	case e.unconfirmed && e.status != "":
		return "Saved, but durability is unconfirmed. Ctrl+S: retry · F1: details."
	case e.conflicted:
		return "Not saved — saved text changed. F2: compare · F1: details."
	case strings.HasPrefix(e.status, "Save failed:"):
		return "Not saved — draft retained. Ctrl+S: retry · F1: details."
	case strings.HasPrefix(e.status, "Recovery:"):
		return "Saving replaces this note; shared updates and the recovery file are retained."
	case strings.HasPrefix(e.status, "Reloaded saved text;"):
		return "Saved text reloaded; earlier draft kept in encrypted recovery. F1: details."
	default:
		return e.status
	}
}
