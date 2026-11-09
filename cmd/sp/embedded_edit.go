package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/editorterminal"
	"github.com/pders01/sp/internal/embeddedvim"
	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/tui"
	"github.com/spf13/cobra"
)

type memoryEditDeps struct {
	terminal     func(*os.File, *os.File) error
	assets       func() error
	configEditor func() (string, []string)
	unlock       func(*scratchpad.Manager, *os.File, *os.File) error
	run          func(context.Context, editorterminal.Options) (editorterminal.Report, error)
	resume       func(context.Context, editorterminal.Options, editorterminal.Report) (editorterminal.Report, error)
	await        func(*os.File, *os.File, <-chan os.Signal)
	builtin      func(*scratchpad.Manager, string) error
}

func newEditCommand(factory managerFactory) *cobra.Command {
	return newEditCommandWith(factory, memoryEditDeps{
		terminal: editorterminal.CheckTerminal,
		assets:   func() error { _, err := embeddedvim.BundledAssets(); return err },
		unlock: func(m *scratchpad.Manager, in, out *os.File) error {
			return unlockPrivateInteractive(m, in, out, tui.ThemePrefAuto, "Unlock notes")
		},
		configEditor: func() (string, []string) {
			cfg, notices := loadUIConfiguration()
			return cfg.Editor.Private, notices
		},
		run:    editorterminal.RunInMemory,
		resume: editorterminal.ResumeInMemory,
		await:  awaitMemoryRecovery,
		builtin: func(m *scratchpad.Manager, date string) error {
			cfg, notices := loadUIConfiguration()
			printConfigurationNotices(os.Stderr, notices)
			return editPrivateAndSave(m, date, cfg.UI.Theme)
		},
	})
}

// Fault seams are local to command construction, never user flags/environment.
func newEditCommandWith(factory managerFactory, d memoryEditDeps) *cobra.Command {
	// Empty means "follow editor.private in the configuration file"; the
	// value is only resolved after foreground-terminal admission, never
	// during flag parsing.
	var choice string
	cmd := &cobra.Command{
		Use:   "edit [YYYY-MM-DD]",
		Short: "Edit encrypted private notes with an explicit in-memory editor",
		Long:  "Edit one encrypted private note. --editor=vim opts into the experimental bundled Vim engine; unset follows editor.private in the configuration file. Plaintext editing is unchanged. No external editor is used.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return errors.New("edit accepts at most one YYYY-MM-DD date")
			}
			if len(args) == 1 {
				parsed, err := time.Parse("2006-01-02", args[0])
				if err != nil || parsed.Format("2006-01-02") != args[0] {
					return errors.New("edit requires a YYYY-MM-DD date")
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			// An explicit flag is validated before any admission; the configured
			// default is only read after the foreground-terminal check below.
			if choice != "" && choice != "builtin" && choice != "vim" {
				return errors.New("--editor must be builtin or vim")
			}
			in, inOK := cmd.InOrStdin().(*os.File)
			out, outOK := cmd.OutOrStdout().(*os.File)
			if !inOK || !outOK {
				return editorterminal.ErrTerminal
			}
			// No normal-store/config/identity discovery before foreground-terminal and
			// linked-public-program admission. Never resolve an external executable.
			if err := d.terminal(in, out); err != nil {
				return err
			}
			if choice == "" {
				configured, notices := d.configEditor()
				printConfigurationNotices(out, notices)
				choice = configured
			}
			if choice != "builtin" && choice != "vim" {
				return errors.New("--editor must be builtin or vim")
			}
			if choice == "vim" {
				if err := d.assets(); err != nil {
					return err
				}
			}
			mgr, err := factory()
			if err != nil {
				return errors.New("private editor: store unavailable")
			}
			// The owned runner below NEVER returns while its editor/input is retained.
			defer mgr.Lock()
			enabled, err := mgr.PrivacyEnabled()
			if err != nil {
				return errors.New("private editor: privacy state unavailable")
			}
			if !enabled {
				return errors.New("sp edit requires privacy enabled; existing sp editing is unchanged")
			}
			date := time.Now().Format("2006-01-02")
			if len(args) == 1 {
				date = args[0]
			}
			if choice == "builtin" {
				return d.builtin(mgr, date)
			}
			if _, err := fmt.Fprintln(out, "Experimental Vim: in-process capability isolation, not OS enforcement. Private-note limit: 1 MiB."); err != nil {
				return errors.New("private editor: output unavailable")
			}
			if !mgr.IsUnlocked() {
				if err := d.unlock(mgr, in, out); err != nil {
					return errors.New("private editor: unlock did not complete")
				}
			}
			return runOwnedMemoryEdit(cmd.Context(), mgr, date, in, out, d)
		},
	}
	cmd.Flags().StringVar(&choice, "editor", "", "Private editor: builtin or experimental vim; unset follows editor.private in config")
	return cmd
}

