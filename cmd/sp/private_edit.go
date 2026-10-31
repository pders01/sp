package main

import (
	"errors"
	"fmt"
	"io"
	"os"

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
		Inspect: inspect, IsUnlocked: mgr.IsUnlocked, Unlock: mgr.Unlock, Lock: mgr.Lock, AsyncUnlock: true,
		Load: func(date string) (tui.PrivatePage, error) {
			shared, err := inspect(date)
			if err != nil {
				return tui.PrivatePage{}, err
			}
			page := tui.PrivatePage{Shared: shared.Shared, HasPrivate: shared.HasPrivate, Locked: !mgr.IsUnlocked()}
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
		Save: makePolicySaver(mgr, true), Recover: mgr.SealRecovery,
	}
}

func unlockPrivateInteractive(mgr *scratchpad.Manager, input io.Reader, output io.Writer, theme, title string) error {
	prompt, err := newTerminalPrompt(input, output)
	if err != nil {
		return err
	}
	return tui.UnlockNotes(tui.UnlockOptions{
		Input: prompt.input, Output: output.(*os.File), Theme: theme, Title: title,
		IsUnlocked: mgr.IsUnlocked, Unlock: mgr.Unlock, Lock: mgr.Lock,
	})
}

func editPrivateAndSave(mgr *scratchpad.Manager, pickedDate string, preference ...string) error {
	theme := tui.ThemePrefAuto
	if len(preference) > 0 {
		theme = preference[0]
	}
	if !mgr.IsUnlocked() {
		if err := unlockPrivateInteractive(mgr, os.Stdin, os.Stderr, theme, "Unlock notes"); err != nil {
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
	return tui.EditPrivateWithTheme(page.Date, page.Content, makePolicySaver(mgr, true), mgr.SealRecovery, makeLoader(mgr), theme)
}
