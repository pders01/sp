package main

import (
	"fmt"

	"github.com/pders01/sp/internal/tui"
	"github.com/spf13/cobra"
)

func newRecoverCommand(factory managerFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "recover <recovery-file>",
		Short: "Unlock an encrypted recovery file for review in the built-in editor",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
}
