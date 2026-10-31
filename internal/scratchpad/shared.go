package scratchpad

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// SharedPage deliberately has no private content or ciphertext field. Legacy
// indicates that a plaintext private document exists, not permission to read it.
type SharedPage struct {
	Date       string
	Exists     bool
	Content    string
	HasPrivate bool
	Legacy     bool
}

// ReadShared never decrypts or returns legacy private content, even if unlocked.
func (m *Manager) ReadShared(date string) (*SharedPage, error) {
	if err := validateDate(date); err != nil {
		return nil, err
	}
	metadata, _, err := m.readMetadata()
	if err != nil {
		return nil, err
	}
	data, err := readBounded(filepath.Join(m.storageDir, date+".json"), maxDocumentSize)
	if os.IsNotExist(err) {
		return &SharedPage{Date: date}, nil
	}
	if err != nil {
		return nil, err
	}
	r, err := decodeRecord(data, date)
	if err != nil {
		return nil, err
	}
	if r.Private != nil && metadata == nil {
		return nil, errors.New("encrypted scratchpad exists but privacy metadata is missing")
	}
	return &SharedPage{Date: date, Exists: true, Content: r.Agent.Content, HasPrivate: r.Private != nil || r.legacy != nil, Legacy: r.legacy != nil}, nil
}

// HasPrivacyIdentity inspects validated metadata only, including the retained
// identity of a disabled store. It never unlocks or decrypts private notes.
func (m *Manager) HasPrivacyIdentity() (bool, error) {
	metadata, _, err := m.readMetadata()
	return metadata != nil, err
}

// ValidateSharedDate validates a shared-operation date without opening storage.
func ValidateSharedDate(date string) error { return validateDate(date) }

// AppendResult describes only shared input outcomes. Duplicates includes each
// skipped occurrence, including repetitions within the request. Bullets are
// normalized (without the optional "- " prefix), in input order.
type AppendResult struct {
	Date       string   `json:"date"`
	Section    string   `json:"section"`
	Source     string   `json:"source"`
	Added      []string `json:"added_bullets"`
	Duplicates []string `json:"duplicate_bullets"`
}

// ValidateAppend validates all append input without opening storage.
func ValidateAppend(date, section, source string, bullets []string) error {
	_, err := normalizeAppend(date, section, source, bullets)
	return err
}

func normalizeAppend(date, section, source string, bullets []string) ([]string, error) {
	if err := validateDate(date); err != nil {
		return nil, err
	}
	if err := validateSection(section); err != nil {
		return nil, err
	}
	if err := validateLabel(source, 500); err != nil {
		return nil, fmt.Errorf("invalid source: %w", err)
	}
	if len(bullets) == 0 || len(bullets) > 1000 {
		return nil, errors.New("provide 1 to 1000 bullets")
	}
	normalized := make([]string, len(bullets))
	for i, bullet := range bullets {
		var err error
		normalized[i], err = normalizeBullet(bullet)
		if err != nil {
			return nil, err
		}
	}
	return normalized, nil
}

// Append changes only the shared agent area. It serializes the entire read /
// modify / publish operation with private saves and migration. Attribution is
// self-reported and is not cryptographically authenticated.
func (m *Manager) Append(date, section, source string, bullets []string) error {
	_, err := m.AppendDetailed(date, section, source, bullets)
	return err
}

// AppendDetailed preserves Append's locking, conflict and exact dedup semantics.
// A result is returned only on success; errors may include publication durability
// failures and must not be interpreted as confirmation that nothing was written.
func (m *Manager) AppendDetailed(date, section, source string, bullets []string) (*AppendResult, error) {
	normalized, err := normalizeAppend(date, section, source, bullets)
	if err != nil {
		return nil, err
	}
	storeLock, err := lockNote(m.storageDir, "privacy")
	if err != nil {
		return nil, err
	}
	defer storeLock.Close()
	enabled, err := m.PrivacyEnabled()
	if err != nil {
		return nil, err
	}
	lock, err := lockNote(m.storageDir, date)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	filename := filepath.Join(m.storageDir, date+".json")
	data, err := readBounded(filename, maxDocumentSize)
	var r *diskRecord
	switch {
	case os.IsNotExist(err):
		r = &diskRecord{Version: 2, Date: date}
	case err != nil:
		return nil, err
	default:
		r, err = decodeRecord(data, date)
		if err != nil {
			return nil, err
		}
	}
	if enabled && r.legacy != nil {
		return nil, errors.New("legacy plaintext page must be migrated before shared append")
	}
	content, added, err := appendBullets(r.Agent.Content, section, normalized)
	if err != nil {
		return nil, err
	}
	result := &AppendResult{Date: date, Section: section, Source: source, Added: append([]string{}, added...), Duplicates: []string{}}
	// An added value consumes only its first occurrence; all other requested
	// occurrences were skipped by appendBullets, whether preexisting or repeated.
	pending := make(map[string]bool, len(added))
	for _, bullet := range added {
		pending[bullet] = true
	}
	for _, bullet := range normalized {
		if pending[bullet] {
			delete(pending, bullet)
		} else {
			result.Duplicates = append(result.Duplicates, bullet)
		}
	}
	if len(added) == 0 {
		return result, nil
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	r.Agent.Content = content
	r.Agent.Modified = now
	r.Agent.Contributions = append(r.Agent.Contributions, Contribution{ID: id, Date: date, Source: source, Section: section, Bullets: added, Created: now})
	data, err = marshalRecord(r)
	if err != nil {
		return nil, err
	}
	_, err = publish(filename, data, false, syncDirectory)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func validateLabel(s string, limit int) error {
	if s == "" || len(s) > limit || strings.TrimSpace(s) != s || !utf8.ValidString(s) {
		return errors.New("expected nonempty, trimmed single-line text")
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return errors.New("control characters are not allowed")
		}
	}
	return nil
}

func validateSection(section string) error {
	if err := validateLabel(section, 200); err != nil {
		return fmt.Errorf("invalid section: %w", err)
	}
	_, title := heading("### " + section)
	if strings.HasPrefix(section, "#") || strings.ContainsAny(section, "`~") || title != section {
		return errors.New("invalid section heading")
	}
	return nil
}

func normalizeBullet(s string) (string, error) {
	s = strings.TrimPrefix(s, "- ")
	if err := validateLabel(s, 8192); err != nil {
		return "", fmt.Errorf("invalid bullet: %w", err)
	}
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "```") || strings.HasPrefix(s, "~~~") || strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") || strings.HasPrefix(s, "+ ") {
		return "", errors.New("invalid bullet: Markdown block injection")
	}
	return s, nil
}

