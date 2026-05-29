package extract

import (
	"strings"
	"testing"
)

func TestMarkdown_SectionsSplitOnHeadings(t *testing.T) {
	md := "# Title\n\nIntro prose.\n\n## Alpha\n\nAlpha body.\n\n## Beta\n\nBeta body."
	got, err := Markdown(md)
	if err != nil {
		t.Fatalf("Markdown: %v", err)
	}

	if got.Title != "Title" {
		t.Errorf("Title = %q, want %q", got.Title, "Title")
	}
	// One section per heading plus none for the preamble: the preamble here sits
	// under the H1, so the first section's path is "Title".
	wantPaths := []string{"Title", "Title > Alpha", "Title > Beta"}
	if len(got.Sections) != len(wantPaths) {
		t.Fatalf("got %d sections %v, want %d", len(got.Sections), paths(got.Sections), len(wantPaths))
	}
	for i, w := range wantPaths {
		if got.Sections[i].HeadingPath != w {
			t.Errorf("section %d path = %q, want %q", i, got.Sections[i].HeadingPath, w)
		}
	}

	// Heading text is captured in Headings, not in body sections.
	for _, h := range []string{"Title", "Alpha", "Beta"} {
		if !strings.Contains(got.Headings, h) {
			t.Errorf("Headings %q missing %q", got.Headings, h)
		}
	}
	// The heading line is captured in Headings, so the section body is the prose
	// alone — not "Alpha\nAlpha body.".
	if got.Sections[1].Text != "Alpha body." {
		t.Errorf("section 1 text = %q, want %q", got.Sections[1].Text, "Alpha body.")
	}
}

func TestMarkdown_PreambleBeforeFirstHeadingHasEmptyPath(t *testing.T) {
	md := "Loose intro with no heading yet.\n\n## Later\n\nUnder later."
	got, err := Markdown(md)
	if err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	if len(got.Sections) < 1 {
		t.Fatal("expected a preamble section")
	}
	if got.Sections[0].HeadingPath != "" {
		t.Errorf("preamble path = %q, want empty", got.Sections[0].HeadingPath)
	}
	if got.Sections[0].Text != "Loose intro with no heading yet." {
		t.Errorf("preamble text = %q", got.Sections[0].Text)
	}
}

func TestMarkdown_DeepHeadingBreadcrumb(t *testing.T) {
	md := "# A\n\n## B\n\n### C\n\nLeaf text."
	got, err := Markdown(md)
	if err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	last := got.Sections[len(got.Sections)-1]
	if last.HeadingPath != "A > B > C" {
		t.Errorf("deep path = %q, want %q", last.HeadingPath, "A > B > C")
	}
}

func TestMarkdown_StripsMarkupToText(t *testing.T) {
	md := "## H\n\nThis is **bold** and `code` and a [link](https://example.com)."
	got, err := Markdown(md)
	if err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	if strings.ContainsAny(got.BodyText, "<>") {
		t.Errorf("body text retained markup: %q", got.BodyText)
	}
	for _, w := range []string{"bold", "code", "link"} {
		if !strings.Contains(got.BodyText, w) {
			t.Errorf("body text %q missing word %q", got.BodyText, w)
		}
	}
}

func TestMarkdown_DropsDangerousHTML(t *testing.T) {
	// Raw HTML is filtered by bluemonday; the script must not survive into text.
	md := "## H\n\nSafe text.\n\n<script>alert('xss')</script>\n\n<img src=x onerror=alert(1)>"
	got, err := Markdown(md)
	if err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	if strings.Contains(got.BodyText, "alert") || strings.Contains(strings.ToLower(got.BodyText), "script") {
		t.Errorf("dangerous content leaked into body text: %q", got.BodyText)
	}
	if !strings.Contains(got.BodyText, "Safe text") {
		t.Errorf("safe text dropped: %q", got.BodyText)
	}
}

func TestText_SinglePreambleSection(t *testing.T) {
	got := Text("  plain   text  with   spaces ")
	if got.BodyText != "plain text with spaces" {
		t.Errorf("BodyText = %q", got.BodyText)
	}
	if len(got.Sections) != 1 || got.Sections[0].HeadingPath != "" {
		t.Errorf("want one empty-path section, got %v", paths(got.Sections))
	}
	if got.Headings != "" {
		t.Errorf("plain text should have no headings, got %q", got.Headings)
	}
}

func TestText_EmptyIsEmpty(t *testing.T) {
	got := Text("   ")
	if got.BodyText != "" || len(got.Sections) != 0 {
		t.Errorf("blank text should extract nothing, got %+v", got)
	}
}

func TestHTML_ExtractsTextAndHeadings(t *testing.T) {
	got, err := HTML("<h2>Heading</h2><p>Para one.</p><p>Para two.</p>")
	if err != nil {
		t.Fatalf("HTML: %v", err)
	}
	if got.Headings != "Heading" {
		t.Errorf("Headings = %q", got.Headings)
	}
	if got.Sections[0].Text != "Para one. Para two." {
		t.Errorf("section text = %q, want %q", got.Sections[0].Text, "Para one. Para two.")
	}
}

func paths(ss []Section) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.HeadingPath
	}
	return out
}
