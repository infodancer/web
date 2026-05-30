package sanitize

import (
	"strings"
	"testing"
)

func TestPolicyAllowsCommonInlineTags(t *testing.T) {
	p := Policy()
	allowed := []struct {
		name string
		in   string
		want string
	}{
		{"kbd", "Press <kbd>Esc</kbd>.", "<kbd>Esc</kbd>"},
		{"sub", "x<sub>1</sub>", "<sub>1</sub>"},
		{"sup", "x<sup>2</sup>", "<sup>2</sup>"},
		{"mark", "<mark>note</mark>", "<mark>note</mark>"},
		{"strong", "<strong>x</strong>", "<strong>x</strong>"},
		{"em", "<em>x</em>", "<em>x</em>"},
	}
	for _, tc := range allowed {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Sanitize(tc.in)
			if !strings.Contains(got, tc.want) {
				t.Errorf("Sanitize(%q) = %q, missing %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPolicyStripsDangerousTags(t *testing.T) {
	p := Policy()
	dangerous := []string{
		`<script>alert('x')</script>`,
		`<iframe src="x"></iframe>`,
		`<object data="x"></object>`,
		`<embed src="x">`,
		`<form action="x"><input></form>`,
		`<style>body { display: none; }</style>`,
	}
	for _, in := range dangerous {
		got := p.Sanitize(in)
		if strings.Contains(got, "<script") ||
			strings.Contains(got, "<iframe") ||
			strings.Contains(got, "<object") ||
			strings.Contains(got, "<embed") ||
			strings.Contains(got, "<form") ||
			strings.Contains(got, "<style") {
			t.Errorf("dangerous tag survived: input=%q output=%q", in, got)
		}
	}
}

func TestPolicyStripsEventHandlers(t *testing.T) {
	p := Policy()
	out := p.Sanitize(`<a href="x" onclick="alert(1)">link</a>`)
	if strings.Contains(out, "onclick") {
		t.Errorf("on* handler survived: %q", out)
	}
}

func TestPolicyReturnsFreshInstance(t *testing.T) {
	// Each call yields an independent policy. Mutating one does not
	// affect another (callers commonly cache one in sync.OnceValues).
	a := Policy()
	b := Policy()
	if a == b {
		t.Error("Policy() should return distinct instances")
	}
}

func TestStripEmptyDivs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"flat-empty", `<p>hi</p><div></div>`, `<p>hi</p>`},
		{"nested-empty", `<p>hi</p><div><div></div></div>`, `<p>hi</p>`},
		{
			"trailing-div-artifact",
			`<p>real content</p>` + "\n" + `<p><div><div></div></div></p>`,
			`<p>real content</p>` + "\n" + `<p></p>`,
		},
		{"with-attrs", `<div class="x"></div><p>kept</p>`, `<p>kept</p>`},
		{"whitespace-content", "<div>  \n  </div><p>kept</p>", `<p>kept</p>`},
		{"non-empty-untouched", `<div><p>real</p></div>`, `<div><p>real</p></div>`},
		{"text-inside-untouched", `<div>text</div>`, `<div>text</div>`},
		{"empty-strings", ``, ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StripEmptyDivs(tc.in)
			if got != tc.want {
				t.Errorf("StripEmptyDivs(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
