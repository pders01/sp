package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/templates"
	"github.com/pders01/sp/internal/tui"
	"github.com/spf13/cobra"
)

func testCLI(factory managerFactory, args ...string) (stdout, stderr string, executionErr error) {
	cmd := &cobra.Command{Use: "sp", SilenceErrors: true, SilenceUsage: true}
	cmd.AddCommand(newPrivacyCommand(factory), newAppendCommand(factory), newSharedCommand(factory))
	var output, diagnostic bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&diagnostic)
	cmd.SetIn(strings.NewReader("not a terminal\n"))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return output.String(), diagnostic.String(), err
}

func TestPrivacyCLIArgumentValidation(t *testing.T) {
	for _, args := range [][]string{
		{"privacy", "status", "extra"},
		{"privacy", "enable", "extra"},
		{"privacy", "enable", "--passphrase", "do-not-accept"},
		{"privacy", "enable", "--passphrase-file", "ignored"},
		{"privacy", "change"},
		{"privacy", "disable", "extra"},
		{"privacy", "disable", "--passphrase", "do-not-accept"},
		{"privacy", "disable", "--passphrase-file", "ignored"},
		{"privacy", "disable", "--yes"},
		{"privacy", "recover"},
		{"privacy", "recover", "one", "two"},
		{"privacy", "recover", "one", "--passphrase", "do-not-accept"},
		{"append", "2025-01-02", "--", "- item"},
		{"append", "2025-01-02", "--section", "Notes"},
		{"shared"},
		{"shared", "2025-01-02", "extra"},
		{"shared", "2025-01-02", "--private"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			factory := func() (*scratchpad.Manager, error) {
				t.Fatal("manager created before CLI argument validation")
				return nil, errors.New("unexpected manager creation")
			}
			if _, _, err := testCLI(factory, args...); err == nil {
				t.Fatal("expected invalid arguments to fail")
			}
		})
	}
}

func TestPrivacyCommandsRefuseNoninteractiveInput(t *testing.T) {
	factory := func() (*scratchpad.Manager, error) {
		t.Fatal("noninteractive privacy commands must not initialize storage")
		return nil, errors.New("unexpected manager creation")
	}
	for _, args := range [][]string{{"privacy", "enable"}, {"privacy", "disable"}, {"privacy", "recover", "synthetic.age"}} {
		_, _, err := testCLI(factory, args...)
		if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
			t.Fatalf("noninteractive %v = %v", args, err)
		}
	}
}

func TestTerminalPromptRejectsRedirectedDescriptors(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if _, err := newTerminalPrompt(input, output); err == nil {
		t.Fatal("regular files were accepted as terminal descriptors")
	}
}

func TestPrivacyStatusNoninteractive(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Lock()
	if err := mgr.Save(&scratchpad.Scratchpad{Date: "2025-01-02", Content: "private status sentinel"}); err != nil {
		t.Fatal(err)
	}
	factory := func() (*scratchpad.Manager, error) { return mgr, nil }
	output, _, err := testCLI(factory, "privacy", "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Privacy: disabled", "Remaining plaintext dates (1)", "2025-01-02"} {
		if !strings.Contains(output, want) {
			t.Errorf("status %q does not contain %q", output, want)
		}
	}
	if strings.Contains(output, "private status sentinel") {
		t.Fatal("status leaked note content")
	}
	if enableErr := mgr.EnablePrivacy([]byte("synthetic privacy test passphrase")); enableErr != nil {
		t.Fatal(enableErr)
	}
	mgr.Lock()
	output, _, err = testCLI(factory, "privacy", "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Privacy: enabled", "Encrypted dates: 1", "Remaining plaintext dates (0)"} {
		if !strings.Contains(output, want) {
			t.Errorf("enabled status %q does not contain %q", output, want)
		}
	}
	if mgr.IsUnlocked() {
		t.Fatal("status unlocked manager")
	}
}

