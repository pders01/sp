package main

import (
	"fmt"

	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/tui"
	"github.com/spf13/cobra"
)

func recordRetainedBuffer(mgr *scratchpad.Manager, date, path string, cause error) error {
	if err := mgr.RecordBufferRecovery(date, path); err != nil {
		return fmt.Errorf("%w; could not index recovery path: %v; keep the reported path", cause, err)
	}
	return cause
}

func newRecoverCommand(factory managerFactory) *cobra.Command {
	var list bool
	cmd := &cobra.Command{
		Use:   "recover <recovery-file> | --list",
		Short: "List recovery paths or unlock encrypted recovery for review",
		Args: func(cmd *cobra.Command, args []string) error {
			if list {
				return cobra.NoArgs(cmd, args)
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				mgr, err := factory()
				if err != nil {
					return err
				}
				defer mgr.Lock()
				refs, listErr := mgr.ListRecoveries()
				if len(refs) == 0 && listErr == nil {
					fmt.Fprintln(cmd.OutOrStdout(), "No recorded recovery artifacts.")
				} else if len(refs) > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Encrypted candidates are unauthenticated until unlock; recorded temporary paths may be stale.")
					tui.WriteRecoveryReport(cmd.OutOrStdout(), refs)
				}
				return listErr
			}
			prompt, err := newTerminalPrompt(cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			mgr, err := factory()
			if err != nil {
				return err
			}
			defer mgr.Lock()
			passphrase, err := prompt.Password("Unlock recovery — passphrase: ")
			defer clear(passphrase)
			if err != nil {
				return err
			}
			if unlockErr := mgr.Unlock(passphrase); unlockErr != nil {
				return unlockErr
			}
			recovery, err := mgr.ReadRecovery(args[0])
			if err != nil {
				return err
			}
			current, err := mgr.GetByDate(recovery.Date)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "Review recovered text before saving: this replaces private Markdown, not an automatic merge. The recovery file is retained.")
			return tui.EditRecoveredPrivate(recovery.Date, current.Content, recovery.Content, makeSaver(mgr), mgr.SealRecovery)
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "List recovery paths without unlocking or reading note text")
	return cmd
}
