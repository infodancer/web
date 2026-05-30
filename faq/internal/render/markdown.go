package render

import (
	"bytes"
	"html"
	"html/template"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"

	"github.com/infodancer/web/faq/sanitize"
)

// Markdown rendering for user-authored fields (question body, answer
// body, comment body).
//
// Pipeline:
//
//  1. goldmark parses CommonMark + GFM extensions (tables, strikethrough,
//     autolinks, task lists). WithUnsafe IS set: raw HTML passes through
//     goldmark unchanged. This is deliberate — import tooling stores
//     raw inline HTML (kbd/sub/sup/mark) inside body_md for imported
//     content, and the M4 edit flow must round-trip those tags. Without
//     WithUnsafe, every edit of an imported post would silently strip
//     those tags. goldmark's IsDangerousURL still rejects javascript:,
//     vbscript:, data: text/html, and file: link targets.
//
//  2. The shared faq sanitize policy (internal/sanitize) is the actual
//     security boundary. It strips script/style/iframe/object/embed/form
//     plus on* attributes, and allows only the inline HTML tags we
//     want in output (UGCPolicy + kbd/sub/sup/mark). bluemonday is
//     designed for exactly this "trust nothing, filter strictly" role.
//
// We return template.HTML so html/template won't double-escape it. The
// contract: only the sanitized output of this function may be cast to
// template.HTML for these fields.
//
// faq diverges from osg here: osg's render uses plain goldmark.New()
// (CommonMark only). See the [portfolio-wide task #4166] to bring osg
// to GFM if/when the divergence becomes a problem.

// markdownPipeline is built once and reused for every render call. Both
// goldmark.Markdown and *bluemonday.Policy are safe for concurrent reads
// once constructed.
var markdownPipeline = sync.OnceValues(func() (goldmark.Markdown, sanitizePolicy) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
	)
	return md, sanitize.Policy()
})

// sanitizePolicy is a tiny alias so we can name the OnceValues return
// type without leaking bluemonday into every file that talks to it.
type sanitizePolicy = interface {
	Sanitize(string) string
	SanitizeBytes([]byte) []byte
}

// RenderMarkdown converts a Markdown source string to safe HTML suitable
// for dropping into a template. Output is sanitized to remove
// script/style/iframe/object/embed/form tags and on* event-handler
// attributes, while keeping the markup users actually need (headings,
// lists, code, links, images, blockquotes, tables, strikethrough,
// autolinks, kbd/sub/sup/mark).
//
// Empty or whitespace-only input returns "" so callers can guard with
// {{if}} on the original Markdown string and skip rendering entirely.
//
// (server/internal/directory/render/markdown.go); the "stutters" lint
// is suppressed to keep the two packages in lockstep.
//
//nolint:revive // RenderMarkdown matches osg's render package naming
func RenderMarkdown(md string) template.HTML {
	if strings.TrimSpace(md) == "" {
		return ""
	}
	gm, policy := markdownPipeline()
	var buf bytes.Buffer
	if err := gm.Convert([]byte(md), &buf); err != nil {
		// goldmark.Convert only errors on writer failure; bytes.Buffer
		// never fails to write. Fall back to escaped plain text so a
		// programming error here never leaks raw Markdown as HTML.
		return template.HTML(html.EscapeString(md)) //nolint:gosec // escaped above
	}
	clean := sanitize.StripEmptyDivs(string(policy.SanitizeBytes(buf.Bytes())))
	return template.HTML(clean) //nolint:gosec // sanitized by shared faq policy
}

// RenderMarkdownString is RenderMarkdown's plain-string variant for
// callers that don't want template.HTML in their type signatures (e.g.
// the M4 write-path handlers, which store body_html as a database
// column).
//
//nolint:revive // matches the package's intentional Render* naming
func RenderMarkdownString(md string) string {
	return string(RenderMarkdown(md))
}
