package sharedmcp

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

// Parse into maps, not structs: encoding/json's case-insensitive field matching
// and lossy surrogate repair are inappropriate at an authority boundary.
func strictValue(data []byte) (any, error) {
	if !utf8.Valid(data) || !pairedSurrogates(data) {
		return nil, errInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := jsonValue(d, 0)
	if err != nil {
		return nil, errInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errInvalid
	}
	return v, nil
}

func jsonValue(d *json.Decoder, depth int) (any, error) {
	if depth > 16 {
		return nil, errInvalid
	}
	t, err := d.Token()
	if err != nil || t == nil {
		return nil, errInvalid
	}
	switch t {
	case json.Delim('{'):
		m := map[string]any{}
		for d.More() {
			keyToken, err := d.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return nil, errInvalid
			}
			if _, exists := m[key]; exists {
				return nil, errInvalid
			}
			v, err := jsonValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			m[key] = v
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, errInvalid
		}
		return m, nil
	case json.Delim('['):
		a := []any{}
		for d.More() {
			v, err := jsonValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return nil, errInvalid
		}
		return a, nil
	default:
		switch t.(type) {
		case string, bool, json.Number:
			return t, nil
		default:
			return nil, errInvalid
		}
	}
}

func pairedSurrogates(data []byte) bool {
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
			return false
		}
		if data[i+1] != 'u' {
			i++
			continue
		}
		if i+6 > len(data) {
			return false
		}
		v, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 16)
		if err != nil {
			return false
		}
		switch {
		case v >= 0xd800 && v <= 0xdbff:
			if i+12 > len(data) || data[i+6] != '\\' || data[i+7] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(data[i+8:i+12]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 11
		case v >= 0xdc00 && v <= 0xdfff:
			return false
		default:
			i += 5
		}
	}
	return true
}

func object(v any, required []string, optional ...string) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	allowed := map[string]bool{}
	for _, key := range required {
		allowed[key] = true
		if _, exists := m[key]; !exists {
			return nil, false
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key := range m {
		if !allowed[key] {
			return nil, false
		}
	}
	return m, true
}

func text(v any, limit int) (string, bool) {
	s, ok := v.(string)
	return s, ok && len(s) <= limit
}

func validID(v any) bool {
	if _, ok := v.(string); ok {
		_, ok = text(v, 128)
		return ok
	}
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	i, err := strconv.ParseInt(string(n), 10, 64)
	return err == nil && i >= -9007199254740991 && i <= 9007199254740991
}

func sameID(a, b any) bool {
	if x, ok := a.(json.Number); ok {
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		// Both values have already passed validID. In particular, -0 and 0
		// are the same integer request ID, not distinct active operations.
		xi, _ := x.Int64()
		yi, _ := y.Int64()
		return xi == yi
	}
	return a == b
}

type request struct {
	id     any
	method string
	params any
}

func decodeRequest(data []byte) (req request, code int) {
	v, err := strictValue(data)
	if err != nil {
		return request{}, -32700
	}
	m, ok := object(v, []string{"jsonrpc", "method"}, "id", "params")
	if !ok || m["jsonrpc"] != "2.0" {
		return request{}, -32600
	}
	method, ok := text(m["method"], 64)
	if !ok || method == "" {
		return request{}, -32600
	}
	id, hasID := m["id"]
	if hasID && !validID(id) {
		return request{}, -32600
	}
	params := m["params"]
	if params == nil {
		params = map[string]any{}
	}
	if _, ok := params.(map[string]any); !ok {
		return request{}, -32600
	}
	return request{id: id, method: method, params: params}, 0
}
