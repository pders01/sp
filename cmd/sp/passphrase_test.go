package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/scratchpad"
)

type changeManagerFixture struct {
	status         scratchpad.PrivacyStatus
	statusErr      error
	unlockErr      error
	changeErr      error
	published      bool
	locks, changes int
	current, next  string
}

func (m *changeManagerFixture) PrivacyStatus() (scratchpad.PrivacyStatus, error) {
	return m.status, m.statusErr
}
func (m *changeManagerFixture) Unlock([]byte) error { return m.unlockErr }
func (m *changeManagerFixture) Lock()               { m.locks++ }
func (m *changeManagerFixture) ChangePassphrase(current, next []byte) (bool, error) {
	m.changes++
	m.current, m.next = string(current), string(next)
	return m.published, m.changeErr
}

type changePromptFixture struct {
	line      string
	passwords []string
	borrowed  [][]byte
	calls     int
}

func (p *changePromptFixture) Line(string) (string, error) { return p.line, nil }
func (p *changePromptFixture) Password(string) ([]byte, error) {
	if p.calls >= len(p.passwords) {
		return nil, errors.New("synthetic cancelled input")
	}
	value := []byte(p.passwords[p.calls])
	p.calls++
	p.borrowed = append(p.borrowed, value)
	return value, nil
}

func TestChangePassphraseCLIRejectsArgumentsAndNoninteractiveInput(t *testing.T) {
	for _, args := range [][]string{
		{"privacy", "change-passphrase"},
		{"privacy", "change-passphrase", "extra"},
		{"privacy", "change-passphrase", "--passphrase", "synthetic"},
		{"privacy", "change-passphrase", "--passphrase-file", "synthetic"},
		{"privacy", "change-passphrase", "--yes"},
	} {
		_, _, err := testCLI(func() (*scratchpad.Manager, error) {
			t.Fatal("manager initialized before terminal/argument validation")
			return nil, errors.New("unexpected store access")
		}, args...)
		if err == nil {
			t.Fatalf("unsafe invocation accepted: %v", args)
		}
	}
}

func TestChangePassphraseCLIExactSecretsOutcomesAndClearing(t *testing.T) {
	for _, published := range []bool{false, true} {
		for _, failure := range []bool{false, true} {
			mgr := &changeManagerFixture{status: scratchpad.PrivacyStatus{Enabled: true}, published: published}
			injected := errors.New("synthetic publication outcome")
			if failure {
				mgr.changeErr = injected
			}
			prompt := &changePromptFixture{line: "CHANGE PASSPHRASE", passwords: []string{" old  α� ", " new  β� ", " new  β� "}}
			var output bytes.Buffer
			err := changePrivacyPassphrase(mgr, prompt, &output)
			if (err != nil) != failure || (failure && !errors.Is(err, injected)) {
				t.Fatalf("published=%v failure=%v: %v", published, failure, err)
			}
			if mgr.changes != 1 || mgr.current != prompt.passwords[0] || mgr.next != prompt.passwords[1] || mgr.locks < 2 {
				t.Fatal("changed exact secrets, skipped publication or retained an unlocked session")
			}
			for _, borrowed := range prompt.borrowed {
				if bytes.Count(borrowed, []byte{0}) != len(borrowed) {
					t.Fatal("caller-owned collected secret bytes were not cleared")
				}
			}
			text := output.String()
			if err != nil {
				text += err.Error()
			}
			for _, secret := range prompt.passwords {
				if strings.Contains(text, secret) {
					t.Fatal("output exposed a secret")
				}
			}
			if failure && published && (!strings.Contains(text, "was changed") || !strings.Contains(text, "unconfirmed")) {
				t.Fatal("published outcome was presented as no change/rollback")
			}
			if !failure && published && !strings.Contains(text, "Session locked") {
				t.Fatal("successful change did not explain the session boundary")
			}
			if !failure && !published && !strings.Contains(text, "Passphrase unchanged") {
				t.Fatal("no-op was reported as publication")
			}
		}
	}
}

