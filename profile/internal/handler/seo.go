package handler

import (
	"strings"
	"unicode"

	ui "github.com/infodancer/ui"

	"github.com/infodancer/web/profile/store"
)

// descriptionMax bounds the meta description built from the bio; search
// engines truncate around 160 characters anyway.
const descriptionMax = 160

// applyProfileMeta fills per-profile SEO onto the page meta: a description
// from the bio, and -- when a BaseURL is configured -- the canonical URL,
// social-card image, and a Person JSON-LD graph.
func (s *Server) applyProfileMeta(meta *ui.Meta, p *store.Profile) {
	name := displayLabel(p)
	meta.Title = name
	if d := bioSummary(p.BioMD, descriptionMax); d != "" {
		meta.Description = d
	}
	if s.baseURL == "" {
		return
	}

	canonical := s.baseURL + s.basePath + "/" + p.Handle
	meta.Canonical = canonical
	image := s.absoluteURL(s.avatarSrc(p))
	meta.Image = image

	person := map[string]any{
		"@context": "https://schema.org",
		"@type":    "Person",
		"name":     name,
		"url":      canonical,
	}
	if image != "" {
		person["image"] = image
	}
	if meta.Description != "" {
		person["description"] = meta.Description
	}
	if len(p.Links) > 0 {
		sameAs := make([]string, 0, len(p.Links))
		for _, l := range p.Links {
			sameAs = append(sameAs, l.URL)
		}
		person["sameAs"] = sameAs
	}
	ld, err := ui.JSONLD(person)
	if err != nil {
		s.log.Warn("profile json-ld", "err", err)
		return
	}
	meta.JSONLD = append(meta.JSONLD, ld)
}

// absoluteURL prefixes site-relative URLs with the configured origin;
// already-absolute URLs (an external avatar CDN) pass through.
func (s *Server) absoluteURL(u string) string {
	if u == "" || !strings.HasPrefix(u, "/") {
		return u
	}
	return s.baseURL + u
}

// bioSummary reduces bio markdown to a single plain-text line for the meta
// description: inline links keep their text, markup punctuation is dropped,
// whitespace collapses, and the result truncates on a word boundary. It is a
// summary heuristic, not a markdown parser -- good enough for a description
// tag, never rendered as HTML.
func bioSummary(md string, maxLen int) string {
	if md == "" {
		return ""
	}
	// [text](url) -> text, then strip residual markup characters.
	var b strings.Builder
	b.Grow(len(md))
	skipURL := false
	for i := 0; i < len(md); i++ {
		c := md[i]
		switch {
		case skipURL:
			if c == ')' {
				skipURL = false
			}
		case c == ']' && i+1 < len(md) && md[i+1] == '(':
			skipURL = true
			i++ // consume the '('
		case strings.ContainsRune("*_`#>[!", rune(c)):
			// markdown emphasis/heading/quote/image punctuation
		default:
			b.WriteByte(c)
		}
	}
	out := strings.Join(strings.FieldsFunc(b.String(), unicode.IsSpace), " ")
	if len(out) <= maxLen {
		return out
	}
	cut := strings.LastIndexByte(out[:maxLen], ' ')
	if cut <= 0 {
		cut = maxLen
	}
	return strings.TrimRight(out[:cut], " ,;:.") + "..."
}