func validateAgent(agent AgentArea, date string) error {
	if !utf8.ValidString(agent.Content) {
		return errors.New("invalid agent content encoding")
	}
	ids := map[string]bool{}
	for _, c := range agent.Contributions {
		if c.ID == "" || len(c.ID) > 128 || ids[c.ID] || c.Date != date {
			return errors.New("invalid contribution identity/date")
		}
		ids[c.ID] = true
		if err := validateLabel(c.Source, 500); err != nil {
			return err
		}
		if err := validateSection(c.Section); err != nil {
			return err
		}
		if len(c.Bullets) == 0 {
			return errors.New("contribution without bullets")
		}
		for _, bullet := range c.Bullets {
			if _, err := normalizeBullet(bullet); err != nil {
				return err
			}
		}
	}
	return nil
}

// appendBullets recognizes ATX headings and backtick/tilde fences with up to
// three leading spaces. A target ends at the next heading of level <= 3, not
// the next subheading. Deduplication is exact and scoped to that section only.
func appendBullets(content, section string, bullets []string) (updatedContent string, addedBullets []string, appendErr error) {
	lines := strings.SplitAfter(content, "\n")
	start, end := -1, len(content)
	offset := 0
	fenceChar, fenceLength := byte(0), 0
	seen := map[string]bool{}
	for _, raw := range lines {
		line := strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
		text, indented := markdownLine(line)
		if fenceChar != 0 {
			if !indented && isFenceClose(text, fenceChar, fenceLength) {
				fenceChar, fenceLength = 0, 0
			}
			offset += len(raw)
			continue
		}
		if !indented {
			if char, n := fenceStart(text); n >= 3 {
				fenceChar, fenceLength = char, n
				offset += len(raw)
				continue
			}
			level, title := heading(text)
			if level > 0 && level <= 3 {
				if start >= 0 && end == len(content) {
					end = offset
				}
				if level == 3 && title == section {
					if start >= 0 {
						return "", nil, errors.New("ambiguous duplicate shared section heading")
					}
					start = offset + len(raw)
				}
			}
			if start >= 0 && offset >= start && offset < end && strings.HasPrefix(text, "- ") {
				seen[text[2:]] = true
			}
		}
		offset += len(raw)
	}
	if fenceChar != 0 {
		return "", nil, errors.New("shared content has an unclosed code fence")
	}
	var added []string
	for _, bullet := range bullets {
		if !seen[bullet] {
			added = append(added, bullet)
			seen[bullet] = true
		}
	}
	if len(added) == 0 {
		return content, nil, nil
	}
	var block strings.Builder
	if start < 0 {
		if content != "" && !strings.HasSuffix(content, "\n") {
			block.WriteByte('\n')
		}
		if content != "" && !strings.HasSuffix(content, "\n\n") {
			block.WriteByte('\n')
		}
		block.WriteString("### " + section + "\n\n")
	} else if end > 0 && content[end-1] != '\n' {
		block.WriteByte('\n')
	}
	for _, bullet := range added {
		block.WriteString("- " + bullet + "\n")
	}
	if end < len(content) {
		block.WriteByte('\n')
	}
	return content[:end] + block.String() + content[end:], added, nil
}

func markdownLine(line string) (string, bool) {
	n := len(line) - len(strings.TrimLeft(line, " "))
	if n > 3 || strings.HasPrefix(line, "\t") {
		return line, true
	}
	return line[n:], false
}

func heading(text string) (level int, title string) {
	n := len(text) - len(strings.TrimLeft(text, "#"))
	if n == 0 || n > 6 || (len(text) > n && text[n] != ' ' && text[n] != '\t') {
		return 0, ""
	}
	title = strings.TrimSpace(text[n:])
	// Optional ATX closing hashes only count when preceded by whitespace.
	trimmed := strings.TrimRight(title, "#")
	if len(trimmed) < len(title) && strings.HasSuffix(trimmed, " ") {
		title = strings.TrimSpace(trimmed)
	}
	return n, title
}

func fenceStart(text string) (fenceChar byte, fenceLength int) {
	if len(text) < 3 || (text[0] != '`' && text[0] != '~') {
		return 0, 0
	}
	char := text[0]
	n := 0
	for n < len(text) && text[n] == char {
		n++
	}
	if char == '`' && strings.ContainsRune(text[n:], '`') {
		return 0, 0
	}
	return char, n
}

func isFenceClose(text string, char byte, length int) bool {
	n := 0
	for n < len(text) && text[n] == char {
		n++
	}
	return n >= length && strings.TrimSpace(text[n:]) == ""
}