type changeStatusFailure struct{ cause error }

func (w changeStatusFailure) Write(data []byte) (int, error) {
	if bytes.HasPrefix(data, []byte("Passphrase changed.")) {
		return 0, w.cause
	}
	return len(data), nil
}

func TestChangePassphraseCLIFinalOutputFailureRetainsPublicationFact(t *testing.T) {
	injected := errors.New("synthetic output failure")
	mgr := &changeManagerFixture{status: scratchpad.PrivacyStatus{Enabled: true}, published: true}
	prompt := &changePromptFixture{line: "CHANGE PASSPHRASE", passwords: []string{"synthetic old", "synthetic new", "synthetic new"}}
	err := changePrivacyPassphrase(mgr, prompt, changeStatusFailure{injected})
	if !errors.Is(err, injected) || mgr.changes != 1 || !strings.Contains(err.Error(), "metadata was changed successfully") {
		t.Fatal("final output failure lost the successful publication outcome")
	}
	if strings.Contains(err.Error(), "not changed") || strings.Contains(err.Error(), "durability is unconfirmed") {
		t.Fatal("output failure incorrectly changed the storage outcome")
	}
	for _, borrowed := range prompt.borrowed {
		if bytes.Count(borrowed, []byte{0}) != len(borrowed) {
			t.Fatal("output failure retained collected secret bytes")
		}
	}
}

func TestChangePassphraseCLICancellationAndPolicyNeverPublish(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    scratchpad.PrivacyStatus
		line      string
		passwords []string
		wrong     bool
	}{
		{"disabled", scratchpad.PrivacyStatus{}, "CHANGE PASSPHRASE", nil, false},
		{"decrypting", scratchpad.PrivacyStatus{Enabled: true, Decrypting: true}, "CHANGE PASSPHRASE", nil, false},
		{"unmigrated", scratchpad.PrivacyStatus{Enabled: true, PlaintextDates: []string{"2026-01-01"}}, "CHANGE PASSPHRASE", nil, false},
		{"confirmation", scratchpad.PrivacyStatus{Enabled: true}, "yes", nil, false},
		{"empty", scratchpad.PrivacyStatus{Enabled: true}, "CHANGE PASSPHRASE", []string{""}, false},
		{"wrong", scratchpad.PrivacyStatus{Enabled: true}, "CHANGE PASSPHRASE", []string{"synthetic wrong"}, true},
		{"same", scratchpad.PrivacyStatus{Enabled: true}, "CHANGE PASSPHRASE", []string{"old", "old"}, false},
		{"mismatch", scratchpad.PrivacyStatus{Enabled: true}, "CHANGE PASSPHRASE", []string{"old", "new", "other"}, false},
		{"invalid", scratchpad.PrivacyStatus{Enabled: true}, "CHANGE PASSPHRASE", []string{"old", "new\nsecret"}, false},
		{"cancel-new", scratchpad.PrivacyStatus{Enabled: true}, "CHANGE PASSPHRASE", []string{"old"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &changeManagerFixture{status: tc.status}
			if tc.wrong {
				mgr.unlockErr = errors.New("synthetic authentication failure")
			}
			prompt := &changePromptFixture{line: tc.line, passwords: tc.passwords}
			var output bytes.Buffer
			if err := changePrivacyPassphrase(mgr, prompt, &output); err == nil || mgr.changes != 0 || mgr.locks == 0 {
				t.Fatal("cancelled/invalid request published or retained an unlocked session")
			}
			if strings.Contains(output.String(), "Passphrase changed.") {
				t.Fatal("failed request claimed success")
			}
			for _, borrowed := range prompt.borrowed {
				if bytes.Count(borrowed, []byte{0}) != len(borrowed) {
					t.Fatal("cancelled request retained secret bytes")
				}
			}
		})
	}
}
