package errnotice

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/infodancer/web/obs"
)

// Storage caps. A failure row should be cheap to store and read back into an
// admin view; a multi-megabyte panic dump or stack helps no one in a table.
// These bound a single row's worst case.
const (
	maxPanicLen = 1 << 10 // 1 KiB
	maxStackLen = 8 << 10 // 8 KiB
)

// uuidRe matches a canonical UUID (8-4-4-4-12 hex with dashes).
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// hexRe matches a run of hex digits (used with a length test for long hashes).
var hexRe = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// digitsRe matches an all-digit segment (a numeric id).
var digitsRe = regexp.MustCompile(`^[0-9]+$`)

// fingerprint produces the stable grouping key for a notice. It is a sha256 hex
// digest over (component, method, normalized path, status, panic class), so
// repeats of the same logical failure collapse onto one error_notices row while
// genuinely different failures stay apart.
//
// This is a grouping heuristic, not a security boundary: an attacker who can
// drive arbitrary requests can already influence which rows exist (that is the
// point -- we record real failures). The fingerprint only decides which
// failures are treated as "the same"; a worst-case collision merges two rows or
// splits one, never grants access or corrupts data. The store still uses
// parameterized queries, so the (untrusted) inputs never reach SQL as text.
func fingerprint(n obs.Notice) string {
	// Join with a unit separator the inputs cannot contain so distinct field
	// tuples cannot alias by concatenation (e.g. ("ab","c") vs ("a","bc")).
	parts := []string{
		n.Component,
		n.Method,
		normalizePath(n.Path),
		fmt.Sprintf("%d", n.Status),
		panicClass(n),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// normalizePath rewrites path segments that look like volatile ids to ":id" so
// /posts/123 and /posts/456 -- or /users/<uuid> across requests -- group into
// one row instead of one row per id. A segment is treated as an id when it is
// all digits, a long hex string (>= 16 hex chars, catching sha/hex tokens), or
// a canonical UUID. This is deliberately conservative: a short word like "v2"
// is not an id and stays put.
func normalizePath(path string) string {
	if path == "" {
		return ""
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if isVolatileID(s) {
			segs[i] = ":id"
		}
	}
	return strings.Join(segs, "/")
}

func isVolatileID(s string) bool {
	switch {
	case s == "":
		return false
	case digitsRe.MatchString(s):
		return true
	case uuidRe.MatchString(s):
		return true
	case len(s) >= 16 && hexRe.MatchString(s):
		return true
	default:
		return false
	}
}

// panicClass is a stable discriminator that keeps different panics from merging
// while still grouping repeats of the same panic. For a recovered panic it is
// the Go type of the panic value plus the top user stack frame; for a plain 5xx
// (no panic) it is empty, so all plain 5xx for the same route+status group
// together.
func panicClass(n obs.Notice) string {
	if n.Panic == nil {
		return ""
	}
	return fmt.Sprintf("%T", n.Panic) + "|" + topFrame(n.Stack)
}

// topFrame extracts the first "file:line" reference from a runtime stack dump,
// which identifies where the panic was raised. It is best-effort: an
// unrecognized stack yields "", which still leaves the panic type as a
// discriminator.
func topFrame(stack []byte) string {
	for line := range strings.Lines(string(stack)) {
		line = strings.TrimSpace(line)
		// Runtime stack file lines look like:
		//   /path/to/file.go:42 +0x1d
		if i := strings.Index(line, ".go:"); i >= 0 {
			frame := line[:i+len(".go:")]
			rest := line[i+len(".go:"):]
			// Take the line number, drop the trailing " +0x.." offset.
			if sp := strings.IndexByte(rest, ' '); sp >= 0 {
				rest = rest[:sp]
			}
			return frame + rest
		}
	}
	return ""
}

// truncate caps s to at most limit bytes, appending a marker when it cut. The
// cut is on a rune boundary so the stored text stays valid UTF-8.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	const marker = "...[truncated]"
	if limit <= len(marker) {
		return s[:limit]
	}
	cut := limit - len(marker)
	// Back up to a rune boundary so we never store a split multibyte rune.
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + marker
}

// utf8RuneStart reports whether b is a leading byte of a UTF-8 rune (i.e. not a
// 10xxxxxx continuation byte).
func utf8RuneStart(b byte) bool {
	return b&0xC0 != 0x80
}
