package scratchpad

import (
	"errors"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/templates"
)

func TestTemplatePublicationReturnsSnapshotWhenDurabilityIsUnconfirmed(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		name := "plaintext"
		if encrypted {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			m := setupTestManager(t)
			if encrypted {
				m = privateTestManager(t)
			}
			defer m.Lock()
			const date = "2024-01-01"
			note, err := m.applyTemplateSections(date, []templates.Section{{ID: "one", Title: "One", Body: "synthetic template"}},
				func(string) error { return errors.New("injected directory sync failure") })
			if !errors.Is(err, ErrDurabilityUnconfirmed) || note == nil || !note.exists {
				t.Fatalf("published result = %+v, %v", note, err)
			}
			if !strings.Contains(note.Content, "synthetic template") || strings.Join(note.AppliedTemplates, ",") != "one" {
				t.Fatal("published result lost template content or metadata")
			}
			visible, readErr := m.GetByDate(date)
			if readErr != nil || visible.Content != note.Content || visible.revision != note.revision {
				t.Fatalf("published snapshot does not match visible note: %+v, %v", visible, readErr)
			}
		})
	}
}
