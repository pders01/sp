package tui

import (
	"errors"
	"fmt"

	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
)

type editOutcome uint8

const (
	editUnchanged editOutcome = iota
	editSaved
	editCancelled
)

// externalEditResult distinguishes publication from confirmation of durability.
// A published replacement must refresh views even when its buffer is retained.
type externalEditResult struct {
	content   string
	published bool
	status    string
	err       error
}

func completeExternalEdit(msg editDoneMsg, save Saver) externalEditResult {
	fail := func(stage string, err error) externalEditResult {
		status := fmt.Sprintf("%s: %v", stage, err)
		if msg.path != "" {
			status += "; buffer path: " + msg.path
		}
		return externalEditResult{status: status, err: err}
	}
	if msg.err != nil {
		return fail("editor", msg.err)
	}
	content, err := editor.ReadEdited(msg.path)
	if err != nil {
		return fail("read", err)
	}
	result := externalEditResult{content: content, status: "No changes."}
	if content != msg.original || msg.unconfirmed {
		if save == nil {
			return fail("save", errors.New("saving is unavailable"))
		}
		err := save(msg.date, msg.original, content)
		if err != nil {
			if !errors.Is(err, scratchpad.ErrDurabilityUnconfirmed) {
				return fail("save", err)
			}
			result = fail("Saved, but durability is unconfirmed", err)
			result.content, result.published = content, true
			return result
		}
		result.published, result.status = true, "Saved"
	}
	if msg.cleanup != nil {
		msg.cleanup()
	}
	return result
}
