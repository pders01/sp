package editorterminal

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/pders01/sp/internal/editorcore"
)

func clip(text string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(text, width, "")
}

// Tea alone owns cursor positioning/terminal modes. Never embed Frame.ANSI's
// absolute CUP controls into its renderer; consume only validated safe rows.
func (m *model) View() string {
	if m.phase == "done" || m.width <= 1 || m.height <= 0 {
		return ""
	}
	width := min(m.width-1, editorcore.MaxColumns)
	if m.small() {
		stopHint := "Ctrl+C: encrypted recovery/close."
		if m.forwardEditorControls {
			stopHint = "Ctrl+Q/Z: encrypted recovery/close."
		}
		lines := []string{"SP · Editor", "Resize to at least 40x10.", "Editing and confirmation paused.", stopHint}
		if len(lines) > m.height {
			lines = lines[:m.height]
		}
		for i := range lines {
			lines[i] = clip(lines[i], width)
		}
		return terminalCanvas(lines, width, m.height)
	}
	if m.phase == "confirm" || m.phase == "known-confirm" {
		return terminalCanvas(strings.Split(m.confirmationView(width), "\n"), width, m.height)
	}
	w, h := m.geometry()
	bindings := "F2 save · F3 cancel · Ctrl+L/C/Z encrypted recovery+lock (not suspend)"
	lines := []string{"SP · Editor", bindings}
	if m.forwardEditorControls {
		lines = nil
	}
	body := make([]string, h)
	noticeRow := h - 1
	if m.editor != nil {
		frame := m.editor.Frame()
		rows := frame.StyledLines(m.phase == "editing")
		if len(rows) > 0 && frame.StatusRow != nil && *frame.StatusRow < h {
			noticeRow = *frame.StatusRow
		}
		for row := 0; row < min(h, len(rows)); row++ {
			body[row] = clip(rows[row], w)
		}
	} else {
		body[0] = "Starting isolated editor..."
	}
	status := m.status
	if m.forwardEditorControls {
		if m.busy && m.phase != "editing" && m.editor != nil {
			status = "Working; draft remains owned."
		}
		// Reuse the frame's designated status row, leaving an active prompt
		// intact. Normal editing adds no header, footer or shortcut guide.
		if status != "" {
			notice := strings.Split(ansi.Wrap(status, w, ""), "\n")
			start := max(0, noticeRow-len(notice)+1)
			for i := 0; i < len(notice) && start+i <= noticeRow; i++ {
				body[start+i] = notice[i]
			}
		}
		lines = append(lines, body...)
	} else {
		lines = append(lines, body...)
		if status == "" {
			status = "Draft remains owned until saved or recovered."
		}
		if m.busy {
			status = "Working; operation remains owned until settled. " + status
		}
		lines = append(lines, status, "Physical y/n only in confirmation.")
	}
	return terminalCanvas(lines, width, m.height)
}

// Only application-owned prose is wrapped. Draft bytes are never transformed.
// A dedicated modal keeps the entire loss warning and physical choices visible
// even at the minimum supported 40x10 terminal size.
func (m *model) confirmationView(width int) string {
	warning := "Recover changed or durability-unconfirmed text as encrypted recovery, then close?"
	no := "Physical N: resume editing"
	if m.phase == "known-confirm" {
		warning = "Latest edits may be missing. Pending keys will be discarded. Recover known copies and close? Missing edits can be lost."
		no = "Physical N: keep owner; go back"
	}
	lines := []string{"SP · Confirm", ""}
	lines = append(lines, strings.Split(ansi.Wrap(warning, width, ""), "\n")...)
	lines = append(lines, "", "Physical Y: recover encrypted + close", no, "Pasted text never confirms.")
	return strings.Join(lines, "\n")
}

// Fill every row/cell of the bounded canvas, including shortened status text and
// modal transitions. Do not rely on a terminal's erase-to-end behavior to remove
// an earlier longer footer. Bounds stay tied to the shared frame budget.
func terminalCanvas(lines []string, width, height int) string {
	width = min(width, editorcore.MaxColumns)
	height = min(height, editorcore.MaxRows+4)
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := make([]string, height)
	for row := range rows {
		line := ""
		if row < len(lines) {
			line = clip(lines[row], width)
		}
		rows[row] = line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	}
	return strings.Join(rows, "\n")
}
