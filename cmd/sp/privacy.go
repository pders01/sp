package main

import (
	"bufio"
	"bytes"
	"encoding/json"
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
When re-enabling a disabled store, use its EXISTING passphrase, not a new one.
Stop all old sp instances before continuing; old versions do not support privacy.
`

const privacyDisableWarning = `WARNING: Disabling privacy writes ALL daily private notes back to plaintext.
Private text, timestamps, and template metadata will be readable by programs and
people with access to these files, including backups, snapshots, and sync tools.
Shared contributions are preserved. Existing encrypted backups and recovery files
stay encrypted; privacy.json retains their passphrase-wrapped key. Keep that file
and the passphrase. Re-enabling uses the same key and does NOT erase plaintext
copies or revoke old backups. Stop ALL sp instances before continuing.
`

func newPrivacyCommand(factory managerFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use: "privacy", Short: "Manage private-note encryption",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use: "enable", Short: "Encrypt daily notes or resume migration",
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
		Use: "disable", Short: "Decrypt daily notes or resume reversal",
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
			return disablePrivacy(mgr, prompt, cmd.ErrOrStderr())
		},
	})
	var dates bool
	statusCommand := &cobra.Command{
		Use: "status", Short: "Inspect encryption state without unlocking",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mgr, err := factory()
			if err != nil {
				return err
			}
			defer mgr.Lock()
			return writePrivacyStatusDates(mgr, cmd.OutOrStdout(), dates)
		},
	}
	statusCommand.Flags().BoolVar(&dates, "dates", false, "List all dates by storage state")
	cmd.AddCommand(statusCommand)
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
	enabled, policyErr := mgr.PrivacyEnabled()
	if policyErr != nil {
		return policyErr
	}
	hasIdentity, policyErr := mgr.HasPrivacyIdentity()
	if policyErr != nil {
		return policyErr
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
	passwordPrompt := "Passphrase: "
	if hasIdentity {
		if _, err := fmt.Fprintln(output, "Use the existing passphrase. The retained key is reused; re-enabling does not rotate keys or change the passphrase."); err != nil {
			return err
		}
		passwordPrompt = "Existing passphrase: "
	}
	passphrase, policyErr := prompt.Password(passwordPrompt)
	defer clear(passphrase)
	if policyErr != nil {
		return fmt.Errorf("read passphrase: %w", policyErr)
	}
	if len(passphrase) == 0 {
		return errors.New("passphrase must not be empty")
	}
	if !hasIdentity {
		confirmation, err := prompt.Password("Confirm passphrase: ")
		defer clear(confirmation)
		if err != nil {
			return fmt.Errorf("confirm passphrase: %w", err)
		}
		if !bytes.Equal(passphrase, confirmation) {
			return errors.New("passphrases do not match; privacy was not enabled")
		}
	}
	if err := mgr.EnablePrivacyWithProgress(passphrase, privacyProgressReporter(output)); err != nil {
		return fmt.Errorf("enable privacy: %w; run sp privacy status --dates to inspect remaining plaintext dates", err)
	}
	fmt.Fprintln(output, "Privacy enabled. Shared history and dates remain readable; old plaintext backups are not erased.")
	return writePrivacyStatus(mgr, output)
}

func disablePrivacy(mgr *scratchpad.Manager, prompt privacyPrompter, output io.Writer) error {
	status, statusErr := mgr.PrivacyStatus()
	if statusErr != nil {
		return statusErr
	}
	if !status.Enabled {
		if err := mgr.DisablePrivacy(nil); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, "Privacy is already disabled. Daily private notes are plaintext.")
		return err
	}
	if _, err := fmt.Fprint(output, privacyDisableWarning); err != nil {
		return err
	}
	confirmation, statusErr := prompt.Line("Type DECRYPT ALL NOTES to continue: ")
	if statusErr != nil {
		return fmt.Errorf("read confirmation: %w", statusErr)
	}
	if confirmation != "DECRYPT ALL NOTES" {
		return errors.New("privacy disable cancelled: confirmation did not match")
	}
	passphrase, statusErr := prompt.Password("Existing passphrase: ")
	defer clear(passphrase)
	if statusErr != nil {
		return fmt.Errorf("read passphrase: %w", statusErr)
	}
	if err := mgr.DisablePrivacyWithProgress(passphrase, privacyProgressReporter(output)); err != nil {
		return fmt.Errorf("disable privacy: %w; run sp privacy status and retry sp privacy disable", err)
	}
	if _, err := fmt.Fprintln(output, "Privacy disabled. Daily private notes are plaintext; the wrapped key is retained for encrypted backups and recovery files."); err != nil {
		return err
	}
	return writePrivacyStatus(mgr, output)
}

func writePrivacyStatus(mgr *scratchpad.Manager, output io.Writer) error {
	return writePrivacyStatusDates(mgr, output, false)
}

func writePrivacyStatusDates(mgr *scratchpad.Manager, output io.Writer, dates bool) error {
	status, statusErr2 := mgr.PrivacyStatus()
	if statusErr2 != nil {
		return fmt.Errorf("inspect privacy status: %w", statusErr2)
	}
	hasIdentity, statusErr2 := mgr.HasPrivacyIdentity()
	if statusErr2 != nil {
		return fmt.Errorf("inspect privacy identity: %w", statusErr2)
	}
	state := "disabled"
	if status.Enabled {
		state = "enabled"
	}
	if status.Decrypting {
		state = "decrypting (incomplete)"
	}
	plaintextLabel := "Plaintext dates (normal; privacy disabled)"
	if status.Decrypting {
		plaintextLabel = "Plaintext dates (decryption in progress)"
	} else if status.Enabled {
		plaintextLabel = "Remaining plaintext dates"
	}
	if _, err := fmt.Fprintf(output, "Privacy: %s\nEncrypted dates: %d\nShared-only dates: %d\n%s: %d\n",
		state, len(status.EncryptedDates), len(status.SharedOnlyDates), plaintextLabel, len(status.PlaintextDates)); err != nil {
		return err
	}
	keyState := "none"
	if hasIdentity {
		keyState = "retained; keep privacy.json and the existing passphrase for encrypted backups and recovery files"
	}
	if _, err := fmt.Fprintln(output, "Wrapped key: "+keyState); err != nil {
		return err
	}
	if dates {
		for _, group := range []struct {
			label string
			dates []string
		}{
			{"Encrypted", status.EncryptedDates},
			{"Shared-only", status.SharedOnlyDates},
			{plaintextLabel, status.PlaintextDates},
		} {
			if _, err := fmt.Fprintln(output, group.label+" listing:"); err != nil {
				return err
			}
			for _, date := range group.dates {
				if _, err := fmt.Fprintln(output, "  "+date); err != nil {
					return err
				}
			}
		}
	} else if _, err := fmt.Fprintln(output, "Use sp privacy status --dates to list dates."); err != nil {
		return err
	}
	if status.Decrypting {
		_, statusErr2 = fmt.Fprintln(output, "WARNING: Decryption is incomplete; some private notes may already be plaintext. Normal access is blocked. Run sp privacy disable to resume; stop all sp instances first.")
		return statusErr2
	}
	if status.Enabled && len(status.PlaintextDates) > 0 {
		_, statusErr2 = fmt.Fprintln(output, "WARNING: Migration is incomplete. Run sp privacy enable to resume; stop old sp instances first.")
		return statusErr2
	}
	return nil
}

func newAppendCommand(factory managerFactory) *cobra.Command {
	var section, source string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "append <date> --section <name> [--source <name>] -- '- bullet'...",
		Short: "Append shared bullets without unlocking",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := scratchpad.ValidateAppend(args[0], section, source, args[1:]); err != nil {
				return err
			}
			mgr, err := factory()
			if err != nil {
				return err
			}
			defer mgr.Lock()
			result, err := mgr.AppendDetailed(args[0], section, source, args[1:])
			if err != nil {
				return err
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Report added and duplicate shared bullets as JSON")
	cmd.Flags().StringVar(&section, "section", "", "Shared section name")
	cmd.Flags().StringVar(&source, "source", "sp", "Self-reported contribution source (not authenticated)")
	_ = cmd.MarkFlagRequired("section")
	return cmd
}

func newSharedCommand(factory managerFactory) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use: "shared <date>", Short: "Read shared notes without unlocking",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			return scratchpad.ValidateSharedDate(args[0])
		},
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
			if jsonOutput {
				state := "ok"
				if !page.Exists {
					state = "missing"
				} else if strings.TrimSpace(page.Content) == "" {
					state = "no_shared"
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					Date    string `json:"date"`
					Status  string `json:"status"`
					Content string `json:"content"`
				}{page.Date, state, page.Content})
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), page.Content)
			return err
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Report shared text and missing/no-shared state as JSON")
	return cmd
}
