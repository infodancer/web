// Package sitemap produces XML sitemaps for sites assembled from
// infodancer/web components plus their own custom code. It is used at two
// altitudes in the same site:
//
//   - a Leaf mount inside each content module (e.g. /faq/sitemap.xml) serves
//     that module's URLs as a <urlset>, auto-promoting to a paginated
//     sub-index past the sitemaps.org cap;
//   - an Index mount at the host root (/sitemap.xml) stitches the leaves into
//     a <sitemapindex>.
//
// Inclusion is explicit: a URL appears only because a registered Source emits
// it. The package never walks the filesystem or enumerates a mux. See
// DESIGN.md.
package sitemap

import (
	"context"
	"encoding/xml"
	"path"
	"strings"
	"time"
)

// MaxURLsPerFile is the sitemaps.org cap on URLs in a single <urlset>. A Leaf
// whose source exceeds it promotes to a <sitemapindex> over paginated parts.
const MaxURLsPerFile = 50_000

// xmlNamespace is the sitemaps.org schema URL emitted on every <urlset> and
// <sitemapindex> root.
const xmlNamespace = "http://www.sitemaps.org/schemas/sitemap/0.9"

// Entry is one URL in a sitemap.
type Entry struct {
	// Loc is the URL's location: a root-relative path ("/book/foo") joined to
	// the configured BaseURL, or an already-absolute URL (used as-is).
	Loc string
	// LastMod is the last-modified time. The zero value omits <lastmod>, which
	// is valid -- a sitemap without lastmod is well-formed.
	LastMod time.Time
}

// Source streams a content area's URLs. The yield-iterator shape lets a large
// catalog stream without materializing the whole set; returning false from
// yield stops iteration early (the caller filled a page).
type Source interface {
	SitemapEntries(ctx context.Context, yield func(Entry) bool) error
}

// SourceFunc adapts a plain function to a Source -- the door for custom site
// code (a book catalog, a bestiary) that has no module of its own.
type SourceFunc func(ctx context.Context, yield func(Entry) bool) error

// SitemapEntries implements Source.
func (f SourceFunc) SitemapEntries(ctx context.Context, yield func(Entry) bool) error {
	return f(ctx, yield)
}

// PagedSource is an optional optimization. A Source that can report its size
// and serve a window without streaming the whole set lets a Leaf paginate
// efficiently (the shape a SQL-backed module already has: COUNT plus
// LIMIT/OFFSET). Sources that do not implement it are materialized and cached
// instead. A Leaf prefers PagedSource when the source satisfies it.
type PagedSource interface {
	Source
	// SitemapCount reports the total number of entries.
	SitemapCount(ctx context.Context) (int, error)
	// SitemapPage streams entries [offset, offset+limit) in the same order
	// SitemapEntries would yield them.
	SitemapPage(ctx context.Context, offset, limit int, yield func(Entry) bool) error
}

// partCount returns the number of paginated parts needed for total entries,
// rounded up, with a floor of 1 so an index always has at least one child even
// on an empty source.
func partCount(total, perPage int) int {
	if total <= 0 {
		return 1
	}
	return (total + perPage - 1) / perPage
}

// joinURL joins a configured origin with an entry Loc. An already-absolute Loc
// is returned unchanged; otherwise the origin and a root-relative path are
// concatenated with exactly one slash between them.
func joinURL(origin, loc string) string {
	if loc == "" {
		return origin
	}
	if strings.HasPrefix(loc, "http://") || strings.HasPrefix(loc, "https://") {
		return loc
	}
	return strings.TrimRight(origin, "/") + "/" + strings.TrimLeft(loc, "/")
}

// formatLastMod renders a time as a sitemap <lastmod> date, or "" for the zero
// value (so the field is omitted).
func formatLastMod(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

// XML payload types, shared by Leaf and Index.

type urlEntry struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type urlSet struct {
	XMLName xml.Name   `xml:"urlset"`
	Xmlns   string     `xml:"xmlns,attr"`
	URLs    []urlEntry `xml:"url"`
}

type sitemapEntry struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type sitemapIndex struct {
	XMLName  xml.Name       `xml:"sitemapindex"`
	Xmlns    string         `xml:"xmlns,attr"`
	Sitemaps []sitemapEntry `xml:"sitemap"`
}

// renderXML marshals a payload with the XML header and indentation, matching
// the suite's existing sitemap output.
func renderXML(payload any) ([]byte, error) {
	body, err := xml.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

// selfStem returns the absolute stem of a leaf's own sitemap URLs:
// origin + publicPrefix + "/" + name, with slashes cleaned. The index lives at
// stem+".xml"; parts live at stem+"/{n}.xml".
func selfStem(origin, publicPrefix, name string) string {
	p := path.Join("/", publicPrefix, name)
	return strings.TrimRight(origin, "/") + p
}
