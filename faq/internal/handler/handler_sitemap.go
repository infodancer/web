package handler

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/infodancer/web/faq/store"
)

// sitemapPartSize is the maximum URLs per sub-sitemap. The sitemap.org
// spec caps at 50,000.
const sitemapPartSize = 50_000

// sitemapXMLNamespace is the standard sitemaps.org schema URL emitted
// on every <urlset> / <sitemapindex> root.
const sitemapXMLNamespace = "http://www.sitemaps.org/schemas/sitemap/0.9"

// sitemapSubsitePrefix marks a sub-sitemap path as scoped to a subsite.
// The full path is `/sitemap/subsite-{slug}-{N}.xml`. This shape keeps
// the existing /sitemap/{name} route working — sitemapPartImpl
// dispatches on the prefix.
const sitemapSubsitePrefix = "subsite-"

// sitemapIndexImpl handles GET /sitemap.xml. Emits a sitemap-index
// XML referencing one or more sub-sitemaps:
//
//   - /sitemap/{N}.xml for the global corpus (paginated by sitemapPartSize)
//   - /sitemap/subsite-{slug}-{N}.xml for each subsite, also paginated
//     by total reachable questions (native + tag-matched)
//
// Per DESIGN.md §18.4 D7: native questions appear once in their own
// subsite's section; imported questions appear in every subsite they
// tag-match plus the global section. The global section's canonical
// is /q/{id}/{slug}; the subsite section's URLs are subsite-prefixed
// so search engines can index both forms.
func (s *Server) sitemapIndexImpl(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	total, err := s.deps.Store.CountQuestions(ctx)
	if err != nil {
		s.internalError(w, r, "sitemap count", err)
		return
	}
	globalParts := partCount(total)

	subs, err := s.deps.Store.ListSubsites(ctx)
	if err != nil {
		s.internalError(w, r, "sitemap: list subsites", err)
		return
	}

	base := s.absoluteBase(r)
	now := time.Now().UTC().Format("2006-01-02")

	type sm struct {
		XMLName xml.Name `xml:"sitemap"`
		Loc     string   `xml:"loc"`
		Lastmod string   `xml:"lastmod"`
	}
	type idx struct {
		XMLName xml.Name `xml:"sitemapindex"`
		Xmlns   string   `xml:"xmlns,attr"`
		Items   []sm     `xml:"sitemap"`
	}

	out := idx{Xmlns: sitemapXMLNamespace}
	for i := 0; i < globalParts; i++ {
		out.Items = append(out.Items, sm{
			Loc:     fmt.Sprintf("%s/sitemap/%d.xml", base, i+1),
			Lastmod: now,
		})
	}
	for _, sub := range subs {
		parts := partCount(sub.QuestionCount)
		for i := 0; i < parts; i++ {
			out.Items = append(out.Items, sm{
				Loc:     fmt.Sprintf("%s/sitemap/%s%s-%d.xml", base, sitemapSubsitePrefix, sub.Slug, i+1),
				Lastmod: now,
			})
		}
	}

	s.writeSitemapXML(w, out)
}

// sitemapPartImpl handles GET /sitemap/{name}. Dispatches by name
// shape:
//
//   - "{N}.xml" / "{N}" → global sub-sitemap N
//   - "subsite-{slug}-{N}.xml" → subsite sub-sitemap N, scoped to slug
func (s *Server) sitemapPartImpl(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(r.PathValue("name"), ".xml")
	if name == "" {
		s.notFound(w, r)
		return
	}
	if strings.HasPrefix(name, sitemapSubsitePrefix) {
		s.sitemapSubsitePart(w, r, strings.TrimPrefix(name, sitemapSubsitePrefix))
		return
	}
	s.sitemapGlobalPart(w, r, name)
}

// sitemapGlobalPart emits the per-question <urlset> for global sub-
// sitemap N. The handler accepts both "N" and "N.xml" forms (the .xml
// is stripped before reaching here).
func (s *Server) sitemapGlobalPart(w http.ResponseWriter, r *http.Request, raw string) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		s.notFound(w, r)
		return
	}
	ctx := r.Context()
	total, err := s.deps.Store.CountQuestions(ctx)
	if err != nil {
		s.internalError(w, r, "sitemap count", err)
		return
	}
	if n > partCount(total) {
		s.notFound(w, r)
		return
	}
	offset := int32((n - 1) * sitemapPartSize) //nolint:gosec // bounds-checked
	rows, err := s.deps.Store.ListQuestionsForSitemap(ctx, store.ListQuestionsForSitemapParams{
		OffsetN: offset, LimitN: sitemapPartSize,
	})
	if err != nil {
		s.internalError(w, r, "sitemap list", err)
		return
	}
	s.emitSitemapURLSet(w, r, rows, "")
}

