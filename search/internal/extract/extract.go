// Package extract reduces a document body to the plain-text projections the
// corpus stores: full body text (FTS source, weighted C), concatenated heading
// text (weighted B), and a sequence of heading-delimited sections for chunking
// (DESIGN §5.2). It never stores or returns the source HTML.
//
// Markdown is rendered through infodancer/ui/markdown — the single audited
// goldmark + bluemonday pipeline — and the sanitized HTML is then walked for
// text and structure. Routing through that package keeps one sanitization
// boundary rather than a second goldmark setup that could drift. HTML bodies
// are walked directly: we only ever emit escaped plain text plus our own
// snippet markup (DESIGN §10), so reducing untrusted HTML to text is safe.
package extract

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/infodancer/ui/markdown"
	"golang.org/x/net/html"
)

// Section is one heading-delimited slice of a document. HeadingPath is the
// breadcrumb of enclosing headings ("Combat > Initiative"); it is empty for
// the preamble that precedes the first heading (DESIGN §5.2). Text is the plain
// text under that heading, with the heading line itself excluded.
type Section struct {
	HeadingPath string
	Text        string
}

// Extracted is the plain-text projection of a document body.
type Extracted struct {
	// Title is the first H1's text when the body carries one, else "". It is a
	// fallback only: a caller with an explicit (frontmatter / DB) title should
	// prefer that and ignore this.
	Title string
	// BodyText is all non-heading prose, whitespace-normalized. FTS source.
	BodyText string
	// Headings is every heading's text joined by " ", whitespace-normalized.
	Headings string
	// Sections is the body split at headings, in document order.
	Sections []Section
}

// mdRenderer is the Markdown→sanitized-HTML pipeline used for extraction. GFM
// constructs are enabled so their text (table cells, task items) is captured;
// raw-HTML passthrough stays off because we extract text and never re-emit the
// source markup. UGCPolicy (the non-Restrictive base) preserves headings, which
// sectioning depends on.
var mdRenderer = markdown.New(markdown.Options{
	Tables:        true,
	Strikethrough: true,
	Autolink:      true,
	TaskList:      true,
})

// Markdown renders md through the audited pipeline and extracts its text.
func Markdown(md string) (Extracted, error) {
	return fromHTML(mdRenderer.RenderString(md))
}

// HTML extracts text directly from an HTML body. The source HTML is never
// stored or returned — only the reduced plain text is.
func HTML(h string) (Extracted, error) {
	return fromHTML(h)
}

// Text treats s as already-plain text: one preamble section, no headings.
func Text(s string) Extracted {
	t := normalizeSpace(s)
	if t == "" {
		return Extracted{}
	}
	return Extracted{
		BodyText: t,
		Sections: []Section{{Text: t}},
	}
}

// fromHTML parses an HTML fragment and walks it in document order, splitting
// sections at heading elements.
func fromHTML(htmlStr string) (Extracted, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		return Extracted{}, fmt.Errorf("extract: parse html: %w", err)
	}
	w := &walker{stack: make([]string, 6)}
	if body := doc.Find("body").Get(0); body != nil {
		w.walk(body)
	}
	w.flush()
	return w.result(), nil
}

// walker accumulates text while traversing the HTML tree, starting a new
// section each time it crosses a heading boundary.
type walker struct {
	stack    []string // current heading text per level (index = level-1)
	maxLevel int      // deepest level seen in the current path

	cur      strings.Builder // text of the section being built
	curPath  string          // heading path of the section being built
	sections []Section

	headings strings.Builder
	body     strings.Builder
	title    string
}

func (w *walker) walk(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			w.appendText(c.Data)
		case html.ElementNode:
			if level, ok := headingLevel(c.Data); ok {
				w.startHeading(level, nodeText(c))
				continue // heading text belongs to Headings, not the section body
			}
			w.walk(c)
		}
	}
}

// startHeading closes the current section and opens a new one scoped to the
// heading at the given level.
func (w *walker) startHeading(level int, text string) {
	w.flush()

	text = normalizeSpace(text)
	if text != "" {
		if w.headings.Len() > 0 {
			w.headings.WriteByte(' ')
		}
		w.headings.WriteString(text)
		if w.title == "" && level == 1 {
			w.title = text
		}
	}

	w.stack[level-1] = text
	w.maxLevel = level
	parts := make([]string, 0, level)
	for i := 0; i < level; i++ {
		if w.stack[i] != "" {
			parts = append(parts, w.stack[i])
		}
	}
	w.curPath = strings.Join(parts, " > ")
}

func (w *walker) appendText(s string) {
	if strings.TrimSpace(s) == "" {
		// Collapse inter-block whitespace to a single separating space so
		// adjacent blocks' words don't run together after normalization.
		w.cur.WriteByte(' ')
		return
	}
	w.cur.WriteString(s)
	w.cur.WriteByte(' ')
}

// flush finalizes the in-progress section, if it has any text.
func (w *walker) flush() {
	text := normalizeSpace(w.cur.String())
	w.cur.Reset()
	if text == "" {
		return
	}
	if w.body.Len() > 0 {
		w.body.WriteByte(' ')
	}
	w.body.WriteString(text)
	w.sections = append(w.sections, Section{HeadingPath: w.curPath, Text: text})
}

func (w *walker) result() Extracted {
	return Extracted{
		Title:    w.title,
		BodyText: w.body.String(),
		Headings: w.headings.String(),
		Sections: w.sections,
	}
}

// headingLevel reports whether tag is an h1–h6 element and its level.
func headingLevel(tag string) (int, bool) {
	if len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6' {
		return int(tag[1] - '0'), true
	}
	return 0, false
}

// nodeText returns the concatenated text content of a node subtree.
func nodeText(n *html.Node) string {
	var b strings.Builder
	var rec func(*html.Node)
	rec = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(n)
	return b.String()
}

// normalizeSpace collapses runs of whitespace to single spaces and trims.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
