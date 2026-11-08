package privateedit

import (
	"context"

	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/scratchpad"
)

type managerStore struct {
	prepared *scratchpad.PrivateEdit
}

func (s managerStore) save(original, content string) error {
	return s.prepared.Save(original, content)
}

func (s managerStore) recover(content string) (string, error) {
	return s.prepared.SealRecovery(content)
}

// OpenDocumentExperiment joins a synthetic document adapter to the canonical
// encrypted lifecycle. It is not protected admission or normal CLI routing.
func OpenDocumentExperiment(ctx context.Context, manager *scratchpad.Manager, documentID string, open func(context.Context, string) (editorcore.Document, error)) (*Workflow, error) {
	if err := ctx.Err(); err != nil {
		return nil, failure("prepare", err)
	}
	if manager == nil || open == nil {
		return nil, failure("prepare", editorcore.ErrUnsupported)
	}
	prepared, err := manager.PreparePrivateEdit(documentID)
	if err != nil {
		return nil, failure("prepare", err)
	}
	content := prepared.Content()
	child, err := open(ctx, content)
	if child == nil {
		if err == nil {
			err = editorcore.ErrUnsupported
		}
		return nil, failure("open", err)
	}
	w := newWorkflow(child, managerStore{prepared: prepared}, content)
	w.openErr = failure("open", err)
	return w, w.openErr
}
