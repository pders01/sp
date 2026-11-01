package scratchpad

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/pders01/sp/internal/templates"
)

func TestFirstPrivateSaveOnSharedOnlyInitializesCreated(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		name := "plaintext"
		if encrypted {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			m := setupTestManager(t)
			if encrypted {
				m = privateTestManager(t)
			}
			defer m.Lock()
			const date = "2024-01-01"
			if err := m.Append(date, "Updates", "synthetic-helper", []string{"public update"}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(m.storageDir, date+".json")
			before := readTestFile(t, path)
			for i := 0; i < 2; i++ {
				note, err := m.GetByDate(date)
				if err != nil {
					t.Fatal(err)
				}
				if !note.Created.IsZero() || !note.Modified.IsZero() || note.disk.Private != nil || note.disk.legacy != nil {
					t.Fatalf("opening shared-only note invented private metadata: %+v", note)
				}
				note.Content = "synthetic discarded draft" // cancel without saving
			}
			if _, err := m.ApplyTemplateSections(date, nil); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, readTestFile(t, path)) {
				t.Fatal("read/open/cancel/no-op template created a private payload")
			}
			note, loadErr := m.GetByDate(date)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			agent := note.Agent
			// A failed encoding must not make creation appear published.
			note.Content = string([]byte{0xff})
			if err := m.Save(note); err == nil {
				t.Fatal("invalid private text accepted")
			}
			if !note.Created.IsZero() || !note.Modified.IsZero() || !bytes.Equal(before, readTestFile(t, path)) {
				t.Fatal("failed first save changed private timestamps or shared record")
			}
			note.Content = "synthetic first personal writing"
			started := time.Now()
			injected := errors.New("synthetic directory sync failure")
			calls := 0
			loadErr = m.save(note, func(_ string) error {
				calls++
				if note.Created.IsZero() || !note.Created.Equal(note.Modified) {
					t.Fatalf("publication did not advance creation timestamp: %+v", note)
				}
				saved, err := m.GetByDate(date)
				if err != nil || !saved.Created.Equal(note.Created) {
					t.Fatalf("published creation timestamp = %+v, %v", saved, err)
				}
				return injected
			})
			if !errors.Is(loadErr, ErrDurabilityUnconfirmed) || !errors.Is(loadErr, injected) || calls != 1 {
				t.Fatalf("unconfirmed first save = %v, sync calls = %d", loadErr, calls)
			}
			if note.Created.Before(started) || note.Created.After(time.Now()) {
				t.Fatalf("creation time %v is outside first-save window", note.Created)
			}
			created := note.Created
			note.Content += "\nsecond save"
			if err := m.Save(note); err != nil {
				t.Fatal(err)
			}
			fresh, loadErr := NewManagerAt(m.storageDir)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			defer fresh.Lock()
			if encrypted {
				if err := fresh.Unlock(fixturePassphrase); err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(readTestFile(t, path), []byte("synthetic first personal writing")) {
					t.Fatal("private content was saved as plaintext")
				}
			}
			saved, loadErr := fresh.GetByDate(date)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if !saved.Created.Equal(created) || !saved.Modified.Equal(note.Modified) || saved.Content != note.Content {
				t.Fatalf("private save/reload changed content or timestamps: %+v", saved)
			}
			if !reflect.DeepEqual(saved.Agent, agent) {
				t.Fatal("private save changed the shared area")
			}
			assertNoSaveTemps(t, m)
		})
	}
}

func TestPrivateCreationPreservesExistingTimestamps(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		name := "plaintext"
		if encrypted {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			m := setupTestManager(t)
			if encrypted {
				m = privateTestManager(t)
			}
			defer m.Lock()
			for i, created := range []time.Time{{}, time.Date(2020, 2, 3, 4, 5, 6, 0, time.UTC)} {
				date := []string{"2024-01-01", "2024-01-02"}[i]
				modified := time.Date(2021, 2, 3, 4, 5, 6, 0, time.UTC)
				writeJSON(t, filepath.Join(m.storageDir, date+".json"), legacyDocument{Date: date, Content: "synthetic existing note", Created: created, Modified: modified})
				if encrypted {
					if err := m.EnablePrivacy(fixturePassphrase); err != nil {
						t.Fatal(err)
					}
				}
				note, loadErr2 := m.GetByDate(date)
				if loadErr2 != nil {
					t.Fatal(loadErr2)
				}
				if !note.Created.Equal(created) || !note.Modified.Equal(modified) {
					t.Fatalf("open/migration changed existing timestamps: %+v", note)
				}
				note.Content += "\nnew personal writing"
				if err := m.Save(note); err != nil {
					t.Fatal(err)
				}
				saved, loadErr2 := m.GetByDate(date)
				if loadErr2 != nil || !note.Created.Equal(created) || !saved.Created.Equal(created) {
					t.Fatalf("save changed existing creation timestamp: %+v, %v", saved, loadErr2)
				}
			}
		})
	}
}

func TestTemplateFirstPrivateSaveOnSharedOnlyInitializesCreated(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		name := "plaintext"
		if encrypted {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			m := setupTestManager(t)
			if encrypted {
				m = privateTestManager(t)
			}
			defer m.Lock()
			const date = "2024-01-01"
			if err := m.Append(date, "Updates", "synthetic-helper", []string{"public update"}); err != nil {
				t.Fatal(err)
			}
			note, err := m.ApplyTemplateSections(date, []templates.Section{{ID: "synthetic", Title: "Personal", Body: "synthetic writing"}})
			if err != nil {
				t.Fatal(err)
			}
			if note.Created.IsZero() || !note.Created.Equal(note.Modified) {
				t.Fatalf("first template save has no creation time: %+v", note)
			}
			saved, err := m.GetByDate(date)
			if err != nil || !saved.Created.Equal(note.Created) {
				t.Fatalf("template creation time not persisted: %+v, %v", saved, err)
			}
		})
	}
}
