package editorterminal

import (
	"errors"
	"testing"

	"github.com/pders01/sp/internal/editorcore"
)

func TestUnsupportedRejectionNeedsOwnCompleteZeroReceiptAndHealthyOwner(t *testing.T) {
	for _, name := range []string{"generic-error", "stale", "unknown", "accepted", "count-conflict", "fault", "latest-unknown", "frozen", "closed", "finish", "save", "cancel"} {
		t.Run(name, func(t *testing.T) {
			m := fixtureModel()
			e := m.editor.(*fakeEditor)
			r := jobMsg{kind: "input", input: Input{Text: "synthetic input"}, before: 4, err: editorcore.ErrUnsupported, receipt: editorcore.InputReceipt{RequestID: 5, RequestedBytes: 15, Known: true}}
			switch name {
			case "generic-error":
				r.err = errors.New("synthetic ordinary failure")
			case "stale":
				r.receipt.RequestID = 4
			case "unknown":
				r.receipt.Known = false
			case "accepted":
				r.receipt.AcceptedBytes = 1
			case "count-conflict":
				r.count = 1
			case "fault":
				e.r.Editor.Fault = editorcore.ErrRemote
			case "latest-unknown":
				e.r.Editor.LatestUnknown = true
			case "frozen":
				e.r.Editor.Mirror.Frozen = true
			case "closed":
				e.r.Editor.Closed = true
			case "finish":
				r.input = Input{Key: "Finish"}
			case "save":
				r.input = Input{Key: "Save"}
			case "cancel":
				r.input = Input{Key: "Cancel"}
			}
			m.finished(r)
			if m.held == nil || m.phase == "editing" {
				t.Fatal("unproven rejection released an owned input event")
			}
		})
	}
}

func TestPreExecutionRejectionUsesCapturedReceiptNotLaterInput(t *testing.T) {
	m := fixtureModel()
	e := rejectedPasteEditor{fixtureEditor()}
	m.editor = e
	m.enqueue(Input{Paste: true, Text: "synthetic rejection"})
	cmd := m.next()
	result, ok := cmd().(jobMsg)
	if !ok || !result.receipt.Known || result.receipt.AcceptedBytes != 0 {
		t.Fatal("input job did not capture its own rejection receipt")
	}
	e.receipt = editorcore.InputReceipt{RequestID: result.receipt.RequestID + 1, RequestedBytes: 6, AcceptedBytes: 6, Known: true}
	m.finished(result)
	if m.phase != "editing" || m.held != nil || m.status == "" {
		t.Fatal("later mutable receipt replaced the captured input outcome")
	}
}
