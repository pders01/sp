package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/templates"
	"github.com/pders01/sp/internal/tui"
)

func TestPrivacyValidationHistoryAndConcurrentSharedAppend(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Lock()
	const passphrase = "synthetic validation passphrase"
	dates := []string{"2024-01-15", "2023-01-15"}
	for _, date := range dates {
		if err := mgr.Save(&scratchpad.Scratchpad{
			Date: date, Content: "PRIVATE-" + date, AppliedTemplates: []string{"private-template-" + date},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := mgr.EnablePrivacy([]byte(passphrase)); err != nil {
		t.Fatal(err)
	}
	for _, date := range dates {
		if err := mgr.Append(date, "Updates", "synthetic source", []string{"- PUBLIC-" + date}); err != nil {
			t.Fatal(err)
		}
	}
	hooks := makePrivacyHooks(mgr)
	for _, unlocked := range []bool{false, true} {
		mgr.Lock()
		if unlocked {
			if err := hooks.Unlock([]byte(passphrase)); err != nil {
				t.Fatal(err)
			}
		}
		gotDates, contents, applied, err := loadAll(mgr)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotDates, dates) || len(applied) != 0 {
			t.Fatalf("archive dates/metadata = %v/%v", gotDates, applied)
		}
		for _, date := range dates {
			// Public ALL-date shared history is intentional, including after unlock.
			if !strings.Contains(contents[date], "PUBLIC-"+date) || strings.Contains(contents[date], "PRIVATE-") {
				t.Errorf("unlocked=%v: unexpected archive projection %q", unlocked, contents[date])
			}
			page, err := hooks.Load(date)
			if err != nil || page.Locked == unlocked || !page.HasPrivate {
				t.Fatalf("unlocked=%v: page=%+v, err=%v", unlocked, page, err)
			}
			if !unlocked && (page.Private != "" || len(page.Applied) != 0) {
				t.Fatal("locked page exposed private text or template metadata")
			}
		}
	}

	date := dates[0]
	before, err := hooks.Load(date)
	if err != nil {
		t.Fatal(err)
	}
	// A cooperating shared writer runs after the editor captured its private
	// baseline. Private saving must preserve that newer public contribution.
	if appendErr := mgr.Append(date, "Updates", "second synthetic source", []string{"- concurrent shared bullet"}); appendErr != nil {
		t.Fatal(appendErr)
	}
	if saveErr := hooks.Save(date, before.Private, "edited private"); saveErr != nil {
		t.Fatalf("shared-only change should not conflict with private edit: %v", saveErr)
	}
	after, err := mgr.GetByDate(date)
	if err != nil {
		t.Fatal(err)
	}
	if after.Content != "edited private" || !reflect.DeepEqual(after.AppliedTemplates, before.Applied) ||
		!strings.Contains(after.Agent.Content, "concurrent shared bullet") || len(after.Agent.Contributions) != 2 {
		t.Fatalf("private save lost metadata or shared update: %+v", after)
	}
	if saveErr := hooks.Save(date, before.Private, "stale private edit"); !errors.Is(saveErr, scratchpad.ErrConflict) {
		t.Fatalf("concurrent PRIVATE change must conflict: %v", saveErr)
	}
	latest, err := mgr.GetByDate(date)
	if err != nil || latest.Content != "edited private" {
		t.Fatalf("stale editor overwrote note: %+v, %v", latest, err)
	}
}

func TestPrivacyValidationRecoveryRoundTripAndSharedPreservation(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Lock()
	const date = "2024-01-15"
	const current = "current private baseline\r\n\tcode\r\n"
	const passphrase = "synthetic recovery validation passphrase"
	recovered := "\trecovered\r\n" + strings.Repeat("synthetic line\r\n", 120) + "last line\n\n"
	if err := mgr.Save(&scratchpad.Scratchpad{Date: date, Content: current, AppliedTemplates: []string{"retained-template"}}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnablePrivacy([]byte(passphrase)); err != nil {
		t.Fatal(err)
	}
	hooks := makePrivacyHooks(mgr)
	mgr.Lock()
	path, err := hooks.Recover(date, recovered)
	if err != nil {
		t.Fatal(err)
	}
	if _, readErr := mgr.ReadRecovery(path); !errors.Is(readErr, scratchpad.ErrLocked) {
		t.Fatalf("locked recovery read = %v, want ErrLocked", readErr)
	}
	if unlockErr := mgr.Unlock([]byte(passphrase)); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	recovery, err := mgr.ReadRecovery(path)
	if err != nil || recovery.Date != date || recovery.Content != recovered {
		t.Fatalf("recovery did not preserve exact private Markdown: %+v, %v", recovery, err)
	}
	page, err := hooks.Load(date)
	if err != nil || page.Private != current {
		t.Fatalf("sealing recovery unexpectedly replaced current note: %+v, %v", page, err)
	}
	if appendErr := mgr.Append(date, "Updates", "synthetic recovery source", []string{"- shared while reviewing recovery"}); appendErr != nil {
		t.Fatal(appendErr)
	}
	// Exercises the production recovery saver and conflict baseline without
	// starting EditRecoveredPrivate's terminal program.
	if saveErr := hooks.Save(date, page.Private, recovery.Content); saveErr != nil {
		t.Fatal(saveErr)
	}
	saved, err := mgr.GetByDate(date)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Content != recovered || !strings.Contains(saved.Agent.Content, "shared while reviewing recovery") ||
		!reflect.DeepEqual(saved.AppliedTemplates, []string{"retained-template"}) {
		t.Fatalf("recovery replacement lost private bytes, shared area, or template metadata: %+v", saved)
	}
	retained, err := mgr.ReadRecovery(path)
	if err != nil || retained.Content != recovered {
		t.Fatalf("saving removed/changed the recovery file: %+v, %v", retained, err)
	}
}

func TestPrivacyValidationSharedOnlyCLIStaysLocked(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Lock()
	if err := mgr.EnablePrivacy([]byte("synthetic shared-only validation passphrase")); err != nil {
		t.Fatal(err)
	}
	mgr.Lock()
	factory := func() (*scratchpad.Manager, error) { return mgr, nil }
	const date = "2020-01-15"
	if _, _, err := testCLI(factory, "append", date, "--section", "Updates", "--", "- historical public bullet"); err != nil {
		t.Fatal(err)
	}
	output, _, err := testCLI(factory, "shared", date)
	if err != nil || !strings.Contains(output, "historical public bullet") || mgr.IsUnlocked() {
		t.Fatalf("shared-only CLI read = %q, %v, unlocked=%v", output, err, mgr.IsUnlocked())
	}
	status, err := mgr.PrivacyStatus()
	if err != nil || len(status.EncryptedDates) != 0 || !reflect.DeepEqual(status.SharedOnlyDates, []string{date}) {
		t.Fatalf("append unexpectedly created private payload: %+v, %v", status, err)
	}
	page, err := makePrivacyHooks(mgr).Load(date)
	if err != nil || page.Private != "" || page.HasPrivate || !page.Locked || !strings.Contains(page.Shared, "historical public bullet") {
		t.Fatalf("shared-only notebook projection = %+v, %v", page, err)
	}
}

func TestPrivacyValidationPrivateTemplatePreservesSharedArea(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Lock()
	const date = "2024-01-15"
	if err := mgr.Save(&scratchpad.Scratchpad{Date: date, Content: "private baseline"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnablePrivacy([]byte("synthetic template validation passphrase")); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Append(date, "Updates", "synthetic source", []string{"- public template neighbor"}); err != nil {
		t.Fatal(err)
	}
	sharedBefore, err := mgr.ReadShared(date)
	if err != nil {
		t.Fatal(err)
	}
	definition := templates.Builtins()[0]
	apply := makeTemplateApplier(mgr, []templates.Definition{definition})
	result, err := apply(context.Background(), date, []tui.TemplateSelection{{ID: definition.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Content, "private baseline") || len(result.Applied) != 1 || result.Applied[0] != definition.ID {
		t.Fatalf("private template result = %+v", result)
	}
	sharedAfter, err := mgr.ReadShared(date)
	if err != nil || !reflect.DeepEqual(sharedAfter, sharedBefore) {
		t.Fatalf("private template modified public area: before=%+v after=%+v err=%v", sharedBefore, sharedAfter, err)
	}
	mgr.Lock()
	_, contents, applied, err := loadAll(mgr)
	if err != nil || contents[date] != sharedBefore.Content || len(applied) != 0 {
		t.Fatalf("private template leaked into archive projection: %v, %v, %v", contents, applied, err)
	}
}
