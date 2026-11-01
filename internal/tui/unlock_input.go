package tui

// Standalone unlock uses the same raw byte owner as the application and writer.
// Only its signal-cancellation lifecycle is different: it must wait for owned
// unlock work before returning, even if a signal arrives while help is visible.
type unlockInterruptedMsg struct{}
