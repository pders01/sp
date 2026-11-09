package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/tui"
	"github.com/spf13/cobra"
)

const passphraseChangeWarning = `WARNING: This changes the passphrase wrapping the SAME private key.
Daily notes and encrypted recovery files are not rewritten. This is NOT data-key
rotation or revocation: old metadata/key copies can still decrypt same-key data.
Keep matching metadata with backups. Stop ALL other sp instances before changing
this passphrase. Only a fully migrated, enabled store is supported.
`

// The narrow interface permits truthful CLI outcome tests without replacing
// crypto, storage or publication hooks in production.
type passphraseChangeManager interface {
	PrivacyStatus() (scratchpad.PrivacyStatus, error)
	Unlock([]byte) error
	Lock()
	ChangePassphrase([]byte, []byte) (bool, error)
}

func newChangePassphraseCommand(factory managerFactory) *cobra.Command {
	return &cobra.Command{
		Use: "change-passphrase", Short: "Change the passphrase, not the private key",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Reject noninteractive invocation before constructing a store. No
			// secret flags, environment input, redirected stdin or tty fallback.
			prompt, err := newTerminalPrompt(cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			mgr, err := factory()
			if err != nil {
				return err
			}
			return changePrivacyPassphrase(mgr, &passphraseChangePrompt{prompt}, cmd.ErrOrStderr())
		},
	}
}

type passphraseChangePrompt struct{ *terminalPrompt }

func (p *passphraseChangePrompt) Password(label string) ([]byte, error) {
	return tui.ReadSecret(tui.SecretOptions{
		Input: p.input, Output: p.output.(*os.File), Theme: tui.ThemePrefAuto,
		Title: "Change passphrase", Label: strings.TrimSuffix(label, ": "),
		Details: "Same data key; old metadata/key copies remain usable.",
	})
}

func changePrivacyPassphrase(mgr passphraseChangeManager, prompt privacyPrompter, output io.Writer) error {
	defer mgr.Lock()
	status, err := mgr.PrivacyStatus()
	if err != nil {
		return fmt.Errorf("inspect passphrase-change policy: %w", err)
	}
	if !status.Enabled || status.Decrypting || len(status.PlaintextDates) != 0 {
		return errors.New("passphrase change requires a fully migrated enabled store; inspect sp privacy status")
	}
	if _, err = fmt.Fprint(output, passphraseChangeWarning); err != nil {
		return err
	}
	confirmation, err := prompt.Line("Type CHANGE PASSPHRASE to continue: ")
	if err != nil {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if confirmation != "CHANGE PASSPHRASE" {
		return errors.New("passphrase change cancelled: confirmation did not match")
	}
	current, err := prompt.Password("Current passphrase: ")
	defer clear(current)
	if err != nil {
		return fmt.Errorf("read current passphrase: %w", err)
	}
	if validationErr := scratchpad.ValidatePassphrase(current); validationErr != nil {
		return validationErr
	}
	// Authenticate before asking for a replacement. ChangePassphrase itself
	// authenticates afresh and checks publication-time policy/fingerprints.
	if err = mgr.Unlock(current); err != nil {
		return fmt.Errorf("authenticate current passphrase: %w", err)
	}
	mgr.Lock()
	replacement, err := prompt.Password("New passphrase: ")
	defer clear(replacement)
	if err != nil {
		return fmt.Errorf("read new passphrase: %w", err)
	}
	if validationErr := scratchpad.ValidatePassphrase(replacement); validationErr != nil {
		return validationErr
	}
	if bytes.Equal(current, replacement) {
		return errors.New("new passphrase must differ from the current passphrase; metadata was not changed")
	}
	repeated, err := prompt.Password("Confirm new passphrase: ")
	defer clear(repeated)
	if err != nil {
		return fmt.Errorf("confirm new passphrase: %w", err)
	}
	if !bytes.Equal(replacement, repeated) {
		return errors.New("new passphrases do not match; metadata was not changed")
	}
	if _, err = fmt.Fprintln(output, "Rewrapping the existing private key; interruption may leave a published but unconfirmed change."); err != nil {
		return err
	}
	published, err := mgr.ChangePassphrase(current, replacement)
	if err != nil {
		if published {
			return fmt.Errorf("passphrase metadata was changed but durability is unconfirmed; keep current metadata and retry with the passphrase it accepts, not an automatic rollback: %w", err)
		}
		return fmt.Errorf("passphrase metadata was not changed: %w", err)
	}
	if !published {
		_, err = fmt.Fprintln(output, "Passphrase unchanged. The private key and note files are unchanged.")
		return err
	}
	_, err = fmt.Fprintln(output, "Passphrase changed. Private key and daily/recovery files are unchanged. Session locked; use the new passphrase next time. Old metadata/key copies remain usable.")
	if err != nil {
		return fmt.Errorf("passphrase metadata was changed successfully, but the final status could not be written: %w", err)
	}
	return nil
}
