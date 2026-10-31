package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"filippo.io/age"
)

type privacySessionRetryPrompt struct {
	secrets [][]byte
	err     error
	calls   int
}

func (*privacySessionRetryPrompt) Line(string) (string, error) {
	return "", errors.New("unexpected unmasked input")
}

func (p *privacySessionRetryPrompt) Password(string) ([]byte, error) {
	p.calls++
	if len(p.secrets) == 0 {
		return nil, errors.New("unexpected password request")
	}
	secret := p.secrets[0]
	p.secrets = p.secrets[1:]
	return secret, p.err
}

func privacySessionMismatch() error {
	return fmt.Errorf("unlock identity: %w", &age.NoIdentityMatchError{})
}

func TestPrivacySessionBareRetryPreservesExactSecretsAndClearsCopies(t *testing.T) {
	wrong, correct := []byte(" wrong synthetic secret "), []byte(" exact synthetic secret ")
	prompt := &privacySessionRetryPrompt{secrets: [][]byte{wrong, correct}}
	var output bytes.Buffer
	calls := 0
	err := unlockPrivateWithRetry(prompt, &output, func(secret []byte) error {
		calls++
		if calls == 1 {
			if string(secret) != " wrong synthetic secret " {
				t.Fatal("wrong secret was rewritten")
			}
			return privacySessionMismatch()
		}
		if string(secret) != " exact synthetic secret " {
			t.Fatal("retry secret was rewritten")
		}
		return nil
	})
	if err != nil || calls != 2 || prompt.calls != 2 {
		t.Fatalf("retry did not succeed: calls=%d, prompts=%d, err=%v", calls, prompt.calls, err)
	}
	for _, secret := range [][]byte{wrong, correct} {
		if strings.Trim(string(secret), "\x00") != "" {
			t.Fatal("submitted secret slice was not cleared")
		}
	}
	if !strings.Contains(output.String(), "Try again") || strings.Contains(output.String(), "synthetic secret") {
		t.Fatal("retry feedback was missing or leaked a secret")
	}
}

func TestPrivacySessionBareRetryRejectsInvalidBytesWithoutNormalization(t *testing.T) {
	invalid := []byte("bad\nsecret")
	valid := []byte(" valid synthetic secret ")
	prompt := &privacySessionRetryPrompt{secrets: [][]byte{invalid, valid}}
	calls := 0
	err := unlockPrivateWithRetry(prompt, io.Discard, func(secret []byte) error {
		calls++
		if string(secret) != " valid synthetic secret " {
			t.Fatal("validation rewrote the secret")
		}
		return nil
	})
	if err != nil || calls != 1 || strings.Trim(string(invalid), "\x00") != "" {
		t.Fatal("invalid input reached crypto or retained its bytes")
	}
}

func TestPrivacySessionBareRetryCancellation(t *testing.T) {
	prompt := &privacySessionRetryPrompt{secrets: [][]byte{[]byte("wrong"), {}}}
	calls := 0
	err := unlockPrivateWithRetry(prompt, io.Discard, func([]byte) error {
		calls++
		return privacySessionMismatch()
	})
	if err == nil || !strings.Contains(err.Error(), "cancelled") || calls != 1 || prompt.calls != 2 {
		t.Fatalf("empty cancellation retried crypto or lost cancellation: %v", err)
	}
}

func TestPrivacySessionBareRetryRetainsReadAndStorageErrors(t *testing.T) {
	for _, readFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "storage", true: "terminal"}[readFailure], func(t *testing.T) {
			cause := errors.New("synthetic failure")
			secret := []byte("synthetic secret")
			prompt := &privacySessionRetryPrompt{secrets: [][]byte{secret}}
			if readFailure {
				prompt.err = cause
			}
			calls := 0
			err := unlockPrivateWithRetry(prompt, io.Discard, func([]byte) error { calls++; return cause })
			if !errors.Is(err, cause) || prompt.calls != 1 || (readFailure && calls != 0) || strings.Trim(string(secret), "\x00") != "" {
				t.Fatalf("error/copy handling failed: %v", err)
			}
		})
	}
}

type privacySessionBrokenWriter struct{}

func (privacySessionBrokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestPrivacySessionBareRetryRetainsOutputError(t *testing.T) {
	secret := []byte("synthetic secret")
	prompt := &privacySessionRetryPrompt{secrets: [][]byte{secret}}
	err := unlockPrivateWithRetry(prompt, privacySessionBrokenWriter{}, func([]byte) error { return privacySessionMismatch() })
	if !errors.Is(err, io.ErrClosedPipe) || prompt.calls != 1 || strings.Trim(string(secret), "\x00") != "" {
		t.Fatalf("retry output error was hidden or secret retained: %v", err)
	}
}
