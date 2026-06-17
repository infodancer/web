package errnotice

import (
	"fmt"
	"math"
)

// clampLimit narrows a caller-supplied List limit to the int32 the query
// binds, with a sane floor so a zero or negative limit returns nothing rather
// than the whole table.
func clampLimit(limit int) int32 {
	switch {
	case limit < 0:
		return 0
	case limit > math.MaxInt32:
		return math.MaxInt32
	default:
		return int32(limit)
	}
}

// clampStatus narrows an HTTP status code to the int32 the schema stores. A
// real status is a small positive number; clamping bounds a nonsense value
// instead of risking an overflow conversion (and keeps gosec quiet without a
// suppression).
func clampStatus(status int) int32 {
	switch {
	case status < 0:
		return 0
	case status > math.MaxInt32:
		return math.MaxInt32
	default:
		return int32(status)
	}
}

// panicValueString renders a recovered panic value to text for storage. It uses
// %v so an error or Stringer panic reads naturally and a plain string is itself;
// the panic's Go type is folded into the fingerprint separately (see
// panicClass). Only error and string carry a useful message verbatim; anything
// else still formats to something legible.
//
// Note: a panic value can carry whatever the panicking code put in it. The
// rendered text is truncated by the caller and stored as a parameterized value
// (never interpolated into SQL); operators should still treat stored panic text
// as untrusted when displaying it.
func panicValueString(v any) string {
	switch p := v.(type) {
	case error:
		return p.Error()
	case string:
		return p
	default:
		return fmt.Sprintf("%v", v)
	}
}
