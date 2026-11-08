package editorterminal

import (
	"context"
	"sync"

	"github.com/pders01/sp/internal/privateedit"
	"github.com/pders01/sp/internal/scratchpad"
)

// Private provenance prevents accidentally restarting against another manager,
// date or owner. Pending input remains private and must never be replayed merely
// because the old terminal program returned.
type continuation struct {
	mu       sync.Mutex
	used     bool
	manager  *scratchpad.Manager
	document string
	editor   *privateedit.Embedded
}

// ResumeInMemory reopens only the recovery controls for the same retained owner.
// It never prepares/reloads a second edit, republishes or automatically replays
// pending keys. Retained events are quarantined even from manual retry/resolve:
// the last receipt may belong to a later recovery request, not those events.
// Fresh physical K/y consent is needed for possible-loss release.
func ResumeInMemory(ctx context.Context, o Options, retained Report) (Report, error) {
	c := retained.continuation
	if c == nil || c.manager == nil || c.editor == nil || c.manager != o.Manager || c.document != o.DocumentID || c.editor != retained.Editor || o.InMemory == nil || o.Experiment != nil || o.Driver != nil {
		return retained, ErrRetained
	}
	c.mu.Lock()
	if c.used {
		c.mu.Unlock()
		return retained, ErrRetained
	}
	c.used = true
	c.mu.Unlock()
	opened := false
	report, err := runWithFactory(ctx, o, func(m *model, w, h int) (editor, error) {
		opened = true
		restoreContinuation(m, retained)
		return c.editor, nil
	})
	// Terminal admission failure before the factory must not replace the owner.
	if report.Editor == nil {
		if !opened {
			c.mu.Lock()
			c.used = false
			c.mu.Unlock()
		}
		return retained, err
	}
	return report, err
}

func restoreContinuation(m *model, r Report) {
	m.reopening = true
	m.transportRecovery = true
	if len(r.pending) > 0 {
		event := r.pending[0]
		m.held = &event
		m.queue = append([]Input(nil), r.pending[1:]...)
		m.inputPaused = true
	}
}
