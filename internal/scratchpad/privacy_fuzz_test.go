package scratchpad

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"filippo.io/age"
)

// All fuzz entry points bound the combined input before parsing or splitting.
// They neither touch the filesystem nor unlock identities. In particular, no
// passphrase KDF runs in a fuzz callback. Run campaigns separately from tests.
const privacyFuzzLimit = 64 << 10
const privacyFuzzDate = "2024-02-29"

func privacyFuzzCiphertext(f *testing.F) []byte {
	f.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		f.Fatal(err)
	}
	payload, err := encryptAge([]byte("synthetic fuzz seed"), identity.Recipient())
	if err != nil {
		f.Fatal(err)
	}
	return payload
}

// Compare serialized meaning rather than time.Time location internals or an
// optional empty template slice versus nil (both intentionally omit the field).
func privacyFuzzEqualJSON(a, b any) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

func FuzzDecodeRecord(f *testing.F) {
	for _, seed := range []string{
		`{"date":"2024-02-29","content":"legacy\ntext","applied_templates":["a","a",""]}`,
		`{"date":"2024-02-29","content":"\ud83d\ude00 and literal �","applied_templates":[],"created":"2024-02-29T00:00:00+00:00"}`,
		`{"version":1,"date":"2024-02-29","content":"legacy","agent":{"content":"shared"}}`,
		`{"version":2,"date":"2024-02-29","agent":{"content":"shared"}}`,
		`{"version":99,"date":"2024-02-29","agent":{"content":"shared"}}`,
		`{"version":2,"version":1,"date":"2024-02-29","agent":{"content":"shared"}}`,
		`{"version":2,"date":"2024-02-29","agent":null}`,
		`{"version":2,"date":"2024-02-29","agent":{"content":"shared"},"Private":null}`,
		`null`, `[]`, `{`,
	} {
		f.Add([]byte(seed))
	}
	seed, err := json.Marshal(diskRecord{
		Version: 2, Date: privacyFuzzDate, Agent: AgentArea{Content: "### Updates\r\n"},
		Private: &privateEnvelope{Format: privateFormat, Payload: base64.StdEncoding.EncodeToString(privacyFuzzCiphertext(f))},
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > privacyFuzzLimit {
			return
		}
		r, err := decodeRecord(data, privacyFuzzDate)
		if err != nil {
			return
		}
		if r == nil || r.Date != privacyFuzzDate || r.Version < 0 || r.Version > 2 {
			t.Fatalf("accepted invalid record: %+v", r)
		}
		encoded, err := marshalRecord(r)
		if err != nil {
			t.Fatalf("accepted record cannot be serialized: %v", err)
		}
		again, err := decodeRecord(encoded, privacyFuzzDate)
		if err != nil {
			t.Fatalf("serialized record cannot be decoded: %v", err)
		}
		if r.Version != again.Version || r.Date != again.Date || !reflect.DeepEqual(r.Private, again.Private) || !privacyFuzzEqualJSON(r.Agent, again.Agent) {
			t.Fatal("roundtrip changed version/date, opaque private payload, or shared data")
		}
		if (r.legacy == nil) != (again.legacy == nil) {
			t.Fatal("roundtrip changed legacy/private classification")
		}
		if r.legacy != nil {
			before, after := *r.legacy, *again.legacy
			// marshalRecord may materialize an absent, empty legacy agent. Its
			// meaningful fields are checked independently above.
			before.Agent, after.Agent = nil, nil
			if !privacyFuzzEqualJSON(before, after) {
				t.Fatal("roundtrip changed legacy content, timestamps, or template metadata")
			}
		}
		canonical, err := marshalRecord(again)
		if err != nil || !bytes.Equal(encoded, canonical) {
			t.Fatalf("serialization is not stable: %v", err)
		}

		// Mutate accepted records as well as fuzzing raw bytes, so rejection
		// checks are not vacuous when random inputs mostly fail to parse.
		duplicate := append([]byte(`{"date":"2024-02-29",`), encoded[1:]...)
		if _, err := decodeRecord(duplicate, privacyFuzzDate); err == nil {
			t.Fatal("accepted duplicate date field")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		for _, mutation := range []struct{ key, value string }{
			{"version", "99"}, {"version", "null"},
			{"date", `"2024-03-01"`}, {"future_field", `"preserve rather than discard"`},
		} {
			mutatedFields := make(map[string]json.RawMessage, len(fields)+1)
			for key, value := range fields {
				mutatedFields[key] = value
			}
			mutatedFields[mutation.key] = json.RawMessage(mutation.value)
			invalid, err := json.Marshal(mutatedFields)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeRecord(invalid, privacyFuzzDate); err == nil {
				t.Fatalf("accepted forbidden mutation of %s", mutation.key)
			}
		}
	})
}

func FuzzStrictJSONCanonicalRoundtrip(f *testing.F) {
	for _, seed := range []string{
		`{"version":2,"date":"2024-02-29","content":"α\nβ","created":"2024-02-29T01:02:03.123456789+02:00","modified":"2024-02-29T02:03:04Z","applied_templates":["x","x",""]}`,
		`{"version":2,"date":"2024-02-29","content":"\ud83d\ude00 and literal �","created":"2024-02-29T00:00:00+00:00","applied_templates":[]}`,
		`{"version":2,"date":"2024-02-29","content":""}`,
		`{"content":"a","content":"b"}`, `{"Content":"alias"}`,
		`{"content":"a"} {}`, `{"future_field":true}`, `null`,
	} {
		f.Add([]byte(seed))
	}
	f.Add([]byte{'"', 0xff, '"'})
	f.Add([]byte(strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34)))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > privacyFuzzLimit {
			return
		}
		var document privateDocument
		if err := strictJSON(data, &document); err != nil {
			return
		}
		canonical, err := json.Marshal(document)
		if err != nil {
			t.Fatalf("accepted document cannot be serialized: %v", err)
		}
		var again privateDocument
		if decodeErr := strictJSON(canonical, &again); decodeErr != nil {
			t.Fatalf("canonical document rejected: %v", decodeErr)
		}
		if document.Version != again.Version || document.Date != again.Date || document.Content != again.Content || !document.Created.Equal(again.Created) || !document.Modified.Equal(again.Modified) || !slices.Equal(document.AppliedTemplates, again.AppliedTemplates) {
			t.Fatal("canonical roundtrip changed decoded private fields")
		}
		stable, err := json.Marshal(again)
		if err != nil || !bytes.Equal(canonical, stable) {
			t.Fatalf("canonical JSON is not stable: %v", err)
		}
		if err := strictJSON(append(append([]byte(nil), canonical...), []byte(" null")...), &again); err == nil {
			t.Fatal("accepted trailing JSON value")
		}
		for _, prefix := range []string{
			`{"content":"duplicate",`, `{"\u0063ontent":"escaped duplicate",`,
			`{"Content":"case alias",`, `{"future_field":true,`,
		} {
			invalid := append([]byte(prefix), canonical[1:]...)
			if err := strictJSON(invalid, &again); err == nil {
				t.Fatalf("accepted forbidden JSON prefix %s", prefix)
			}
		}
	})
}

