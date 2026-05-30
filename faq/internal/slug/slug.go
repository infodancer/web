// Package slug derives URL slugs from question titles.
//
// Slugs are decorative: lookups happen by question id (e.g. /q/12345
// /how-does-counterspell-work). A canonical-slug redirect is the only
// consumer of the slug for routing — if the URL slug doesn't match the
// canonical form, the handler 301s to the right URL. There is no
// reserved-words list or DB-uniqueness gate because the slug is never
// the primary key.
package slug

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// MaxLen is the maximum length of the slug returned by [Normalize]. Slugs
// are truncated at the last hyphen before MaxLen so words aren't sliced
// mid-token.
const MaxLen = 64

// Normalize converts a question title into a URL slug:
//
//   - Unicode NFKD decomposition + drop combining marks ("Café" → "Cafe",
//     "ﬁ" → "fi", full-width digits → ASCII).
//   - Lowercase.
//   - Any rune not in [a-z0-9] becomes a hyphen.
//   - Runs of hyphens collapse to one.
//   - Leading and trailing hyphens are trimmed.
//   - Result is truncated to at most [MaxLen] runes, at the last hyphen
//     boundary so we don't slice through a word.
//
// Returns "" for input that contains no [a-z0-9] runes after normalization.
// Callers should fall back to a short generated slug ("q-{id}") in that
// case so the URL still has a slug segment.
func Normalize(s string) string {
	chain := transform.Chain(
		norm.NFKD,
		runes.Remove(runes.In(unicode.Mn)),
		norm.NFC,
	)
	t, _, err := transform.String(chain, s)
	if err != nil {
		t = s
	}
	t = strings.ToLower(t)

	var b strings.Builder
	b.Grow(len(t))
	prevHyphen := false
	for _, r := range t {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) <= MaxLen {
		return out
	}
	// Truncate to MaxLen, then back off to the last hyphen so we don't
	// chop a word mid-token. If there's no hyphen in [0:MaxLen], a hard
	// cut is fine — the title was a single very long token.
	cut := out[:MaxLen]
	if i := strings.LastIndexByte(cut, '-'); i > 0 {
		cut = cut[:i]
	}
	return cut
}
