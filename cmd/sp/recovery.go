package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/pders01/sp/internal/editor"
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
	var plaintextDate string
	cmd := &cobra.Command{
		Use:   "recover <file> [--plaintext-date YYYY-MM-DD] | --list",
		Short: "List or review recovery before saving",
		Args: func(cmd *cobra.Command, args []string) error {
			if list {
				if cmd.Flags().Changed("plaintext-date") {
					return errors.New("--list cannot be combined with --plaintext-date")
				}
				return cobra.NoArgs(cmd, args)
			}
			if cmd.Flags().Changed("plaintext-date") {
				parsed, err := time.Parse(time.DateOnly, plaintextDate)
				if err != nil || parsed.Format(time.DateOnly) != plaintextDate {
					return errors.New("--plaintext-date must be YYYY-MM-DD")
				}
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
			if cmd.Flags().Changed("plaintext-date") {
				mgr, err := factory()
				if err != nil {
					return err
				}
				defer mgr.Lock()
				private, err := mgr.PrivacyEnabled()
				if err != nil {
					return err
				}
				if private {
					return errors.New("plaintext buffer review is unavailable while privacy is enabled; use encrypted recovery")
				}
				path, err := filepath.Abs(args[0])
				if err != nil {
					return err
				}
				ed, err := editor.NewEditor()
				if err != nil {
					return err
				}
				cfg, notices := loadUIConfiguration()
				return runApp(mgr, ed, tui.NewIconSet(cfg.UI.Icons), cfg,
					appStartup{recovery: &scratchpad.RecoveryReference{Date: plaintextDate, Path: path}, notices: notices})
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
			if err := unlockPrivateWithRetry(prompt, cmd.ErrOrStderr(), mgr.Unlock, "Unlock recovery — passphrase (empty cancels): ", "Unlocking encrypted recovery…"); err != nil {
				return err
			}
			recovery, err := mgr.ReadRecovery(args[0])
			if err != nil {
				return err
			}
			current, err := mgr.GetByDate(recovery.Date)
			if err != nil {
				return err
			}
			policy, err := recoverySavePolicy(mgr)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), policy+". Review before saving: this replaces private Markdown, not an automatic merge. The recovery file is retained.")
			cfg, notices := loadUIConfiguration()
			printConfigurationNotices(cmd.ErrOrStderr(), notices)
			return tui.EditRecoveredPrivateWithPolicy(recovery.Date, current.Content, recovery.Content, makePolicySaver(mgr, policy == "Saving encrypted"), mgr.SealRecovery, makeLoader(mgr), policy, cfg.UI.Theme)
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "List recovery paths without unlocking or reading note text")
	cmd.Flags().StringVar(&plaintextDate, "plaintext-date", "", "Review a legacy plaintext buffer for this date (privacy must be disabled)")
	return cmd
}
