package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/editorterminal"
	"github.com/pders01/sp/internal/embeddedvim"
	"github.com/pders01/sp/internal/privateedit"
	"github.com/pders01/sp/internal/scratchpad"
)

const memoryDate = "2000-01-01"
const memoryPassword = "synthetic-cli-fixture-only"

func memoryFixture(t *testing.T) *scratchpad.Manager {
	t.Helper()
	store := filepath.Join(t.TempDir(), "store")
	if os.MkdirAll(store, 0o700) != nil {
		t.Fatal("owned CLI store directory failed")
	}
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal("owned CLI identity failed")
	}
	recipient, err := age.NewScryptRecipient(memoryPassword)
	if err != nil {
		t.Fatal("owned CLI recipient failed")
	}
	recipient.SetWorkFactor(1)
	var wrapped bytes.Buffer
	writer, err := age.Encrypt(&wrapped, recipient)
	if err != nil {
		t.Fatal("owned CLI wrap failed")
	}
	if _, err = writer.Write([]byte(identity.String())); err != nil || writer.Close() != nil {
		t.Fatal("owned CLI wrap write failed")
	}
	metadata, err := json.Marshal(map[string]any{
		"version":          1,
		"recipient":        identity.Recipient().String(),
		"wrapped_identity": base64.StdEncoding.EncodeToString(wrapped.Bytes()),
	})
	if err != nil || os.WriteFile(filepath.Join(store, "privacy.json"), metadata, 0o600) != nil {
		t.Fatal("owned CLI metadata write failed")
	}
	m, err := scratchpad.NewManagerAt(store)
	if err != nil {
		t.Fatal("owned CLI fixture setup failed")
	}
	if m.Unlock([]byte(memoryPassword)) != nil {
		t.Fatal("owned CLI privacy setup failed")
	}
	note, err := m.GetByDate(memoryDate)
	if err != nil {
		t.Fatal("owned CLI date setup failed")
	}
	note.Content = "synthetic original"
	if m.SaveWithPrivacyPolicy(note, true) != nil {
		t.Fatal("owned CLI save setup failed")
	}
	t.Cleanup(m.Lock)
	return m
}
func memoryDeps(t *testing.T) memoryEditDeps {
	t.Helper()
	return memoryEditDeps{
		terminal: func(*os.File, *os.File) error { return nil },
		assets:   func() error { _, err := embeddedvim.BundledAssets(); return err },
		configEditor: func() (string, []string) {
			return "builtin", nil
		},
		unlock: func(*scratchpad.Manager, *os.File, *os.File) error {
			t.Fatal("unexpected unlock in owned test")
			return nil
		},
		builtin: func(*scratchpad.Manager, string) error { t.Fatal("Vim path fell back to builtin"); return nil },
		await:   func(*os.File, *os.File, <-chan os.Signal) { t.Fatal("unexpected retained CLI transport") },
		resume: func(context.Context, editorterminal.Options, editorterminal.Report) (editorterminal.Report, error) {
			t.Fatal("unexpected CLI resume")
			return editorterminal.Report{}, nil
		},
	}
}
func memoryCommand(t *testing.T, f managerFactory, d memoryEditDeps, args ...string) error {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "public-output")
	if err != nil {
		t.Fatal("owned output setup failed")
	}
	defer out.Close()
	cmd := newEditCommandWith(f, d)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetIn(os.Stdin)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	return cmd.Execute()
}
func memoryEx(t *testing.T, e *privateedit.Embedded, command string) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.Type(ctx, ":"+command); err != nil {
		t.Fatal("owned command prompt failed")
	}
	if _, err := e.Key(ctx, "Enter"); err != nil {
		t.Fatal("owned command freeze failed")
	}
}

func TestMemoryEditAdmissionBeforeFactory(t *testing.T) {
	for _, tc := range []string{"bad-choice", "bad-date", "terminal", "assets", "bad-config"} {
		t.Run(tc, func(t *testing.T) {
			called := false
			d := memoryDeps(t)
			args := []string{"--editor=vim", memoryDate}
			switch tc {
			case "bad-choice":
				args = []string{"--editor=native", memoryDate}
			case "bad-date":
				args = []string{"--editor=vim", "not-a-date"}
			case "terminal":
				d.terminal = func(*os.File, *os.File) error { return editorterminal.ErrTerminal }
			case "assets":
				d.assets = func() error { return embeddedvim.ErrAssets }
			case "bad-config":
				d.configEditor = func() (string, []string) { return "native", nil }
				args = []string{memoryDate}
			}
			err := memoryCommand(t, func() (*scratchpad.Manager, error) { called = true; return nil, errors.New("forbidden factory") }, d, args...)
			if err == nil || called {
				t.Fatal("invalid entry touched normal store or skipped admission")
			}
		})
	}
}