func TestPrivacyStatusRejectsCorruptDocument(t *testing.T) {
	dir := t.TempDir()
	mgr, err := scratchpad.NewManagerAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(filepath.Join(dir, "2025-01-02.json"), []byte(`{"date":"2025-01-02","content":`), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	output, _, err := testCLI(func() (*scratchpad.Manager, error) { return mgr, nil }, "privacy", "status")
	if err == nil {
		t.Fatalf("corrupt status succeeded: %q", output)
	}
}

func TestPrivacyStatusRejectsCorruptKeyMetadata(t *testing.T) {
	dir := t.TempDir()
	mgr, err := scratchpad.NewManagerAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "privacy.json"), []byte(`{"version":999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testCLI(func() (*scratchpad.Manager, error) { return mgr, nil }, "privacy", "status"); err == nil {
		t.Fatal("status accepted corrupt key metadata")
	}
	if _, _, _, err := loadAll(mgr); err == nil {
		t.Fatal("preview loader accepted corrupt key metadata")
	}
}

type scriptedPrivacyPrompt struct {
	line          string
	passwords     []string
	passwordCalls int
	lineCalls     int
}

func (p *scriptedPrivacyPrompt) Line(string) (string, error) {
	p.lineCalls++
	return p.line, nil
}

func (p *scriptedPrivacyPrompt) Password(string) ([]byte, error) {
	if p.passwordCalls >= len(p.passwords) {
		return nil, fmt.Errorf("unexpected password prompt")
	}
	passphrase := []byte(p.passwords[p.passwordCalls])
	p.passwordCalls++
	return passphrase, nil
}

func TestPrivacyEnableConfirmationAndResume(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		passwords  []string
	}{
		{name: "cancel", line: "yes"},
		{name: "mismatch", line: "ENABLE PRIVACY", passwords: []string{"synthetic one", "synthetic two"}},
		{name: "empty", line: "ENABLE PRIVACY", passwords: []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := testManager(t)
			defer mgr.Lock()
			prompt := &scriptedPrivacyPrompt{line: tc.line, passwords: tc.passwords}
			var output bytes.Buffer
			if err := enablePrivacy(mgr, prompt, &output); err == nil {
				t.Fatal("expected enable to fail")
			}
			if enabled, err := mgr.PrivacyEnabled(); err != nil || enabled {
				t.Fatalf("cancelled setup changed privacy: %v, %v", enabled, err)
			}
			if tc.name == "cancel" && prompt.passwordCalls != 0 {
				t.Fatal("passphrase requested before typed confirmation")
			}
			for _, warning := range []string{"Shared history", "dates remain readable", "backups", "NO recovery", "malicious programs", "old sp instances"} {
				if !strings.Contains(output.String(), warning) {
					t.Errorf("missing warning %q", warning)
				}
			}
		})
	}

	mgr := testManager(t)
	defer mgr.Lock()
	const passphrase = "synthetic enable test passphrase"
	initial := &scriptedPrivacyPrompt{line: "ENABLE PRIVACY", passwords: []string{passphrase, passphrase}}
	var output bytes.Buffer
	if err := enablePrivacy(mgr, initial, &output); err != nil {
		t.Fatal(err)
	}
	if initial.lineCalls != 1 || initial.passwordCalls != 2 {
		t.Fatalf("initial prompts = %+v", initial)
	}
	mgr.Lock()
	resume := &scriptedPrivacyPrompt{passwords: []string{passphrase}}
	if err := enablePrivacy(mgr, resume, &output); err != nil {
		t.Fatal(err)
	}
	if resume.lineCalls != 0 || resume.passwordCalls != 1 {
		t.Fatalf("resume prompts = %+v; want existing passphrase only", resume)
	}
	if strings.Contains(output.String(), passphrase) {
		t.Fatal("passphrase appeared in command output")
	}
}

func TestAppendAndSharedPreservePrivateContent(t *testing.T) {
	for _, private := range []bool{false, true} {
		t.Run(fmt.Sprintf("privacy=%v", private), func(t *testing.T) {
			mgr := testManager(t)
			defer mgr.Lock()
			const date = "2025-01-02"
			const passphrase = "synthetic append test passphrase"
			note := &scratchpad.Scratchpad{Date: date, Content: "private append sentinel"}
			if err := mgr.Save(note); err != nil {
				t.Fatal(err)
			}
			if private {
				if err := mgr.EnablePrivacy([]byte(passphrase)); err != nil {
					t.Fatal(err)
				}
				mgr.Lock()
			}
			factory := func() (*scratchpad.Manager, error) { return mgr, nil }
			if _, _, err := testCLI(factory, "append", date, "--section", "Updates", "--source", "synthetic-agent", "--", "- one item", "- another item"); err != nil {
				t.Fatal(err)
			}
			output, _, err := testCLI(factory, "shared", date)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output, "one item") || !strings.Contains(output, "another item") {
				t.Fatalf("shared output = %q", output)
			}
			if strings.Contains(output, "private append sentinel") {
				t.Fatal("shared command leaked private content")
			}
			if private {
				if mgr.IsUnlocked() {
					t.Fatal("append/shared unlocked manager")
				}
				if unlockErr := mgr.Unlock([]byte(passphrase)); unlockErr != nil {
					t.Fatal(unlockErr)
				}
			}
			got, err := mgr.GetByDate(date)
			if err != nil {
				t.Fatal(err)
			}
			if got.Content != note.Content || len(got.Agent.Contributions) != 1 {
				t.Fatalf("append changed private content or lost attribution: %+v", got)
			}
		})
	}
}

func TestAppendDefaultSource(t *testing.T) {
	mgr := testManager(t)
	factory := func() (*scratchpad.Manager, error) { return mgr, nil }
	if _, _, err := testCLI(factory, "append", "2025-01-02", "--section", "Updates", "--", "- default source item"); err != nil {
		t.Fatal(err)
	}
	note, err := mgr.GetByDate("2025-01-02")
	if err != nil {
		t.Fatal(err)
	}
	if len(note.Agent.Contributions) != 1 || note.Agent.Contributions[0].Source != "sp" {
		t.Fatalf("default source = %+v", note.Agent.Contributions)
	}
}

func TestAppendValidationDelegatedToManager(t *testing.T) {
	mgr := testManager(t)
	factory := func() (*scratchpad.Manager, error) { return mgr, nil }
	for _, args := range [][]string{
		{"append", "../invalid", "--section", "Updates", "--", "- item"},
		{"append", "2025-02-30", "--section", "Updates", "--", "- item"},
		{"append", "2025-01-02", "--section", "", "--", "- item"},
		{"append", "2025-01-02", "--section", "Updates", "--", "- item\n## injected heading"},
	} {
		if _, _, err := testCLI(factory, args...); err == nil {
			t.Errorf("invalid append succeeded: %q", args)
		}
	}
}

func TestPrivatePreviewsAndHooks(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Lock()
	const date = "2025-01-02"
	const passphrase = "synthetic previews test passphrase"
	if err := mgr.Save(&scratchpad.Scratchpad{Date: date, Content: "private preview sentinel"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnablePrivacy([]byte(passphrase)); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Append(date, "Updates", "synthetic-agent", []string{"- shared preview"}); err != nil {
		t.Fatal(err)
	}
	mgr.Lock()
	hooks := makePrivacyHooks(mgr)
	page, err := hooks.Load(date)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Locked || !page.HasPrivate || page.Private != "" || !strings.Contains(page.Shared, "shared preview") {
		t.Fatalf("locked page = %+v", page)
	}
	for _, unlock := range []bool{false, true} {
		if unlock {
			if unlockErr := hooks.Unlock([]byte(passphrase)); unlockErr != nil {
				t.Fatal(unlockErr)
			}
		}
		public, inspectErr := hooks.Inspect(date)
		if inspectErr != nil || !public.HasPrivate || !strings.Contains(public.Shared, "shared preview") || strings.Contains(public.Shared, "private preview sentinel") {
			t.Fatalf("public inspection unlocked=%v: %+v, %v", unlock, public, inspectErr)
		}
		dates, contents, applied, loadErr := loadAll(mgr)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if len(dates) != 1 || !strings.Contains(contents[date], "shared preview") || strings.Contains(contents[date], "private preview sentinel") || len(applied[date]) != 0 {
			t.Fatalf("archive preview unlocked=%v: dates=%v, contents=%v, applied=%v", unlock, dates, contents, applied)
		}
	}
	page, err = hooks.Load(date)
	if err != nil || page.Locked || page.Private != "private preview sentinel" {
		t.Fatalf("unlocked page = %+v, %v", page, err)
	}
	if err := editAndSave(mgr, nil, date); err == nil {
		t.Fatal("private edit was allowed through external-editor path")
	}
}

func TestPartialPrivacyNeverPreviewsLegacyPrivateContent(t *testing.T) {
	dir := t.TempDir()
	mgr, err := scratchpad.NewManagerAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Lock()
	const date = "2025-01-02"
	const passphrase = "synthetic partial migration passphrase"
	if saveErr := mgr.Save(&scratchpad.Scratchpad{Date: date, Content: "legacy private sentinel"}); saveErr != nil {
		t.Fatal(saveErr)
	}
	path := filepath.Join(dir, date+".json")
	legacy, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if enableErr := mgr.EnablePrivacy([]byte(passphrase)); enableErr != nil {
		t.Fatal(enableErr)
	}
	// Synthetic partial migration / old-instance fixture only.
	if writeErr := os.WriteFile(path, legacy, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	mgr.Lock()
	var status bytes.Buffer
	if statusErr := writePrivacyStatus(mgr, &status); statusErr != nil {
		t.Fatal(statusErr)
	}
	if !strings.Contains(status.String(), date) || !strings.Contains(status.String(), "Migration is incomplete") {
		t.Fatalf("partial migration status = %q", status.String())
	}
	hooks := makePrivacyHooks(mgr)
	page, err := hooks.Load(date)
	if err != nil || !page.Locked || !page.HasPrivate || page.Private != "" || strings.Contains(page.Shared, "legacy private sentinel") {
		t.Fatalf("locked legacy page = %+v, %v", page, err)
	}
	for _, unlocked := range []bool{false, true} {
		if unlocked {
			if unlockErr := hooks.Unlock([]byte(passphrase)); unlockErr != nil {
				t.Fatal(unlockErr)
			}
		}
		public, inspectErr := hooks.Inspect(date)
		if inspectErr != nil || !public.HasPrivate || strings.Contains(public.Shared, "legacy private sentinel") {
			t.Fatalf("public legacy inspection unlocked=%v: %+v, %v", unlocked, public, inspectErr)
		}
		_, contents, _, loadErr := loadAll(mgr)
		if loadErr != nil || strings.Contains(contents[date], "legacy private sentinel") {
			t.Fatalf("legacy preview unlocked=%v: %q, %v", unlocked, contents[date], loadErr)
		}
	}
	page, err = hooks.Load(date)
	if err != nil || page.Locked || page.Private != "legacy private sentinel" {
		t.Fatalf("unlocked legacy page = %+v, %v", page, err)
	}
}

func TestPrivateTemplateRequiresUnlockBeforeRendering(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Lock()
	if err := mgr.EnablePrivacy([]byte("synthetic template test passphrase")); err != nil {
		t.Fatal(err)
	}
	mgr.Lock()
	// A missing file would fail rendering with a different error if rendering
	// happened before the unlock check.
	apply := makeTemplateApplier(mgr, []templates.Definition{{ID: "file", Name: "File", File: filepath.Join(t.TempDir(), "absent.md")}})
	_, err := apply(context.Background(), "2025-01-02", []tui.TemplateSelection{{ID: "file"}})
	if !errors.Is(err, scratchpad.ErrLocked) {
		t.Fatalf("locked template application = %v, want ErrLocked before rendering", err)
	}
}
