package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pders01/sp/internal/scratchpad"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type managerFactory func() (*scratchpad.Manager, error)

const privacyWarning = `WARNING: Privacy encrypts only your private notes, not the shared area.
Shared history, contribution sources, and page dates remain readable to other
programs and people with access to these files. Shared content is not trusted.
Existing plaintext copies, backups, snapshots, and synced history are NOT erased.
If you forget your passphrase there is NO recovery for your private notes.
This does not protect against malicious programs running as your user, including
programs that can capture your passphrase or change sp while it is unlocked.
Stop all old sp instances before continuing; old versions do not support privacy.
`

func newPrivacyCommand(factory managerFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use: "privacy", Short: "Opt in to private-note encryption or inspect migration status",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use: "enable", Short: "Interactively enable privacy or resume migration",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			prompt, err := newTerminalPrompt(cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			mgr, err := factory()
			if err != nil {
				return err
			}
			defer mgr.Lock()
			return enablePrivacy(mgr, prompt, cmd.ErrOrStderr())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "status", Short: "Show privacy and remaining plaintext dates without unlocking",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mgr, err := factory()
			if err != nil {
				return err
			}
			defer mgr.Lock()
			return writePrivacyStatus(mgr, cmd.OutOrStdout())
		},
	})
	cmd.AddCommand(newRecoverCommand(factory))
	return cmd
}

// The only production prompter accepts real terminal descriptors. Redirected
// input, environment variables, flags, and Cobra's injected string readers can
// never supply a passphrase.
type privacyPrompter interface {
	Line(string) (string, error)
	Password(string) ([]byte, error)
}

type terminalPrompt struct {
	input  *os.File
	output io.Writer
}

func newTerminalPrompt(input io.Reader, output io.Writer) (*terminalPrompt, error) {
	in, inOK := input.(*os.File)
	out, outOK := output.(*os.File)
	if !inOK || !outOK || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return nil, errors.New("privacy requires an interactive terminal; passphrases cannot be supplied through flags, environment variables, or redirected stdin")
	}
	return &terminalPrompt{input: in, output: out}, nil
}

func (p *terminalPrompt) Line(prompt string) (string, error) {
	if _, err := fmt.Fprint(p.output, prompt); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(p.input).ReadString('\n')
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), err
}

func (p *terminalPrompt) Password(prompt string) ([]byte, error) {
	if _, err := fmt.Fprint(p.output, prompt); err != nil {
		return nil, err
	}
	passphrase, err := term.ReadPassword(int(p.input.Fd()))
	fmt.Fprintln(p.output)
	return passphrase, err
}

func enablePrivacy(mgr *scratchpad.Manager, prompt privacyPrompter, output io.Writer) error {
	enabled, err := mgr.PrivacyEnabled()
	if err != nil {
		return err
	}
	if !enabled {
		if _, writeErr := fmt.Fprint(output, privacyWarning); writeErr != nil {
			return writeErr
		}
		confirmation, confirmationErr := prompt.Line("Type ENABLE PRIVACY to continue: ")
		if confirmationErr != nil {
			return fmt.Errorf("read confirmation: %w", confirmationErr)
		}
		if confirmation != "ENABLE PRIVACY" {
			return errors.New("privacy enable cancelled: confirmation did not match")
		}
	} else {
		fmt.Fprintln(output, "Privacy is already enabled. Unlock to resume any remaining plaintext migration.")
	}
	passphrase, err := prompt.Password("Passphrase: ")
	defer clear(passphrase)
	if err != nil {
		return fmt.Errorf("read passphrase: %w", err)
	}
	if len(passphrase) == 0 {
		return errors.New("passphrase must not be empty")
	}
	if !enabled {
		confirmation, err := prompt.Password("Confirm passphrase: ")
		defer clear(confirmation)
		if err != nil {
			return fmt.Errorf("confirm passphrase: %w", err)
		}
		if !bytes.Equal(passphrase, confirmation) {
			return errors.New("passphrases do not match; privacy was not enabled")
		}
	}
	if err := mgr.EnablePrivacy(passphrase); err != nil {
		return fmt.Errorf("enable privacy: %w; run sp privacy status to inspect remaining plaintext dates", err)
	}
	fmt.Fprintln(output, "Privacy enabled. Shared history and dates remain readable; old plaintext backups are not erased.")
	return writePrivacyStatus(mgr, output)
}

func writePrivacyStatus(mgr *scratchpad.Manager, output io.Writer) error {
	status, err := mgr.PrivacyStatus()
	if err != nil {
		return fmt.Errorf("inspect privacy status: %w", err)
	}
	state := "disabled"
	if status.Enabled {
		state = "enabled"
	}
	if _, writeErr := fmt.Fprintf(output, "Privacy: %s\nEncrypted dates: %d\nShared-only dates: %d\nRemaining plaintext dates (%d):\n",
		state, len(status.EncryptedDates), len(status.SharedOnlyDates), len(status.PlaintextDates)); writeErr != nil {
		return writeErr
	}
	for _, date := range status.PlaintextDates {
		if _, writeErr := fmt.Fprintln(output, "  "+date); writeErr != nil {
			return writeErr
		}
	}
	if status.Enabled && len(status.PlaintextDates) > 0 {
		_, err = fmt.Fprintln(output, "WARNING: Migration is incomplete. Run sp privacy enable to resume; stop old sp instances first.")
		return err
	}
	return nil
}

func newAppendCommand(factory managerFactory) *cobra.Command {
	var section, source string
	cmd := &cobra.Command{
		Use:   "append <date> --section <name> [--source <name>] -- '- bullet'...",
		Short: "Append bullets to the readable shared area without unlocking private notes",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			mgr, err := factory()
			if err != nil {
				return err
			}
			defer mgr.Lock()
			return mgr.Append(args[0], section, source, args[1:])
		},
	}
	cmd.Flags().StringVar(&section, "section", "", "Shared section name")
	cmd.Flags().StringVar(&source, "source", "sp", "Self-reported contribution source (not authenticated)")
	_ = cmd.MarkFlagRequired("section")
	return cmd
}

func newSharedCommand(factory managerFactory) *cobra.Command {
	return &cobra.Command{
		Use: "shared <date>", Short: "Read only a page's readable shared area, without unlocking",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := factory()
			if err != nil {
				return err
			}
			defer mgr.Lock()
			page, err := mgr.ReadShared(args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), page.Content)
			return err
		},
	}
}
