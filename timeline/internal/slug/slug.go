// Package slug derives URL-safe slugs from human-readable titles and names.
package slug

import "strings"

// Make returns a URL-safe slug for s: lowercased, with runs of spaces,
// dashes, and underscores collapsed to a single dash, all other characters
// dropped, and leading/trailing dashes trimmed.
func Make(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '-' || r == '_':
			return '-'
		default:
			return -1
		}
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}
