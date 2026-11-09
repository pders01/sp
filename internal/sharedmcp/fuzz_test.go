package sharedmcp

import (
	"encoding/json"
	"testing"
)

// Fuzzing is parser-only: no store, filesystem, cryptography, process or tool
// dispatch. The production frame cap also bounds each fuzz callback's work.
func FuzzRequestDecoder(f *testing.F) {
	for _, seed := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"synthetic α�","method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":"\ud800","method":"ping"}`,
		`{"jsonrpc":"2.0","id":null,"method":"ping"}`,
		`null`, `[]`, `{}`, `{"jsonrpc":"2.0"} trailing`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, frame []byte) {
		if len(frame) > maxFrame {
			return
		}
		request, code := decodeRequest(frame)
		if code != 0 {
			return
		}
		if !json.Valid(frame) || request.method == "" || (request.id != nil && !validID(request.id)) {
			t.Fatal("accepted invalid JSON-RPC request framing or identity")
		}
		// Reflected request identities must still fit the bounded, escaped
		// response representation; note data is never involved in this check.
		if _, ok := encodeResponse(request.id, map[string]any{}, nil); !ok {
			t.Fatal("accepted identity cannot fit a bounded protocol response")
		}
	})
}
