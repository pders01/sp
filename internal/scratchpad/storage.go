package scratchpad

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unicode/utf8"
)

const (
	maxDocumentSize = 16 << 20
	maxPrivateSize  = 8 << 20
	privateFormat   = "age-x25519"
)

// AgentArea is explicitly shared, unencrypted, and self-reported, not trusted
// attribution. Modified is independent of the private note's Modified time.
type AgentArea struct {
	Content       string         `json:"content"`
	Contributions []Contribution `json:"contributions"`
	Modified      time.Time      `json:"modified"`
}

// Contribution records only bullets newly added by one Append operation.
type Contribution struct {
	ID      string    `json:"id"`
	Date    string    `json:"date"`
	Source  string    `json:"source"`
	Section string    `json:"section"`
	Bullets []string  `json:"bullets"`
	Created time.Time `json:"created"`
}

type privateEnvelope struct {
	Format  string `json:"format"`
	Payload string `json:"payload"`
}

type diskRecord struct {
	Version int              `json:"version"`
	Date    string           `json:"date"`
	Private *privateEnvelope `json:"private,omitempty"`
	Agent   AgentArea        `json:"agent"`
	legacy  *legacyDocument
}

type legacyDocument struct {
	Version          int        `json:"version,omitempty"`
	Date             string     `json:"date"`
	Content          string     `json:"content"`
	AppliedTemplates []string   `json:"applied_templates,omitempty"`
	Created          time.Time  `json:"created"`
	Modified         time.Time  `json:"modified"`
	Agent            *AgentArea `json:"agent,omitempty"`
}

type privateDocument struct {
	Version          int       `json:"version"`
	Date             string    `json:"date"`
	Content          string    `json:"content"`
	Created          time.Time `json:"created"`
	Modified         time.Time `json:"modified"`
	AppliedTemplates []string  `json:"applied_templates,omitempty"`
}

