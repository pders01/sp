package scratchpad

import (
	"errors"
	"fmt"

	"github.com/pders01/sp/internal/templates"
)

var ErrPrivacyPolicyChanged = errors.New("store save policy changed; reopen the editor/recovery to review the current policy")

// SaveWithPrivacyPolicy checks the reviewed policy under the same store lock as
// publication, including new/missing dates that have no revision to invalidate.
func (m *Manager) SaveWithPrivacyPolicy(note *Scratchpad, encrypted bool) error {
	return m.saveInternal(note, syncDirectory, false, encrypted)
}

// SaveContentWithPrivacyPolicy replaces only the reviewed private content,
// preserving current metadata and checking policy and revision at publication.
func (m *Manager) SaveContentWithPrivacyPolicy(date, original, content string, encrypted bool) error {
	return m.saveContentWithPrivacyPolicy(date, original, content, encrypted, nil, m.GetByDate)
}

func (m *Manager) saveContentWithPrivacyPolicy(date, original, content string, encrypted bool, generation *[32]byte, load func(string) (*Scratchpad, error)) error {
	actual, err := m.PrivacyEnabled()
	if err != nil {
		return err
	}
	if actual != encrypted {
		return ErrPrivacyPolicyChanged
	}
	note, err := load(date)
	if err != nil {
		return err
	}
	if note.Content != original {
		return fmt.Errorf("%w: %s; reload before retrying", ErrConflict, date)
	}
	note.Content = content
	return m.saveInternalReviewed(note, syncDirectory, false, generation, encrypted)
}

// ApplyTemplateSectionsWithPrivacyPolicy keeps rendered private sections from
// silently publishing as plaintext if policy changes while commands are running.
func (m *Manager) ApplyTemplateSectionsWithPrivacyPolicy(date string, sections []templates.Section, encrypted bool) (*Scratchpad, error) {
	return m.applyTemplateSections(date, sections, syncDirectory, encrypted)
}
