package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/tui"
)

func makePrivacyHooks(mgr *scratchpad.Manager) tui.PrivacyHooks {
	return tui.PrivacyHooks{
		IsUnlocked: mgr.IsUnlocked,
		Unlock:     mgr.Unlock,
		Load: func(date string) (tui.PrivatePage, error) {
			// Read only the explicitly shared area before deciding whether a
			// private read is allowed. Legacy plaintext is never a preview.
			shared, err := mgr.ReadShared(date)
			if err != nil {
				return tui.PrivatePage{}, err
			}
			page := tui.PrivatePage{
				Shared: shared.Content, HasPrivate: shared.HasPrivate || shared.Legacy,
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
		Save:    makeSaver(mgr),
		Recover: mgr.SealRecovery,
	}
}

func editPrivateAndSave(mgr *scratchpad.Manager, pickedDate string) error {
	if !mgr.IsUnlocked() {
		prompt, err := newTerminalPrompt(os.Stdin, os.Stderr)
		if err != nil {
			return err
		}
		passphrase, err := prompt.Password("Unlock private notes — passphrase: ")
		defer clear(passphrase)
		if err != nil {
			return fmt.Errorf("read passphrase: %w", err)
		}
		if err := mgr.Unlock(passphrase); err != nil {
			return fmt.Errorf("unlock private notes: %w", err)
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
	return tui.EditPrivate(page.Date, page.Content, makeSaver(mgr), mgr.SealRecovery)
}
