package main

import (
	"os"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/tui"
	"golang.org/x/term"
)

func TestInteractiveViewsRejectRedirectedDescriptorsBeforeStorage(t *testing.T) {
	if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		t.Skip("test process owns interactive descriptors")
	}
	// Nil dependencies prove rejection precedes store/config/editor access.
	err := runApp(nil, nil, tui.IconSet{}, nil)
	if err == nil || !strings.Contains(err.Error(), "interactive terminal input and output") {
		t.Fatalf("redirected UI was not rejected: %v", err)
	}
}
