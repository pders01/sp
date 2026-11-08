package embeddedvim

import (
	"context"
	"encoding/binary"
)

// Return only fixed allowlisted exception classes, never messages or stacks.
func (s *Session) exceptionClass(ctx context.Context) string {
	ex, err := s.call(ctx, "JS_GetException", s.jsctx)
	if err != nil {
		return "unknown"
	}
	defer s.call(ctx, "QJS_FreeValue", s.jsctx, ex)
	key, err := s.cstring(ctx, "name")
	if err != nil {
		return "unknown"
	}
	defer s.call(ctx, "free", key)
	name, err := s.call(ctx, "JS_GetPropertyStr", s.jsctx, ex, key)
	if err != nil {
		return "unknown"
	}
	defer s.call(ctx, "QJS_FreeValue", s.jsctx, name)
	ptr, err := s.call(ctx, "QJS_ToCString", s.jsctx, name)
	if err != nil || ptr == 0 {
		return "unknown"
	}
	defer s.call(ctx, "free", ptr)
	data, ok := s.module.Memory().Read(uint32(ptr), 8)
	if !ok {
		return "unknown"
	}
	pair := binary.LittleEndian.Uint64(data)
	str, size := uint32(pair>>32), uint32(pair)
	defer s.call(ctx, "JS_FreeCString", s.jsctx, uint64(str))
	if size > 32 {
		return "unknown"
	}
	data, ok = s.module.Memory().Read(str, size)
	if !ok {
		return "unknown"
	}
	switch string(data) {
	case "Error", "SyntaxError", "TypeError", "ReferenceError", "InternalError", "RangeError", "EvalError", "URIError":
		return string(data)
	}
	return "unknown"
}
