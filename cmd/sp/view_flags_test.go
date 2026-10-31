package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/scratchpad"
	"github.com/spf13/cobra"
)

func TestViewFlagsRejectConflictingModesBeforeOpeningStore(t *testing.T) {
	for _, args := range [][]string{{"-c", "-n"}, {"-cn"}, {"--calendar", "--notebook"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := newRootCommand(func() (*scratchpad.Manager, error) {
				t.Fatal("conflicting view flags must not open storage")
				return nil, nil
			})
			cmd.RunE = func(*cobra.Command, []string) error {
				t.Fatal("conflicting view flags reached execution")
				return nil
			}
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "--calendar") || !strings.Contains(err.Error(), "--notebook") || !strings.Contains(err.Error(), "choose one view") {
				t.Fatalf("conflicting flags = %v", err)
			}
		})
	}
}

func TestViewFlagsPreserveSingleModeAndExplicitFalseValues(t *testing.T) {
	for _, tc := range []struct {
		args               []string
		calendar, notebook bool
	}{
		{nil, false, false},
		{[]string{"-c"}, true, false},
		{[]string{"-n"}, false, true},
		{[]string{"--calendar=false", "--notebook"}, false, true},
		{[]string{"--calendar", "--notebook=false"}, true, false},
		{[]string{"--calendar=false", "--notebook=false"}, false, false},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cmd := newRootCommand(func() (*scratchpad.Manager, error) {
				t.Fatal("synthetic flag check must not open storage")
				return nil, nil
			})
			calls := 0
			cmd.RunE = func(*cobra.Command, []string) error {
				calls++
				if calendarFlag != tc.calendar || notebookFlag != tc.notebook {
					t.Fatal("view flags selected an unexpected mode")
				}
				return nil
			}
			args := append([]string{}, tc.args...)
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil || calls != 1 {
				t.Fatalf("valid view selection = %v, calls=%d", err, calls)
			}
		})
	}
}