// editPrivateVim edits date (today when empty) with the experimental bundled
// Vim engine, mirroring the `sp edit --editor=vim` admission flow: no external
// editor, no plaintext files, and the same experimental notice and recovery
// ownership. Plaintext editing and the normal sp defaults are unchanged.
func editPrivateVim(mgr *scratchpad.Manager, date string) error {
	if _, err := embeddedvim.BundledAssets(); err != nil {
		return err
	}
	if err := editorterminal.CheckTerminal(os.Stdin, os.Stdout); err != nil {
		return err
	}
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	if _, err := fmt.Fprintln(os.Stdout, "Experimental Vim: in-process capability isolation, not OS enforcement. Private-note limit: 1 MiB."); err != nil {
		return errors.New("private editor: output unavailable")
	}
	if !mgr.IsUnlocked() {
		if err := unlockPrivateInteractive(mgr, os.Stdin, os.Stderr, tui.ThemePrefAuto, "Unlock notes"); err != nil {
			return errors.New("private editor: unlock did not complete")
		}
	}
	return runOwnedMemoryEdit(context.Background(), mgr, date, os.Stdin, os.Stdout, memoryEditDeps{
		run:    editorterminal.RunInMemory,
		resume: editorterminal.ResumeInMemory,
		await:  awaitMemoryRecovery,
	})
}

func memoryReportHeld(r editorterminal.Report) bool {
	return r.PendingInput || len(r.RetainedInput()) != 0 || (r.Editor != nil && !r.Editor.Retained().Editor.Closed)
}

func runOwnedMemoryEdit(ctx context.Context, mgr *scratchpad.Manager, date string, in, out *os.File, d memoryEditDeps) error {
	// Own catchable termination across the gaps between terminal programs too.
	// HUP/TERM/EOF must not abandon the draft while waiting for recovery input.
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, memoryEditSignals()...)
	defer signal.Stop(signals)
	options := editorterminal.Options{Input: in, Output: out, Manager: mgr, DocumentID: date, InMemory: editorcore.InMemoryFactory(embeddedvim.OpenBundledUI), ForwardEditorControls: true}
	report, err := d.run(ctx, options)
	for memoryReportHeld(report) {
		// A failed terminal never authorizes losing live or uncertain input, manager
		// relock or process exit. Reentry is physical and reuses this exact owner.
		fmt.Fprintln(out, "Editor/input remains owned. Keep this process open. Press Enter to reopen recovery controls; K then physical y confirms possible-loss recovery.")
		d.await(in, out, signals)
		report, err = d.resume(context.Background(), options, report)
		runtime.KeepAlive(mgr)
	}
	for _, artifact := range report.Outcome.Recoveries {
		fmt.Fprintln(out, "Encrypted recovery:", artifact.Path)
	}
	if report.Outcome.MayHaveLostEdits {
		fmt.Fprintln(out, "Known-copy recovery completed; unacknowledged edits may have been lost with your consent.")
	}
	if err != nil {
		return errors.New("private editor did not complete normally; review any encrypted recovery reported above")
	}
	return nil
}

// Canonical terminal input only. EOF/closed output is not loss consent. Wait for
// a physical interrupt before offering another read rather than spin, discover a
// fallback terminal or terminate with an unresolved draft. Hard kill/crash still
// loses RAM; this is an experimental path, not crash-proof persistence.
func awaitMemoryRecovery(in, out *os.File, signals <-chan os.Signal) {
	reader := bufio.NewReaderSize(in, 256)
	for {
		b, err := reader.ReadByte()
		if err == nil {
			if b == '\n' || b == '\r' {
				return
			}
			continue
		}
		if !errors.Is(err, io.EOF) {
			fmt.Fprintln(out, "Recovery input unavailable; draft remains in this process.")
		}
		<-signals
		fmt.Fprintln(out, "Draft still retained. Press Enter to try the same recovery screen again.")
	}
}
