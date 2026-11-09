package sharedmcp

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/pders01/sp/internal/scratchpad"
)

// Deliberately narrow: neither adapter nor a backend implementation needs a
// private load, identity, unlock, recovery, editor, or template interface.
type sharedStore interface {
	ReadShared(string) (*scratchpad.SharedPage, error)
	AppendDetailed(string, string, string, []string) (*scratchpad.AppendResult, error)
}

type toolCall struct {
	append                bool
	date, section, source string
	bullets               []string
}

type readResult struct {
	Date         string `json:"date"`
	Status       string `json:"status"`
	Content      string `json:"content"`
	NotEncrypted bool   `json:"not_encrypted"`
	Provenance   string `json:"provenance"`
}

type appendResult struct {
	Date         string   `json:"date"`
	Section      string   `json:"section"`
	Source       string   `json:"source"`
	Added        []string `json:"added_bullets"`
	Duplicates   []string `json:"duplicate_bullets"`
	NotEncrypted bool     `json:"not_encrypted"`
	Provenance   string   `json:"provenance"`
}

const provenance = "self_reported_not_authenticated"

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content    []contentBlock `json:"content"`
	Structured any            `json:"structuredContent,omitempty"`
	IsError    bool           `json:"isError"`
}

func success(v any) toolResult {
	b, _ := json.Marshal(v)
	return toolResult{Content: []contentBlock{{Type: "text", Text: string(b)}}, Structured: v}
}

func failure(code, outcome string) toolResult {
	messages := map[string]string{
		"INVALID_INPUT":      "Invalid tool input.",
		"DATE_NOT_ALLOWED":   "Date is not allowed for this operation.",
		"BUSY":               "Another operation is active; no call was queued.",
		"OUTPUT_TOO_LARGE":   "Result exceeds the output limit.",
		"STORE_UNAVAILABLE":  "Shared store is unavailable.",
		"APPEND_UNCONFIRMED": "Append outcome is unconfirmed; review before retrying identical input.",
	}
	b, _ := json.Marshal(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Outcome string `json:"write_outcome"`
	}{code, messages[code], outcome})
	return toolResult{Content: []contentBlock{{Type: "text", Text: string(b)}}, IsError: true}
}

func prepare(params any, cfg config, id any) (toolCall, *toolResult) {
	bad := failure("INVALID_INPUT", "not_attempted")
	m, ok := object(params, []string{"name", "arguments"})
	if !ok {
		return toolCall{}, &bad
	}
	name, ok := text(m["name"], 32)
	if !ok || (name != "sp_shared_read" && name != "sp_shared_append") {
		return toolCall{}, &bad
	}
	call := toolCall{append: name == "sp_shared_append"}
	required := []string{"date"}
	if call.append {
		required = append(required, "section", "source", "bullets")
	}
	a, ok := object(m["arguments"], required)
	if !ok {
		return toolCall{}, &bad
	}
	call.date, ok = text(a["date"], 10)
	if !ok || scratchpad.ValidateSharedDate(call.date) != nil {
		return toolCall{}, &bad
	}
	if call.append {
		call.section, ok = text(a["section"], 200)
		if !ok {
			return toolCall{}, &bad
		}
		call.source, ok = text(a["source"], 500)
		if !ok {
			return toolCall{}, &bad
		}
		items, ok := a["bullets"].([]any)
		if !ok || len(items) == 0 || len(items) > 100 {
			return toolCall{}, &bad
		}
		n := len(call.date) + len(call.section) + len(call.source)
		for _, item := range items {
			bullet, ok := text(item, 8194)
			if !ok {
				return toolCall{}, &bad
			}
			n += len(bullet)
			call.bullets = append(call.bullets, bullet)
		}
		if n > maxAppend || scratchpad.ValidateAppend(call.date, call.section, call.source, call.bullets) != nil {
			return toolCall{}, &bad
		}
	}
	grants := cfg.read
	if call.append {
		grants = cfg.append
	}
	if !grants[call.date] {
		denied := failure("DATE_NOT_ALLOWED", "not_attempted")
		return toolCall{}, &denied
	}
	if call.append {
		// Upper bound: each normalized occurrence is in one array; putting ALL
		// occurrences into BOTH arrays is conservative even for mixed dedup.
		// Include the actual bounded ID and both JSON representations pre-write.
		worst := appendResult{call.date, call.section, call.source, call.bullets, call.bullets, true, provenance}
		if _, ok := encodeResponse(id, success(worst), nil); !ok {
			tooLarge := failure("OUTPUT_TOO_LARGE", "not_attempted")
			return toolCall{}, &tooLarge
		}
	}
	return call, nil
}

func execute(store sharedStore, call toolCall) toolResult {
	if call.append {
		r, err := store.AppendDetailed(call.date, call.section, call.source, call.bullets)
		if err != nil || r == nil {
			return failure("APPEND_UNCONFIRMED", "unknown")
		}
		return success(appendResult{call.date, call.section, call.source, r.Added, r.Duplicates, true, provenance})
	}
	r, err := store.ReadShared(call.date)
	if err != nil || r == nil {
		return failure("STORE_UNAVAILABLE", "not_applicable")
	}
	if len(r.Content) > maxRead {
		return failure("OUTPUT_TOO_LARGE", "not_applicable")
	}
	if !utf8.ValidString(r.Content) {
		return failure("STORE_UNAVAILABLE", "not_applicable")
	}
	status := "ok"
	content := r.Content
	if !r.Exists {
		status, content = "missing", ""
	} else if strings.TrimSpace(content) == "" {
		status = "no_shared"
	}
	return success(readResult{call.date, status, content, true, provenance})
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func protocolError(code int) *rpcError {
	message := "Invalid request"
	switch code {
	case -32700:
		message = "Parse error"
	case -32601:
		message = "Method not found"
	case -32602:
		message = "Invalid params"
	}
	return &rpcError{code, message}
}

func encodeResponse(id, result any, rpcErr *rpcError) ([]byte, bool) {
	var v any
	if rpcErr != nil {
		v = struct {
			Version string    `json:"jsonrpc"`
			ID      any       `json:"id"`
			Error   *rpcError `json:"error"`
		}{"2.0", id, rpcErr}
	} else {
		v = struct {
			Version string `json:"jsonrpc"`
			ID      any    `json:"id"`
			Result  any    `json:"result"`
		}{"2.0", id, result}
	}
	b, err := json.Marshal(v)
	if err != nil || len(b)+1 > maxOutput {
		return nil, false
	}
	return append(b, '\n'), true
}
