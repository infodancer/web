package handler

import (
	"encoding/xml"
	"net/http"
	"time"
)

// sitemapMax caps the URL count per the sitemaps.org spec. Portfolio blogs
// sit far below it; paged sitemap indexes are a v0.2 concern if one ever
// approaches the cap.
const sitemapMax = 50_000

type urlset struct {
	XMLName xml.Name     `xml:"urlset"`
	NS      string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// handleSitemap emits one urlset covering the index, named blogs, published
// posts, author pages, and tag pages.
func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	set := urlset{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	add := func(rel string, mod time.Time) {
		if len(set.URLs) >= sitemapMax {
			return
		}
		u := sitemapURL{Loc: s.absURL(rel)}
		if !mod.IsZero() {
			u.LastMod = mod.UTC().Format("2006-01-02")
		}
		set.URLs = append(set.URLs, u)
	}

	add("/", time.Time{})

	blogs, err := s.store.ListBlogs(ctx)
	if err != nil {
		s.serverError(w, "sitemap blogs", err)
		return
	}
	for _, b := range blogs {
		add("/b/"+b.Slug, time.Time{})
	}

	posts, _, err := s.store.ListPublished(ctx, sitemapMax, 0)
	if err != nil {
		s.serverError(w, "sitemap posts", err)
		return
	}
	for _, p := range posts {
		add("/p/"+p.Slug, p.UpdatedAt)
	}

	authors, err := s.store.ListAuthors(ctx)
	if err != nil {
		s.serverError(w, "sitemap authors", err)
		return
	}
	for _, a := range authors {
		add("/authors/"+a.Slug, time.Time{})
	}

	tags, err := s.store.ListTags(ctx)
	if err != nil {
		s.serverError(w, "sitemap tags", err)
		return
	}
	for _, t := range tags {
		add("/tags/"+t.Slug, time.Time{})
	}

	out, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		s.serverError(w, "sitemap encode", err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(out)
}