func FuzzAppendBullets(f *testing.F) {
	for _, seed := range []struct{ content, section, first, second string }{
		{"", "Updates", "one", "one"},
		{"### Updates", "Updates", "one", "two"},
		{"intro\r\n### Updates\r\n- one\r\n### Other\r\nkeep", "Updates", "one", "two"},
		{"### Updates\n```md\n- one\n### Other\n```\n#### Detail\n- two\n## End\n", "Updates", "one", "two"},
		{"~~~\n### Updates\n~~~\n", "Updates", "one", "two"},
		{"### Updates\n    - one\n", "Updates", "one", "two"},
		{"### Updates\n### Updates\n", "Updates", "one", "two"},
		{"### Updates\n```\nunclosed", "Updates", "one", "two"},
	} {
		f.Add(seed.content, seed.section, seed.first, seed.second)
	}
	f.Fuzz(func(t *testing.T, content, section, first, second string) {
		if len(content) > privacyFuzzLimit || len(section) > privacyFuzzLimit || len(first) > privacyFuzzLimit || len(second) > privacyFuzzLimit || len(content)+len(section)+len(first)+len(second) > privacyFuzzLimit {
			return
		}
		// appendBullets is internal: use the same validated inputs as Append.
		if !utf8.ValidString(content) || validateSection(section) != nil {
			return
		}
		bullets := []string{first, second}
		for i := range bullets {
			var err error
			bullets[i], err = normalizeBullet(bullets[i])
			if err != nil {
				return
			}
		}
		out, added, err := appendBullets(content, section, bullets)
		if err != nil { // Ambiguous headings/unclosed fences are valid rejections.
			return
		}
		if len(added) == 0 && out != content {
			t.Fatal("dedup-only append changed formatting")
		}
		seen := map[string]bool{}
		for _, bullet := range added {
			if seen[bullet] || (bullet != bullets[0] && bullet != bullets[1]) {
				t.Fatal("added bullets are duplicated or were not requested")
			}
			seen[bullet] = true
		}
		// Successful appends are one insertion, never a rewrite of any of
		// the original bytes (including CRLFs, whitespace, and final newline).
		delta := len(out) - len(content)
		if delta < 0 {
			t.Fatal("append removed content")
		}
		prefix := 0
		for prefix < len(content) && out[prefix] == content[prefix] {
			prefix++
		}
		if out[prefix+delta:] != content[prefix:] {
			t.Fatal("append changed original bytes instead of inserting a block")
		}
		repeated, repeatAdded, err := appendBullets(out, section, bullets)
		if err != nil || repeated != out || len(repeatAdded) != 0 {
			t.Fatalf("exact repeat is not a no-op: %v (added %d)", err, len(repeatAdded))
		}
	})
}

