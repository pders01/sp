package main

import (
	"io"
	"strings"
	"testing"
)

func TestPrivateUnlockScreenRejectsRedirectedInputBeforeManagerAccess(t *testing.T) {
	err := unlockPrivateInteractive(nil, strings.NewReader("not-a-passphrase\n"), io.Discard, "dark", "Unlock notes")
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("redirected input reached unlock: %v", err)
	}
}
