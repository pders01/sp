package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/scratchpad"
	"github.com/spf13/cobra"
)

func TestHelpIsCompactAndDoesNotOpenStore(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
		max  int
	}{
		{
			name: "root", args: []string{"--help"}, max: 850,
			want: []string{"Usage:", "Examples:", "Edit today", "Enter/e: edit", "e: edit directly",
				"Shared notes:", "append", "shared", "Privacy:", "privacy", "Utilities:", "completion", "help",
				"--calendar", "--notebook", "--version", "without unlocking"},
		},
		{
			name: "privacy", args: []string{"privacy", "--help"}, max: 550,
			want: []string{"sp privacy [command]", "disable", "enable", "recover", "status", "without unlocking"},
		},
		{
			name: "append", args: []string{"append", "--help"}, max: 700,
			want: []string{"without unlocking", "sp append <date>", "--section", "--source", "not authenticated", "-- '- bullet'"},
		},
		{
			name: "shared", args: []string{"shared", "--help"}, max: 350,
			want: []string{"without unlocking", "sp shared <date>"},
		},
		{
			name: "disable", args: []string{"privacy", "disable", "--help"}, max: 350,
			want: []string{"Decrypt daily notes", "resume reversal", "sp privacy disable"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newRootCommand(func() (*scratchpad.Manager, error) {
				t.Fatal("help must not initialize storage")
				return nil, nil
			})
			cmd.RunE = func(*cobra.Command, []string) error {
				t.Fatal("help must not open the scratchpad")
				return nil
			}
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			help := output.String()
			for _, want := range tc.want {
				if !strings.Contains(help, want) {
					t.Errorf("help missing %q: %s", want, help)
				}
			}
			if len(help) > tc.max {
				t.Errorf("help exceeds compact output budget: %d > %d bytes", len(help), tc.max)
			}
			if strings.Contains(help, "\x1b") {
				t.Fatal("help must remain plain text for agents and pipes")
			}
			if tc.name == "root" {
				last := -1
				for _, heading := range []string{"Examples:", "Shared notes:", "Privacy:", "Utilities:", "Flags:"} {
					next := strings.Index(help, heading)
					if next <= last {
						t.Fatalf("help sections out of order: %q", heading)
					}
					last = next
				}
			}
		})
	}
}
