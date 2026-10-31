package scratchpad

import (
	"errors"

	"github.com/pders01/sp/internal/templates"
)

var ErrPrivacyPolicyChanged = errors.New("store save policy changed; reopen the editor/recovery to review the current policy")

// SaveWithPrivacyPolicy checks the reviewed policy under the same store lock as
// publication, including new/missing dates that have no revision to invalidate.
func (m *Manager) SaveWithPrivacyPolicy(note *Scratchpad, encrypted bool) error {
	return m.saveInternal(note, syncDirectory, false, encrypted)
}

// ApplyTemplateSectionsWithPrivacyPolicy keeps rendered private sections from
// silently publishing as plaintext if policy changes while commands are running.
func (m *Manager) ApplyTemplateSectionsWithPrivacyPolicy(date string, sections []templates.Section, encrypted bool) (*Scratchpad, error) {
	return m.applyTemplateSections(date, sections, syncDirectory, encrypted)
}
