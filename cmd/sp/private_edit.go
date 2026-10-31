package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"filippo.io/age"

	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/tui"
)

func makePrivacyHooks(mgr *scratchpad.Manager) tui.PrivacyHooks {
	inspect := func(date string) (tui.PublicPage, error) {
		shared, err := mgr.ReadShared(date)
		if err != nil {
			return tui.PublicPage{}, err
		}
		return tui.PublicPage{Shared: shared.Content, HasPrivate: shared.HasPrivate || shared.Legacy}, nil
	}
	return tui.PrivacyHooks{
		Inspect:     inspect,
		IsUnlocked:  mgr.IsUnlocked,
		Unlock:      mgr.Unlock,
		Lock:        mgr.Lock,
		AsyncUnlock: true,
		Load: func(date string) (tui.PrivatePage, error) {
			// Read only the explicitly shared area before deciding whether a
			// private read is allowed. Legacy plaintext is never a preview.
			shared, err := inspect(date)
			if err != nil {
				return tui.PrivatePage{}, err
			}
			page := tui.PrivatePage{
				Shared: shared.Shared, HasPrivate: shared.HasPrivate,
				Locked: !mgr.IsUnlocked(),
			}
			if page.Locked {
				return page, nil
			}
			private, err := mgr.GetByDate(date)
			if errors.Is(err, scratchpad.ErrLocked) {
				page.Locked = true
				return page, nil
			}
			if err != nil {
				return tui.PrivatePage{}, err
			}
			page.Private = private.Content
			page.Applied = append([]string(nil), private.AppliedTemplates...)
			return page, nil
		},
		Save:    makePolicySaver(mgr, true),
		Recover: mgr.SealRecovery,
	}
}

// unlockPrivateWithRetry never rewrites a secret or offers an unmasked fallback.
// Only authentication failures retry; storage/metadata errors retain their cause.
func unlockPrivateWithRetry(prompt privacyPrompter, output io.Writer, unlock func([]byte) error, labels ...string) error {
	passwordPrompt, progress := "Unlock private notes — passphrase (empty cancels): ", "Unlocking private notes…"
	if len(labels) > 0 {
		passwordPrompt = labels[0]
	}
	if len(labels) > 1 {
		progress = labels[1]
	}
	for {
		passphrase, err := prompt.Password(passwordPrompt)
		if err != nil {
			clear(passphrase)
			return fmt.Errorf("read passphrase: %w", err)
		}
		if len(passphrase) == 0 {
			clear(passphrase)
			return errors.New("unlock private notes cancelled")
		}
		if validationErr := scratchpad.ValidatePassphrase(passphrase); validationErr != nil {
			clear(passphrase)
			if _, err := fmt.Fprintf(output, "Invalid passphrase input: %v. Try again, or submit empty input to cancel.\n", validationErr); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintln(output, progress); err != nil {
			clear(passphrase)
			return err
		}
		err = unlock(passphrase)
		clear(passphrase)
		if err == nil {
			return nil
		}
		var mismatch *age.NoIdentityMatchError
		if !errors.As(err, &mismatch) {
			return fmt.Errorf("unlock private notes: %w", err)
		}
		if _, err := fmt.Fprintln(output, "Incorrect passphrase. Try again, or submit an empty passphrase to cancel."); err != nil {
			return fmt.Errorf("write unlock retry: %w", err)
		}
	}
}

func editPrivateAndSave(mgr *scratchpad.Manager, pickedDate string, preference ...string) error {
	if !mgr.IsUnlocked() {
		prompt, err := newTerminalPrompt(os.Stdin, os.Stderr)
		if err != nil {
			return err
		}
		if err := unlockPrivateWithRetry(prompt, os.Stderr, mgr.Unlock); err != nil {
			return err
		}
	}
	var page *scratchpad.Scratchpad
	var err error
	if pickedDate == "" {
		page, err = mgr.GetToday()
	} else {
		page, err = mgr.GetByDate(pickedDate)
	}
	if err != nil {
		return fmt.Errorf("failed to load private scratchpad: %w", err)
	}
	theme := tui.ThemePrefAuto
	if len(preference) > 0 {
		theme = preference[0]
	}
	return tui.EditPrivateWithTheme(page.Date, page.Content, makePolicySaver(mgr, true), mgr.SealRecovery, makeLoader(mgr), theme)
}