func FuzzValidateAgeEnvelope(f *testing.F) {
	f.Add(privacyFuzzCiphertext(f), false)
	// Parser-only scrypt fixture, deliberately unauthenticated. headerProbe
	// inspects structure and work bounds, not MACs, and never performs a KDF.
	scrypt := "age-encryption.org/v1\n-> scrypt " + strings.Repeat("A", 22) + " 1\n" + strings.Repeat("A", 43) + "\n--- " + strings.Repeat("A", 43) + "\n" + strings.Repeat("x", 32)
	f.Add([]byte(scrypt), true)
	f.Add([]byte(strings.Replace(scrypt, " 1\n", " 30\n", 1)), true)
	f.Add([]byte(scrypt[:len(scrypt)-1]), true)
	f.Add([]byte("age-encryption.org/v1\n"+strings.Repeat("x", 4096)), false)
	f.Add([]byte{}, false)
	f.Fuzz(func(t *testing.T, data []byte, scrypt bool) {
		if len(data) > privacyFuzzLimit {
			return
		}
		kind, other := "X25519", "scrypt"
		if scrypt {
			kind, other = other, kind
		}
		if err := validateAgeEnvelope(data, kind); err != nil {
			return
		}
		if !bytes.HasPrefix(data, []byte("age-encryption.org/v1\n")) {
			t.Fatal("accepted unsupported age format")
		}
		if err := validateAgeEnvelope(data, other); err == nil {
			t.Fatal("accepted the same header as both X25519 and scrypt")
		}
		mac := bytes.Index(data, []byte("\n--- "))
		if mac < 0 || mac >= 4096 {
			t.Fatal("accepted missing or oversized header")
		}
		end := bytes.IndexByte(data[mac+1:], '\n')
		if end < 0 {
			t.Fatal("accepted unterminated MAC line")
		}
		headerEnd := mac + 1 + end + 1
		if len(data)-headerEnd < 32 || validateAgeEnvelope(data[:headerEnd+31], kind) == nil {
			t.Fatal("accepted truncated age envelope")
		}
	})
}
