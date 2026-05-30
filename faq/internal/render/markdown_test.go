package render

import (
	"strings"
	"testing"
)

func TestRenderMarkdownEmpty(t *testing.T) {
	if got := RenderMarkdown(""); got != "" {
		t.Errorf(`RenderMarkdown("") = %q, want ""`, got)
	}
	if got := RenderMarkdown("   \n\t  "); got != "" {
		t.Errorf("whitespace-only should yield empty, got %q", got)
	}
}

func TestRenderMarkdownBasic(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"paragraph", "Hello, world.", "<p>Hello, world.</p>"},
		{"bold", "**bold**", "<strong>bold</strong>"},
		{"italic", "*italic*", "<em>italic</em>"},
		{"link", "[osg](https://osg.example)", `<a href="https://osg.example"`},
		{"unordered-list", "- one\n- two", "<li>one</li>"},
		{"heading", "# Title", "<h1>Title</h1>"},
		{"blockquote", "> quoted", "<blockquote>"},
		{"inline-code", "use `make` to build", "<code>make</code>"},
		{"fenced-code", "```\nx := 1\n```", "<pre><code>"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(RenderMarkdown(tc.in))
			if !strings.Contains(got, tc.want) {
				t.Errorf("RenderMarkdown(%q) = %q, missing %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRenderMarkdownGFM(t *testing.T) {
	gfm := []struct {
		name string
		in   string
		want string
	}{
		{"table", "| a | b |\n| - | - |\n| 1 | 2 |", "<table>"},
		{"strikethrough", "~~struck~~", "<del>struck</del>"},
		{"autolink", "visit https://example.com please", `<a href="https://example.com"`},
		// Task lists render as plain bullet lists in v0.1: bluemonday's
		// UGCPolicy strips <input> tags (correctly — they're form
		// elements). We could allow <input type="checkbox" disabled>
		// specifically, but it's a niche feature for a Q&A site and
		// the degraded rendering (a bullet list) is acceptable.
		{"task-list-strikes-checkbox", "- [x] done\n- [ ] todo", "<li>"},
	}
	for _, tc := range gfm {
		t.Run(tc.name, func(t *testing.T) {
			got := string(RenderMarkdown(tc.in))
			if !strings.Contains(got, tc.want) {
				t.Errorf("GFM input %q produced %q, missing %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRenderMarkdownStripsRawHTML(t *testing.T) {
	// goldmark without WithUnsafe elides raw HTML, and bluemonday
	// strips anything that slips through. Both layers must agree.
	bad := []string{
		`<script>alert(1)</script>`,
		`<iframe src="x"></iframe>`,
		`<a href="javascript:alert(1)">x</a>`,
	}
	for _, in := range bad {
		got := string(RenderMarkdown(in))
		if strings.Contains(got, "<script") ||
			strings.Contains(got, "<iframe") ||
			strings.Contains(got, "javascript:") {
			t.Errorf("dangerous content leaked: input=%q output=%q", in, got)
		}
	}
}

func TestRenderMarkdownAllowsSharedInlineTags(t *testing.T) {
	// The shared sanitize policy permits kbd/sub/sup/mark, so callers
	// can mix raw HTML inline tags with their markdown. goldmark
	// without WithUnsafe normally elides raw HTML — but it preserves
	// inline HTML at the span level when no block-level wrapper is
	// involved. (If goldmark elides it, the test will flag the gap.)
	in := "press <kbd>Esc</kbd> then continue"
	got := string(RenderMarkdown(in))
	if !strings.Contains(got, "<kbd>Esc</kbd>") {
		t.Errorf("inline <kbd> should survive (sanitize policy allows it): %q", got)
	}
}

func TestRenderMarkdownStringMatchesHTML(t *testing.T) {
	in := "**bold**"
	if RenderMarkdownString(in) != string(RenderMarkdown(in)) {
		t.Error("RenderMarkdownString diverges from RenderMarkdown")
	}
}