func TestMemoryEditCancelledBeforeAdmissionDoesNotOpenStore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := memoryDeps(t)
	d.terminal = func(*os.File, *os.File) error { t.Fatal("cancelled command inspected terminal"); return nil }
	cmd := newEditCommandWith(func() (*scratchpad.Manager, error) {
		t.Fatal("cancelled command opened store")
		return nil, nil
	}, d)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"--editor=vim", memoryDate})
	if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled command did not stop before admission")
	}
}

func TestMemoryEditRealBundledLifecycle(t *testing.T) {
	for _, scenario := range []string{"checkpoint", "cancel", "relock", "conflict"} {
		t.Run(scenario, func(t *testing.T) {
			m := memoryFixture(t)
			d := memoryDeps(t)
			called := 0
			expected := "synthetic original"
			d.run = func(ctx context.Context, o editorterminal.Options) (editorterminal.Report, error) {
				called++
				if o.Manager != m || o.DocumentID != memoryDate || o.InMemory == nil || o.Experiment != nil || o.Driver != nil || !o.ForwardEditorControls {
					t.Fatal("application selected wrong factory or metadata")
				}
				e, err := privateedit.OpenInMemory(ctx, m, editorcore.Options{DocumentID: memoryDate, Width: 50, Height: 10}, o.InMemory)
				if err != nil || e == nil {
					t.Fatal("application bundled lifecycle open failed")
				}
				if _, err = e.Type(ctx, "AX"); err != nil {
					t.Fatal("application typed edit failed")
				}
				if _, err = e.Key(ctx, "Escape"); err != nil {
					t.Fatal("application mode exit failed")
				}
				var r privateedit.Result
				switch scenario {
				case "checkpoint":
					memoryEx(t, e, "w")
					r, err = e.Checkpoint(ctx)
					if err != nil || r.Closed || !r.Published {
						t.Fatal("application checkpoint failed")
					}
					if _, err = e.Type(ctx, "u"); err != nil {
						t.Fatal("application checkpoint lost history")
					}
					memoryEx(t, e, "wq")
					r, err = e.Finish(ctx)
				case "cancel":
					memoryEx(t, e, "q!")
					r, err = e.Cancel(ctx, false)
					if !errors.Is(err, privateedit.ErrConfirmationRequired) || r.Closed {
						t.Fatal("q bang granted loss consent")
					}
					r, err = e.Cancel(ctx, true)
				case "relock":
					m.Lock()
					memoryEx(t, e, "wq")
					r, err = e.Finish(ctx)
					if !errors.Is(err, scratchpad.ErrLocked) || r.Closed {
						t.Fatal("relock did not revoke save authority")
					}
					r, err = e.Cancel(ctx, true)
				case "conflict":
					note, loadErr := m.GetByDate(memoryDate)
					if loadErr != nil {
						t.Fatal("owned conflict fixture failed")
					}
					note.Content = "synthetic concurrent"
					if m.SaveWithPrivacyPolicy(note, true) != nil {
						t.Fatal("owned concurrent save failed")
					}
					expected = note.Content
					memoryEx(t, e, "wq")
					r, err = e.Finish(ctx)
					if !errors.Is(err, scratchpad.ErrConflict) || r.Closed {
						t.Fatal("application conflict did not retain owner")
					}
					r, err = e.Cancel(ctx, true)
				}
				if err != nil || !r.Closed {
					t.Fatal("application lifecycle did not release safely")
				}
				if scenario != "checkpoint" && (len(r.Recoveries) == 0 || !r.Recoveries[0].Durable) {
					t.Fatal("application cancellation lacks durable encrypted recovery")
				}
				return editorterminal.Report{Editor: e, Outcome: r}, nil
			}
			err := memoryCommand(t, func() (*scratchpad.Manager, error) { return m, nil }, d, "--editor=vim", memoryDate)
			if err != nil || called != 1 || m.IsUnlocked() {
				t.Fatal("application command failed or did not relock after closure")
			}
			if m.Unlock([]byte(memoryPassword)) != nil {
				t.Fatal("owned fixture reauthentication failed")
			}
			note, err := m.GetByDate(memoryDate)
			if err != nil || note.Content != expected {
				t.Fatal("application canonical bytes changed")
			}
		})
	}
}

