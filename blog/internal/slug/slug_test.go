package slug

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Hello, World!", "hello-world"},
		{"Café au lait", "cafe-au-lait"},
		{"  --- spaced ---  ", "spaced"},
		{"The Plague at Bobwhite Castle", "the-plague-at-bobwhite-castle"},
		{"ALL CAPS 123", "all-caps-123"},
		{"日本語のみ", ""},
		{"", ""},
		{"a&b@c", "a-b-c"},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeTruncates(t *testing.T) {
	long := strings.Repeat("word-", 30) // 150 chars
	got := Normalize(long)
	if len(got) > MaxLen {
		t.Fatalf("len = %d, want <= %d", len(got), MaxLen)
	}
	if strings.HasSuffix(got, "-") {
		t.Fatalf("trailing hyphen in %q", got)
	}
}