// strictJSON rejects unknown and duplicate fields, trailing data, and excessive
// nesting rather than silently discarding future data during read/modify/write.
func strictJSON(data []byte, dst any) error {
	if !utf8.Valid(data) {
		return errors.New("JSON contains invalid UTF-8")
	}
	if err := validateJSONSurrogates(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := checkJSONValue(dec, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	dec = json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD without error.
// Reject those escapes before decoding so migration cannot silently lose text.
func validateJSONSurrogates(data []byte) error {
	inString := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || data[i] != '\\' {
			continue
		}
		if i+1 >= len(data) {
			return errors.New("truncated JSON escape")
		}
		if data[i+1] != 'u' {
			i++ // Includes escaped quotes and literal backslashes.
			continue
		}
		if i+6 > len(data) {
			return errors.New("truncated JSON Unicode escape")
		}
		value, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 16)
		if err != nil {
			return errors.New("invalid JSON Unicode escape")
		}
		switch {
		case value >= 0xd800 && value <= 0xdbff:
			if i+12 > len(data) || data[i+6] != '\\' || data[i+7] != 'u' {
				return errors.New("unpaired JSON high surrogate")
			}
			low, err := strconv.ParseUint(string(data[i+8:i+12]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return errors.New("invalid JSON surrogate pair")
			}
			i += 11
		case value >= 0xdc00 && value <= 0xdfff:
			return errors.New("unpaired JSON low surrogate")
		default:
			i += 5
		}
	}
	return nil
}

func checkJSONValue(dec *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("JSON nesting too deep")
	}
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, tokenErr := dec.Token()
			if tokenErr != nil {
				return tokenErr
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("invalid or duplicate JSON field")
			}
			// encoding/json matches struct fields case-insensitively, while
			// format dispatch uses exact names. Reject aliases (including
			// Unicode case-fold aliases) before either decoder can disagree.
			if name == "" {
				return errors.New("empty JSON field name")
			}
			for _, c := range name {
				if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
					return errors.New("JSON field names must use canonical lowercase ASCII spelling")
				}
			}
			seen[name] = true
			if valueErr := checkJSONValue(dec, depth+1); valueErr != nil {
				return valueErr
			}
		}
	case '[':
		for dec.More() {
			if valueErr := checkJSONValue(dec, depth+1); valueErr != nil {
				return valueErr
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	_, err = dec.Token()
	return err
}

func decodeRecord(data []byte, date string) (*diskRecord, error) {
	if len(data) > maxDocumentSize {
		return nil, errors.New("scratchpad too large")
	}
	var fields map[string]json.RawMessage
	if err := strictJSON(data, &fields); err != nil {
		return nil, fmt.Errorf("invalid scratchpad: %w", err)
	}
	if fields == nil {
		return nil, errors.New("scratchpad must be an object")
	}
	version := 0
	if raw, ok := fields["version"]; ok {
		if string(raw) == "null" {
			return nil, errors.New("invalid scratchpad version")
		}
		if err := json.Unmarshal(raw, &version); err != nil {
			return nil, err
		}
	}
	r := &diskRecord{Version: version}
	switch version {
	case 0, 1:
		var legacy legacyDocument
		if err := strictJSON(data, &legacy); err != nil {
			return nil, err
		}
		r.Date, r.legacy = legacy.Date, &legacy
		if legacy.Agent != nil {
			r.Agent = *legacy.Agent
		}
	case 2:
		if err := strictJSON(data, r); err != nil {
			return nil, err
		}
		if raw, ok := fields["agent"]; !ok || string(raw) == "null" {
			return nil, errors.New("missing agent area")
		}
		var agentFields map[string]json.RawMessage
		if err := json.Unmarshal(fields["agent"], &agentFields); err != nil {
			return nil, err
		}
		if raw, ok := agentFields["content"]; !ok || string(raw) == "null" {
			return nil, errors.New("missing shared content field")
		}
		if raw, ok := fields["private"]; ok && string(raw) == "null" {
			return nil, errors.New("invalid private envelope")
		}
		if r.Private != nil {
			if r.Private.Format != privateFormat {
				return nil, errors.New("unsupported private format")
			}
			payload, err := decodePayload(r.Private.Payload, maxDocumentSize)
			if err != nil {
				return nil, err
			}
			if err := validateAgeEnvelope(payload, "X25519"); err != nil {
				return nil, fmt.Errorf("invalid private envelope: %w", err)
			}
		}
	default:
		return nil, fmt.Errorf("unsupported scratchpad version %d", version)
	}
	if r.Date != date {
		return nil, fmt.Errorf("scratchpad date %q does not match requested date %q", r.Date, date)
	}
	if err := validateAgent(r.Agent, date); err != nil {
		return nil, err
	}
	return r, nil
}

func decodePayload(s string, limit int) ([]byte, error) {
	if s == "" || len(s) > base64.StdEncoding.EncodedLen(limit) {
		return nil, errors.New("invalid encrypted payload size")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(s)
	if err != nil || len(data) > limit {
		return nil, errors.New("invalid encrypted payload encoding")
	}
	return data, nil
}

func (m *Manager) openRecord(r *diskRecord) (*Scratchpad, error) {
	s := &Scratchpad{Date: r.Date, Agent: r.Agent}
	if r.legacy != nil {
		s.Content, s.Created, s.Modified = r.legacy.Content, r.legacy.Created, r.legacy.Modified
		s.AppliedTemplates = r.legacy.AppliedTemplates
		return s, nil
	}
	if r.Private == nil {
		return s, nil
	}
	identity, err := m.unlockedIdentity()
	if err != nil {
		return nil, err
	}
	payload, err := decodePayload(r.Private.Payload, maxDocumentSize)
	if err != nil {
		return nil, err
	}
	plain, err := decryptAge(payload, identity, maxPrivateSize)
	if err != nil {
		return nil, fmt.Errorf("decrypt private scratchpad: %w", err)
	}
	inner, err := decodePrivateDocument(plain, r.Date)
	if err != nil {
		return nil, err
	}
	s.Content, s.Created, s.Modified, s.AppliedTemplates = inner.Content, inner.Created, inner.Modified, inner.AppliedTemplates
	return s, nil
}

func decodePrivateDocument(plain []byte, date string) (*privateDocument, error) {
	var inner privateDocument
	if err := strictJSON(plain, &inner); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(plain, &fields); err != nil {
		return nil, err
	}
	for _, required := range []string{"version", "date", "content", "created", "modified"} {
		if raw, ok := fields[required]; !ok || string(raw) == "null" {
			return nil, fmt.Errorf("missing private field %s", required)
		}
	}
	if inner.Version != 2 || inner.Date != date {
		return nil, errors.New("private document version/date mismatch")
	}
	return &inner, nil
}

func (m *Manager) encodeScratchpad(s *Scratchpad, encrypted bool) ([]byte, *diskRecord, error) {
	if !utf8.ValidString(s.Content) {
		return nil, nil, errors.New("invalid private content encoding")
	}
	for _, id := range s.AppliedTemplates {
		if !utf8.ValidString(id) {
			return nil, nil, errors.New("invalid template metadata encoding")
		}
	}
	if err := validateAgent(s.Agent, s.Date); err != nil {
		return nil, nil, err
	}
	if !encrypted {
		if s.disk != nil && s.disk.Version == 2 && s.disk.Private != nil {
			return nil, nil, errors.New("cannot downgrade encrypted scratchpad")
		}
		legacy := &legacyDocument{Date: s.Date, Content: s.Content, Created: s.Created, Modified: s.Modified, AppliedTemplates: s.AppliedTemplates}
		if s.disk != nil && s.disk.legacy != nil {
			legacy.Version = s.disk.legacy.Version
		}
		if s.Agent.Content != "" || s.Agent.Contributions != nil || !s.Agent.Modified.IsZero() {
			legacy.Agent = &s.Agent
		}
		data, err := json.MarshalIndent(legacy, "", "  ")
		if len(data) > maxDocumentSize {
			return nil, nil, errors.New("scratchpad too large")
		}
		return data, &diskRecord{Date: s.Date, legacy: legacy, Agent: s.Agent}, err
	}
	identity, err := m.unlockedIdentity()
	if err != nil {
		return nil, nil, err
	}
	inner := privateDocument{Version: 2, Date: s.Date, Content: s.Content, Created: s.Created, Modified: s.Modified, AppliedTemplates: s.AppliedTemplates}
	plain, err := json.Marshal(inner)
	if err != nil {
		return nil, nil, err
	}
	if len(plain) > maxPrivateSize {
		return nil, nil, errors.New("private scratchpad too large")
	}
	payload, err := encryptAge(plain, identity.Recipient())
	if err != nil {
		return nil, nil, err
	}
	// Before publishing a migration (or save), authenticate the complete stream
	// and verify the exact serialized inner document, including all metadata.
	verified, err := decryptAge(payload, identity, maxPrivateSize)
	if err != nil {
		return nil, nil, err
	}
	if !bytes.Equal(plain, verified) {
		return nil, nil, errors.New("encrypted scratchpad verification failed")
	}
	r := &diskRecord{Version: 2, Date: s.Date, Private: &privateEnvelope{Format: privateFormat, Payload: base64.StdEncoding.EncodeToString(payload)}, Agent: s.Agent}
	data, err := marshalRecord(r)
	return data, r, err
}

func marshalRecord(r *diskRecord) ([]byte, error) {
	var data []byte
	var err error
	if r.legacy != nil {
		legacyCopy := *r.legacy
		legacyCopy.Agent = &r.Agent
		data, err = json.MarshalIndent(legacyCopy, "", "  ")
	} else {
		data, err = json.MarshalIndent(r, "", "  ")
	}
	if len(data) > maxDocumentSize {
		return nil, errors.New("scratchpad too large")
	}
	return data, err
}

func readBounded(path string, limit int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("SP data must be a regular file, not a symlink or special file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("file exceeds size limit")
	}
	return data, nil
}

// publish never creates a plaintext staging file for encrypted writes. Exclusive
// publication uses a hard link, so existing metadata can never be overwritten.
func publish(filename string, data []byte, exclusive bool, syncDir func(string) error) (bool, error) {
	dir := filepath.Dir(filename)
	tmp, err := os.CreateTemp(dir, ".sp-*.tmp")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, writeErr := tmp.Write(data); writeErr != nil {
		return false, writeErr
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		return false, syncErr
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return false, closeErr
	}
	if exclusive {
		err = os.Link(tmp.Name(), filename)
	} else {
		err = os.Rename(tmp.Name(), filename)
	}
	if err != nil {
		return false, err
	}
	if err := syncDir(dir); err != nil {
		return true, fmt.Errorf("%w: sync storage directory: %w", ErrDurabilityUnconfirmed, err)
	}
	return true, nil
}