func TestMemoryEditRetainsOwnerBeforeReentryAndRelock(t *testing.T) {
	m := memoryFixture(t)
	d := memoryDeps(t)
	var owner *privateedit.Embedded
	waits, resumes := 0, 0
	d.run = func(ctx context.Context, o editorterminal.Options) (editorterminal.Report, error) {
		var err error
		owner, err = privateedit.OpenInMemory(ctx, m, editorcore.Options{DocumentID: memoryDate}, o.InMemory)
		if err != nil {
			t.Fatal("retained application setup failed")
		}
		if _, err = owner.Type(ctx, "AX"); err != nil {
			t.Fatal("retained application input failed")
		}
		if _, err = owner.Key(ctx, "Escape"); err != nil {
			t.Fatal("retained application mode failed")
		}
		return editorterminal.Report{Editor: owner}, editorterminal.ErrRetained
	}
	d.await = func(*os.File, *os.File, <-chan os.Signal) {
		waits++
		if !m.IsUnlocked() || owner.Retained().Editor.Closed {
			t.Fatal("manager relocked or owner released before physical reentry")
		}
	}
	d.resume = func(ctx context.Context, o editorterminal.Options, r editorterminal.Report) (editorterminal.Report, error) {
		resumes++
		if r.Editor != owner || o.Manager != m {
			t.Fatal("reentry replaced owner or manager")
		}
		if resumes == 1 {
			return r, editorterminal.ErrTerminal
		}
		memoryEx(t, owner, "q!")
		result, err := owner.Cancel(ctx, true)
		if err != nil || !result.Closed {
			t.Fatal("owned reentry fixture release failed")
		}
		return editorterminal.Report{Editor: owner, Outcome: result}, nil
	}
	if memoryCommand(t, func() (*scratchpad.Manager, error) { return m, nil }, d, "--editor=vim", memoryDate) != nil || waits != 2 || resumes != 2 || m.IsUnlocked() {
		t.Fatal("retained reentry/closure ordering failed")
	}
}

func TestMemoryEditConfiguredDefault(t *testing.T) {
	t.Run("config vim is honored", func(t *testing.T) {
		d := memoryDeps(t)
		d.configEditor = func() (string, []string) { return "vim", []string{"notice"} }
		assetsCalled := false
		d.assets = func() error { assetsCalled = true; return nil }
		d.unlock = func(*scratchpad.Manager, *os.File, *os.File) error { return nil }
		runs := 0
		d.run = func(context.Context, editorterminal.Options) (editorterminal.Report, error) {
			runs++
			return editorterminal.Report{}, nil
		}
		if err := memoryCommand(t, func() (*scratchpad.Manager, error) { return memoryFixture(t), nil }, d, memoryDate); err != nil {
			t.Fatalf("configured vim default failed: %v", err)
		}
		if runs != 1 || !assetsCalled {
			t.Fatalf("vim runs=%d assets=%v, want 1/true", runs, assetsCalled)
		}
	})
	t.Run("explicit builtin overrides configured vim", func(t *testing.T) {
		d := memoryDeps(t)
		d.configEditor = func() (string, []string) { return "vim", nil }
		d.assets = func() error { t.Fatal("builtin must not admit vim assets"); return nil }
		runs := 0
		d.builtin = func(*scratchpad.Manager, string) error { runs++; return nil }
		if err := memoryCommand(t, func() (*scratchpad.Manager, error) { return memoryFixture(t), nil }, d, "--editor=builtin", memoryDate); err != nil {
			t.Fatalf("explicit builtin failed: %v", err)
		}
		if runs != 1 {
			t.Fatalf("builtin runs = %d, want 1", runs)
		}
	})
	t.Run("config is not read before terminal admission", func(t *testing.T) {
		d := memoryDeps(t)
		d.terminal = func(*os.File, *os.File) error { return editorterminal.ErrTerminal }
		d.configEditor = func() (string, []string) { t.Fatal("config read before terminal admission"); return "vim", nil }
		if err := memoryCommand(t, func() (*scratchpad.Manager, error) { return nil, errors.New("forbidden") }, d, memoryDate); err == nil {
			t.Fatal("expected terminal rejection")
		}
	})
}
