package tui

import tea "github.com/charmbracelet/bubbletea"

// TerminalLiteralPaste recognizes the existing literal paste and decoded text
// batch messages. Recognized text must be treated as literal data, never
// reinterpreted as shortcut keys. It does not validate text for an application.
func TerminalLiteralPaste(msg tea.Msg) (string, bool) {
	return privatePasteText(msg)
}

// TerminalRejection returns the byte adapter's existing fixed rejection reason.
// It exposes neither rejected input nor parser state.
func TerminalRejection(msg tea.Msg) (string, bool) {
	if rejected, ok := msg.(terminalRejectedMsg); ok {
		return rejected.reason, true
	}
	return "", false
}

// TerminalInputObserver optionally observes input termination in
// RunTerminalProgram. The underlying startup model receives at most one call
// when input ends or errors, after all queued transport events have been
// consumed by the filter. Ordinary cleanup/cancellation does not notify.
//
// TerminalInputEnded may run on the reader or filter goroutine. It must be a
// notification-only, nonblocking operation: do not update model state or
// synchronously send to the program here. Signal a channel consumed by a Tea
// command instead. The event loop finishes the current Update before handling
// that notification; input termination is not itself a synthetic key or Quit.
// Deferred Escape resolution retains its existing timer semantics.
type TerminalInputObserver interface {
	TerminalInputEnded()
}
