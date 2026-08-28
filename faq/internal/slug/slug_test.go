package slug

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"basic", "How does counterspell work?", "how-does-counterspell-work"},
		{"already-canonical", "how-does-counterspell-work", "how-does-counterspell-work"},
		{"strips-accents", "Café Au Lait", "cafe-au-lait"},
		{"ligature-nfkd", "ﬁnding paths", "finding-paths"},
		{"fullwidth-digits", "Question ５ revisited", "question-5-revisited"},
		{"collapse-runs", "foo  --  bar", "foo-bar"},
		{"trim-edges", "---  fancy  ---", "fancy"},
		{"empty-input", "", ""},
		{"all-punctuation", "!!! ??? ...", ""},
		{"keeps-digits", "D&D 5e", "d-d-5e"},
		{"unicode-numbers", "α β γ", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalize(tc.in)
			if got != tc.want {
				t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeMaxLen(t *testing.T) {
	long := strings.Repeat("alpha-bravo-", 20) // 240 chars of hyphenated tokens
	got := Normalize(long)
	if len(got) > MaxLen {
		t.Errorf("len = %d, want <= %d", len(got), MaxLen)
	}
	if strings.HasSuffix(got, "-") {
		t.Errorf("truncated slug has trailing hyphen: %q", got)
	}
	if !strings.HasPrefix(long, got) {
		// Truncated slugs must remain a prefix of the canonical normalized
		// form (modulo trailing hyphen trim).
		t.Errorf("truncation diverged from prefix: %q vs %q", got, long)
	}
}

func TestNormalizeHardTruncate(t *testing.T) {
	// Single very long token with no hyphens — must hard-cut to MaxLen.
	one := strings.Repeat("a", 200)
	got := Normalize(one)
	if len(got) != MaxLen {
		t.Errorf("len = %d, want exactly %d (no hyphen to back off to)", len(got), MaxLen)
	}
}

func TestNormalizeIdempotent(t *testing.T) {
	// Normalize(Normalize(x)) == Normalize(x) — canonical form is a fixed point.
	for _, in := range []string{
		"How does counterspell work?",
		"Café Au Lait",
		"D&D 5e",
		"!!! ??? ...",
	} {
		first := Normalize(in)
		second := Normalize(first)
		if first != second {
			t.Errorf("not idempotent: %q → %q → %q", in, first, second)
		}
	}
}

// safeSlugRune reports whether r is one of the characters Normalize is allowed
// to emit.
func safeSlugRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
}

// Normalize's output is spliced into redirect targets as a trailing path
// segment (`{BasePath}/q/{id}/{slug}`), so it must never be able to introduce
// URL structure. That property is what makes those redirects safe despite
// carrying user-submitted titles, and it is why the suite excludes gosec's
// G710 taint rule (see .golangci.yml and infodancer/web#102) -- so it is
// asserted here rather than left as a comment on the exclusion.
//
// A leading hyphen is included: a segment starting with "-" is harmless in a
// URL, but the guarantee callers rely on is the whole allowlist, and pinning
// the shape of the output keeps a future rewrite honest.
func TestNormalizeEmitsNoURLStructure(t *testing.T) {
	hostile := []string{
		"//evil.example.com",
		"https://evil.example.com/pwned",
		"/../../etc/passwd",
		`\\evil.example.com\share`,
		"javascript:alert(1)",
		"?next=https://evil.example.com",
		"#fragment",
		"a/b/c",
		"a:b",
		"%2f%2fevil.example.com",
		"\x00/evil",
		"\r\nLocation: https://evil.example.com",
		"  //evil  ",
		"ⓗttps://evil.example.com",
	}
	for _, in := range hostile {
		t.Run(in, func(t *testing.T) {
			got := Normalize(in)
			for i, r := range got {
				if !safeSlugRune(r) {
					t.Errorf("Normalize(%q) = %q: rune %q at %d is outside [a-z0-9-]", in, got, r, i)
				}
			}
			if strings.HasPrefix(got, "-") {
				t.Errorf("Normalize(%q) = %q, must not begin with a hyphen", in, got)
			}
		})
	}
}

// The same guarantee, over arbitrary input rather than a fixed list.
func FuzzNormalizeEmitsNoURLStructure(f *testing.F) {
	for _, seed := range []string{"", "//evil", "a/b", "Café", "ﬁnding", "\x00", "----"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got := Normalize(in)
		for i, r := range got {
			if !safeSlugRune(r) {
				t.Errorf("Normalize(%q) = %q: rune %q at %d is outside [a-z0-9-]", in, got, r, i)
			}
		}
		if strings.HasPrefix(got, "-") {
			t.Errorf("Normalize(%q) = %q, must not begin with a hyphen", in, got)
		}
	})
}
