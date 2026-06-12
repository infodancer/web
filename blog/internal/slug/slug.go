// Package slug derives URL slugs from post titles, named-blog names, tag
// names, and author display names. The algorithm matches faq's slug
// package so URLs feel consistent across the suite.
//
// Unlike faq's decorative slugs, blog slugs are lookup keys (posts and
// blogs route by slug), so callers enforce uniqueness at the store layer
// and disambiguate with numeric suffixes where needed.
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

// Normalize converts a title into a URL slug:
//
//   - Unicode NFKD decomposition + drop combining marks ("Café" → "cafe",
//     "ﬁ" → "fi", full-width digits → ASCII).
//   - Lowercase.
//   - Any rune not in [a-z0-9] becomes a hyphen.
//   - Runs of hyphens collapse to one.
//   - Leading and trailing hyphens are trimmed.
//   - Result is truncated to at most [MaxLen] runes, at the last hyphen
//     boundary so we don't slice through a word.
//
// Returns "" for input that contains no [a-z0-9] runes after
// normalization. Callers fall back to a generated slug ("p-{id}") so the
// URL still has a slug segment.
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
	cut := out[:MaxLen]
	if i := strings.LastIndexByte(cut, '-'); i > 0 {
		cut = cut[:i]
	}
	return cut
}
