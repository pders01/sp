package scratchpad

import (
	"bytes"
	"encoding/json"
	"testing"
)

// This targets semantic validation shared by daily private reads and recovery,
// rather than strictJSON's syntax-only contract. It performs no I/O or KDF.
func FuzzDecodePrivateDocumentFollowup(f *testing.F) {
	for _, seed := range []string{
		`{"version":2,"date":"2024-02-29","content":"synthetic α\r\n\tno final newline","created":"2024-02-29T01:02:03.123456789+02:00","modified":"2024-03-01T02:03:04Z","applied_templates":["repeat","repeat",""]}`,
		`{"version":2,"date":"2024-02-29","content":"","created":"0001-01-01T00:00:00Z","modified":"0001-01-01T00:00:00Z"}`,
		`{"version":2,"date":"2024-02-29","content":"\ud83d\ude00 and literal �","created":"2024-02-29T00:00:00Z","modified":"2024-02-29T00:00:00Z","applied_templates":[]}`,
		`{"version":2,"date":"2024-02-29","content":null,"created":"2024-02-29T00:00:00Z","modified":"2024-02-29T00:00:00Z"}`,
		`{"version":2,"date":"2024-03-01","content":"wrong date","created":"2024-02-29T00:00:00Z","modified":"2024-02-29T00:00:00Z"}`,
		`{"version":2,"date":"2024-02-29","content":"missing timestamps"}`,
		`null`, `{}`, `[]`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > privacyFuzzLimit {
			return
		}
		document, err := decodePrivateDocument(data, privacyFuzzDate)
		if err != nil {
			if document != nil {
				t.Fatal("rejected private document returned partial content")
			}
			return
		}
		if document == nil || document.Version != 2 || document.Date != privacyFuzzDate {
			t.Fatal("accepted private document has wrong version/date")
		}
		canonical, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		again, err := decodePrivateDocument(canonical, privacyFuzzDate)
		if err != nil || !privacyFuzzEqualJSON(document, again) {
			t.Fatalf("private fields changed on canonical roundtrip: %v", err)
		}
		stable, err := json.Marshal(again)
		if err != nil || !bytes.Equal(canonical, stable) {
			t.Fatalf("private serialization is not stable: %v", err)
		}
		if _, err := decodePrivateDocument(canonical, "2024-03-01"); err == nil {
			t.Fatal("authenticated document would be accepted under a swapped date")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(canonical, &fields); err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{"version", "date", "content", "created", "modified"} {
			for _, null := range []bool{false, true} {
				mutated := make(map[string]json.RawMessage, len(fields))
				for key, value := range fields {
					mutated[key] = value
				}
				if null {
					mutated[required] = json.RawMessage(`null`)
				} else {
					delete(mutated, required)
				}
				invalid, err := json.Marshal(mutated)
				if err != nil {
					t.Fatal(err)
				}
				if got, err := decodePrivateDocument(invalid, privacyFuzzDate); err == nil || got != nil {
					t.Fatalf("accepted missing/null required private field %s (null=%v)", required, null)
				}
			}
		}
	})
}