// sitemapSubsitePart emits the per-question <urlset> for one subsite
// sub-sitemap. raw is the bit after "subsite-": "{slug}-{N}". The
// slug itself may contain hyphens, so we cut on the LAST hyphen — the
// integer N is always at the tail.
func (s *Server) sitemapSubsitePart(w http.ResponseWriter, r *http.Request, raw string) {
	cut := strings.LastIndex(raw, "-")
	if cut <= 0 || cut == len(raw)-1 {
		s.notFound(w, r)
		return
	}
	slug := raw[:cut]
	n, err := strconv.Atoi(raw[cut+1:])
	if err != nil || n < 1 {
		s.notFound(w, r)
		return
	}
	ctx := r.Context()
	sub, err := s.deps.Store.GetSubsiteBySlug(ctx, slug)
	if err != nil {
		// Unknown slug → 404, matches sitemap consumer expectations.
		s.notFound(w, r)
		return
	}
	total, err := s.deps.Store.CountSubsiteQuestions(ctx, &sub.ID)
	if err != nil {
		s.internalError(w, r, "sitemap: subsite count", err)
		return
	}
	if n > partCount(total) {
		s.notFound(w, r)
		return
	}
	offset := int32((n - 1) * sitemapPartSize) //nolint:gosec // bounds-checked
	rows, err := s.deps.Store.ListSubsiteQuestionsForSitemap(ctx, store.ListSubsiteQuestionsForSitemapParams{
		SubsiteID: &sub.ID,
		OffsetN:   offset,
		LimitN:    sitemapPartSize,
	})
	if err != nil {
		s.internalError(w, r, "sitemap: subsite list", err)
		return
	}
	// Subsite URLs are prefixed; pass slug as the URL infix.
	s.emitSitemapURLSet(w, r, convertSubsiteSitemapRows(rows), sub.Slug)
}

// sitemapRow is the common shape both global and subsite list rows
// project into for emitSitemapURLSet's loop.
type sitemapRow struct {
	ID      int64
	Title   string
	Lastmod time.Time
	HasMod  bool
}

func convertSubsiteSitemapRows(rows []store.ListSubsiteQuestionsForSitemapRow) []sitemapRow {
	out := make([]sitemapRow, len(rows))
	for i, r := range rows {
		row := sitemapRow{ID: r.ID, Title: r.Title}
		if r.Lastmod.Valid {
			row.Lastmod = r.Lastmod.Time
			row.HasMod = true
		}
		out[i] = row
	}
	return out
}

// emitSitemapURLSet writes the <urlset> for a slice of rows. If
// subsiteSlug is empty, the URLs use the global /q/{id}/{slug} form;
// otherwise the subsite-prefixed /{subsite}/q/{id}/{slug} form.
//
// The ctx parameter is unused but kept on the signature for future
// per-row enrichment (e.g. fetching a question's slug from a row that
// doesn't carry the title).
func (s *Server) emitSitemapURLSet(w http.ResponseWriter, r *http.Request, rows any, subsiteSlug string) {
	// Normalize input: support both store.ListQuestionsForSitemapRow
	// and the converted []sitemapRow slice.
	var entries []sitemapRow
	switch v := rows.(type) {
	case []sitemapRow:
		entries = v
	case []store.ListQuestionsForSitemapRow:
		entries = make([]sitemapRow, len(v))
		for i, e := range v {
			row := sitemapRow{ID: e.ID, Title: e.Title}
			if e.Lastmod.Valid {
				row.Lastmod = e.Lastmod.Time
				row.HasMod = true
			}
			entries[i] = row
		}
	default:
		s.deps.Logger.Error("sitemap: unexpected row type", "type", fmt.Sprintf("%T", rows))
		s.internalError(w, r, "sitemap: bad rows", nil)
		return
	}

	base := s.absoluteBase(r)
	prefix := base
	if subsiteSlug != "" {
		prefix = base + "/" + subsiteSlug
	}

	type url struct {
		XMLName xml.Name `xml:"url"`
		Loc     string   `xml:"loc"`
		Lastmod string   `xml:"lastmod"`
	}
	type urlset struct {
		XMLName xml.Name `xml:"urlset"`
		Xmlns   string   `xml:"xmlns,attr"`
		URLs    []url    `xml:"url"`
	}
	out := urlset{Xmlns: sitemapXMLNamespace, URLs: make([]url, len(entries))}
	for i, e := range entries {
		var lastmod string
		if e.HasMod {
			lastmod = e.Lastmod.UTC().Format("2006-01-02")
		}
		out.URLs[i] = url{
			Loc:     fmt.Sprintf("%s/q/%d/%s", prefix, e.ID, slugFor(e.ID, e.Title)),
			Lastmod: lastmod,
		}
	}
	s.writeSitemapXML(w, out)
}

// partCount returns the number of sub-sitemaps needed for total
// questions, rounded up. Always returns at least 1 so the index has
// at least one entry on an empty corpus or empty subsite.
func partCount(total int64) int {
	if total <= 0 {
		return 1
	}
	return int((total + sitemapPartSize - 1) / sitemapPartSize)
}

// absoluteBase returns the absolute URL prefix for module-served URLs
// (e.g. "https://example.com/faq"). Picks the scheme from
// X-Forwarded-Proto when present, falling back to "https" when in doubt
// (sitemap consumers prefer https-only URLs anyway).
func (s *Server) absoluteBase(r *http.Request) string {
	scheme := "https"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS == nil && r.Host == "" {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s%s", scheme, r.Host, s.deps.BasePath)
}

func (s *Server) writeSitemapXML(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		s.deps.Logger.Warn("sitemap: write header", "err", err)
		return
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(payload); err != nil {
		s.deps.Logger.Error("sitemap: encode", "err", err)
	}
}

// Touch context to avoid the unused-import lint if the ctx-only paths
// above ever change.
var _ = context.Background
