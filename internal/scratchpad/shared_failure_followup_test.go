package scratchpad

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSharedFollowupMarkdownFailurePreservesProtectedBytes(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		for _, markdown := range []struct{ name, content string }{
			{"duplicate-heading", "intro\r\n### Updates\r\n- existing\r\n### Updates\r\n"},
			{"unclosed-fence", "### Updates\n```md\n- existing\n"},
		} {
			name := "legacy/"
			if encrypted {
				name = "encrypted/"
			}
			t.Run(name+markdown.name, func(t *testing.T) {
				var m *Manager
				if encrypted {
					m = privateTestManager(t)
				} else {
					m = setupTestManager(t)
				}
				defer m.Lock()
				const date = "2024-02-29"
				note := &Scratchpad{Date: date, Content: "synthetic private α\r\n\tno final newline", AppliedTemplates: []string{"repeat", "repeat", ""}, Agent: AgentArea{Content: markdown.content}}
				if err := m.Save(note); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(m.storageDir, date+".json")
				before := readTestFile(t, path)
				for _, locked := range []bool{false, true} {
					if locked {
						m.Lock()
					}
					page, err := m.ReadShared(date)
					if err != nil || page == nil || page.Content != markdown.content || !page.HasPrivate || page.Legacy == encrypted {
						t.Fatalf("shared inspection (locked=%v) = %+v, %v", locked, page, err)
					}
					if !bytes.Equal(before, readTestFile(t, path)) {
						t.Fatal("inspection rewrote the protected document")
					}
					result, err := m.AppendDetailed(date, "Updates", "synthetic agent", []string{"new bullet"})
					if err == nil || result != nil || errors.Is(err, ErrDurabilityUnconfirmed) {
						t.Fatalf("Markdown rejection (locked=%v) = %+v, %v", locked, result, err)
					}
					if !bytes.Equal(before, readTestFile(t, path)) {
						t.Fatal("failed append changed private/ciphertext, shared bytes, or provenance")
					}
					assertNoSaveTemps(t, m)
				}
			})
		}
	}
}

func TestSharedFollowupMetadataFailuresPreserveCiphertext(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt", "decrypting"} {
		t.Run(mode, func(t *testing.T) {
			m := privateTestManager(t)
			defer m.Lock()
			const date = "2024-02-29"
			if err := m.Save(&Scratchpad{Date: date, Content: "synthetic protected metadata failure", Agent: AgentArea{Content: "public α\r\n"}}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(m.storageDir, date+".json")
			before := readTestFile(t, path)
			metadataPath := filepath.Join(m.storageDir, "privacy.json")
			switch mode {
			case "missing":
				if err := os.Remove(metadataPath); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(metadataPath, []byte(`{"version":999}`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "decrypting":
				metadata, _, err := m.readMetadata()
				if err != nil {
					t.Fatal(err)
				}
				metadata.Version, metadata.State = 2, "decrypting"
				writeJSON(t, metadataPath, metadata)
			}
			var metadataBefore []byte
			if mode != "missing" {
				metadataBefore = readTestFile(t, metadataPath)
			}
			fresh, err := NewManagerAt(m.storageDir)
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.Lock()
			// Exercise both the manager that observed the original policy and
			// a fresh process with no observed-policy or unlocked-key cache.
			for _, reader := range []*Manager{m, fresh} {
				page, err := reader.ReadShared(date)
				if mode == "decrypting" {
					if err != nil || page == nil || page.Content != "public α\r\n" {
						t.Fatalf("transition inspection = %+v, %v", page, err)
					}
				} else if err == nil || page != nil {
					t.Fatalf("invalid policy inspection = %+v, %v", page, err)
				}
				result, err := reader.AppendDetailed(date, "Updates", "synthetic agent", []string{"new"})
				if err == nil || result != nil || errors.Is(err, ErrDurabilityUnconfirmed) {
					t.Fatalf("invalid policy append = %+v, %v", result, err)
				}
				if mode == "decrypting" && !errors.Is(err, ErrPrivacyDecrypting) {
					t.Fatalf("transition append = %v", err)
				}
				if !bytes.Equal(before, readTestFile(t, path)) {
					t.Fatal("shared operation rewrote ciphertext after policy failure/transition")
				}
				if mode == "missing" {
					if _, err := os.Lstat(metadataPath); !os.IsNotExist(err) {
						t.Fatalf("shared operation recreated metadata: %v", err)
					}
				} else if !bytes.Equal(metadataBefore, readTestFile(t, metadataPath)) {
					t.Fatal("shared operation rewrote policy metadata")
				}
				assertNoSaveTemps(t, reader)
			}
		})
	}
}
