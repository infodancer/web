package handler

import (
	"net/http"
	"strings"
	"testing"
)

func withBaseURL(u string) func(*Deps) {
	return func(d *Deps) { d.BaseURL = u }
}

func TestProfilePageEmitsCanonicalAndPersonJSONLD(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, nil, withBaseURL("https://example.org"))

	body := do(t, h, http.MethodGet, basePath+"/alice", nil, nil).Body.String()
	for _, want := range []string{
		`<link rel="canonical" href="https://example.org/profile/alice">`,
		`"@type":"Person"`,
		`"name":"Alice"`,
		`"url":"https://example.org/profile/alice"`,
		`"image":"https://example.org/profile/avatar/alice"`,
		`"sameAs":["https://example.org/blog"]`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestProfilePageDescriptionFromBio(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs) // bio: "Likes *dice* and <script>alert(1)</script>"
	h := newServer(t, fs, nil, withBaseURL("https://example.org"))

	body := do(t, h, http.MethodGet, basePath+"/alice", nil, nil).Body.String()
	if !strings.Contains(body, `content="Likes dice and`) {
		t.Error("meta description not derived from bio")
	}
}

func TestNoBaseURLSkipsAbsoluteMarkup(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, nil)

	body := do(t, h, http.MethodGet, basePath+"/alice", nil, nil).Body.String()
	for _, never := range []string{`rel="canonical"`, "application/ld+json"} {
		if strings.Contains(body, never) {
			t.Errorf("page emitted %q without a BaseURL", never)
		}
	}
}

func TestBioSummary(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"Plain text.", "Plain text."},
		{"Likes *dice* and **maps**.", "Likes dice and maps."},
		{"See [my site](https://example.org) for more.", "See my site for more."},
		{"# Heading\n\n> quoted\nline two", "Heading quoted line two"},
		{strings.Repeat("word ", 60), strings.TrimRight(strings.Repeat("word ", 60)[:strings.LastIndexByte(strings.Repeat("word ", 60)[:160], ' ')], " ") + "..."},
	}
	for _, c := range cases {
		if got := bioSummary(c.in, 160); got != c.want {
			t.Errorf("bioSummary(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
