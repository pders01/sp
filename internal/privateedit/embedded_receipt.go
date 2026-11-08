package privateedit

import "github.com/pders01/sp/internal/editorcore"

// InputReceipt contains acknowledgement metadata only, not key contents. A
// terminal queue uses it to settle a timed-out event without duplicate input.
func (e *Embedded) InputReceipt() editorcore.InputReceipt { return e.ui.InputReceipt() }
