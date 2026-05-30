// Package sanitize provides the shared HTML allowlist policy used by every
// faq code path that writes HTML to the database or to a response. Two
// kinds of callers consume it:
//
//   - import tooling sanitizes upstream-supplied HTML before storing it as
//     body_html on imported rows.
//   - internal/render/markdown.go (M4) sanitizes goldmark's output before
//     handing it back to templates as template.HTML.
//
// Both call sites must produce HTML that survives the same allowlist so
// imported and native content are visually equivalent in the rendered
// page. Centralizing the policy here is what keeps that invariant: a tag
// added once shows up for both call paths automatically.
package sanitize

import (
	"regexp"

	"github.com/microcosm-cc/bluemonday"
)

// Policy returns a fresh bluemonday policy configured for faq content.
//
// Base: bluemonday.UGCPolicy() — the canonical user-generated-content
// allowlist. It permits the markup goldmark and html-to-markdown emit
// (headings, paragraphs, lists, code, blockquotes, links, images,
// emphasis) and strips script/style/iframe/object/embed/form tags plus
// every on* event-handler attribute.
//
// Extensions: kbd, sub, sup, mark — common in imported Q&A bodies and useful for
// native posts too. UGCPolicy does not include them by default.
//
// Each call returns a new Policy so callers are free to further customize
// without affecting each other. bluemonday policies are safe for
// concurrent reads once configured, so most callers can build one in a
// sync.OnceValues and reuse it.
func Policy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowElements("kbd", "sub", "sup", "mark")
	return p
}

// StripEmptyDivs removes empty <div>...</div> blocks (whitespace-only
// content is treated as empty) from a sanitized HTML string. Runs
// iteratively so nested empties like <div><div></div></div> collapse in
// one call.
//
// Imported post HTML frequently ends with trailing
// <p><div><div></div></div></p> editor artifacts from older composer
// versions. bluemonday's UGCPolicy keeps them because <div> is
// allowlisted, and they add nothing semantically but bloat the rendered
// page and clutter the markup.
//
// Callers should apply this AFTER bluemonday: the sanitize pass may
// remove disallowed attributes that would otherwise have kept a <div>
// "non-empty" in the byte-level sense. Inputs are expected to be
// well-formed enough that a tag-balance regex is adequate — bluemonday
// guarantees this in practice.
func StripEmptyDivs(html string) string {
	for {
		next := emptyDivPattern.ReplaceAllString(html, "")
		if next == html {
			return html
		}
		html = next
	}
}

// Matches a <div> with optional attributes, containing only whitespace,
// closed by </div>. The character class [^<>] in the body ensures we
// don't accidentally swallow a nested non-empty element.
var emptyDivPattern = regexp.MustCompile(`<div\b[^>]*>\s*</div>`)
