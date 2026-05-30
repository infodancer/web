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
