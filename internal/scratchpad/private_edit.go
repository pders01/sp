package scratchpad

import "filippo.io/age"

// PrivateEdit is an opaque, manager/generation/date-bound prepared edit. It
// retains content, a public-only recovery sealer and an authentication nonce,
// never a private identity/passphrase outside the manager. Do not log it.
type PrivateEdit struct {
	manager    *Manager
	date       string
	content    string
	generation [32]byte
	seal       func(string, string) (string, error)
}

func (e *PrivateEdit) Content() string { return e.content }

// Save uses the canonical saver, additionally binding encryption/publication to
// the prepared authenticated generation. Relock/re-unlock invalidates saving,
// even for the same key. Recovery remains available with the original public pin.
func (e *PrivateEdit) Save(original, content string) error {
	if e == nil || e.manager == nil {
		return ErrLocked
	}
	return e.save(original, content, e.manager.GetByDate)
}

func (e *PrivateEdit) save(original, content string, load func(string) (*Scratchpad, error)) error {
	return e.manager.saveContentWithPrivacyPolicy(e.date, original, content, true, &e.generation, load)
}

func (e *PrivateEdit) SealRecovery(content string) (string, error) {
	if e == nil || e.seal == nil {
		return "", ErrLocked
	}
	return e.seal(e.date, content)
}

// PreparePrivateEdit captures only the selected private content and public
// recovery sealer from the SAME authenticated identity generation. No note or
// recovery is published. Policy-changing writers are excluded during prepare;
// relock/re-unlock or metadata substitution fails closed, without a retry.
func (m *Manager) PreparePrivateEdit(date string) (*PrivateEdit, error) {
	return m.preparePrivateEdit(date, m.GetByDate)
}

// The loader is operation-local deterministic fault injection, not a public
// alternate preparation path or a global hook.
func (m *Manager) preparePrivateEdit(date string, load func(string) (*Scratchpad, error)) (*PrivateEdit, error) {
	if err := validateDate(date); err != nil {
		return nil, err
	}
	lock, err := lockNote(m.storageDir, "privacy")
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	enabled, err := m.PrivacyEnabled()
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrPrivacyPolicyChanged
	}
	identity, err := m.unlockedIdentity()
	if err != nil {
		return nil, err
	}
	m.keyMu.RLock()
	generation, same := m.keyGeneration, m.identity == identity
	m.keyMu.RUnlock()
	if !same {
		return nil, ErrLocked
	}
	seal, err := m.PinRecovery()
	if err != nil {
		return nil, err
	}
	note, err := load(date)
	if err != nil {
		return nil, err
	}
	current, err := m.unlockedIdentity()
	// Original object stays referenced: A -> B -> A Unlock allocates a fresh
	// object, so pointer comparison rejects ABA without retaining that private
	// identity in the returned prepared edit or public sealer.
	if err != nil || current != identity || !m.matchesEditGeneration(&generation, identity) {
		return nil, ErrLocked
	}
	return &PrivateEdit{manager: m, date: date, content: note.Content, generation: generation, seal: seal}, nil
}

// Called inside the publication critical section AND with the exact immutable
// identity selected for encryption. A later manager switch cannot make that
// already-selected local identity encrypt to a different recipient.
func (m *Manager) matchesEditGeneration(expected *[32]byte, identity *age.X25519Identity) bool {
	if expected == nil {
		return true
	}
	m.keyMu.RLock()
	defer m.keyMu.RUnlock()
	return m.identity != nil && m.keyGeneration == *expected && (identity == nil || m.identity == identity)
}
