package handler

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/infodancer/web/timeline/db"
)

// ---- Sitemap ----

type sitemapURL struct {
	XMLName xml.Name `xml:"url"`
	Loc     string   `xml:"loc"`
	LastMod string   `xml:"lastmod,omitempty"`
}

type sitemapIndex struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	logger := s.logger(r)
	slug := r.PathValue("slug")
	ctx := r.Context()

	tl, err := s.store.GetTimeline(ctx, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.canViewTimeline(r.Context(), tl.IsPublic) {
		http.NotFound(w, r)
		return
	}

	baseURL := s.baseURL
	if baseURL == "" {
		baseURL = "http://" + r.Host
	}

	sitemap := sitemapIndex{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
	}

	// Timeline page
	sitemap.URLs = append(sitemap.URLs, sitemapURL{
		Loc:     fmt.Sprintf("%s/t/%s", baseURL, tl.Slug),
		LastMod: tl.UpdatedAt.Time.Format("2006-01-02"),
	})

	// All events
	events, err := s.store.ListEventsAfterDate(ctx, db.ListEventsAfterDateParams{
		TimelineID:     tl.ID,
		AfterDate:      pgtype.Date{Time: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		AfterSortOrder: -1,
		MaxResults:     100000,
	})
	if err != nil {
		// Degraded: the sitemap omits events rather than failing the response.
		logger.Warn("sitemap events", "err", err)
	}
	for _, e := range events {
		sitemap.URLs = append(sitemap.URLs, sitemapURL{
			Loc:     fmt.Sprintf("%s/t/%s/event/%s", baseURL, tl.Slug, e.Slug),
			LastMod: e.UpdatedAt.Time.Format("2006-01-02"),
		})
	}

	// All people
	people, err := s.store.ListPeople(ctx, tl.ID)
	if err != nil {
		// Degraded: the sitemap omits people rather than failing the response.
		logger.Warn("sitemap people", "err", err)
	}
	for _, p := range people {
		sitemap.URLs = append(sitemap.URLs, sitemapURL{
			Loc:     fmt.Sprintf("%s/t/%s/person/%s", baseURL, tl.Slug, p.Slug),
			LastMod: p.UpdatedAt.Time.Format("2006-01-02"),
		})
	}

	// People index
	sitemap.URLs = append(sitemap.URLs, sitemapURL{
		Loc: fmt.Sprintf("%s/t/%s/people", baseURL, tl.Slug),
	})

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		return
	}
	if err := xml.NewEncoder(w).Encode(sitemap); err != nil {
		// Response already started (headers + xml.Header written); record it.
		logger.Warn("encode sitemap", "err", err)
	}
}

// ---- Atom Feed ----

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	XMLNS   string      `xml:"xmlns,attr"`
	Title   string      `xml:"title"`
	Link    []atomLink  `xml:"link"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr,omitempty"`
}

type atomEntry struct {
	Title     string    `xml:"title"`
	Link      atomLink  `xml:"link"`
	ID        string    `xml:"id"`
	Published string    `xml:"published"`
	Updated   string    `xml:"updated"`
	Summary   *atomText `xml:"summary,omitempty"`
	Content   *atomText `xml:"content,omitempty"`
	Category  []atomCat `xml:"category,omitempty"`
}

type atomText struct {
	Type string `xml:"type,attr"`
	Body string `xml:",chardata"`
}

type atomCat struct {
	Term string `xml:"term,attr"`
}

func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	logger := s.logger(r)
	slug := r.PathValue("slug")
	ctx := r.Context()

	tl, err := s.store.GetTimeline(ctx, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.canViewTimeline(r.Context(), tl.IsPublic) {
		http.NotFound(w, r)
		return
	}

	baseURL := s.baseURL
	if baseURL == "" {
		baseURL = "http://" + r.Host
	}

	// Get the 50 most recently added events (by created_at, not event_date)
	events, err := s.store.ListEventsAfterDate(ctx, db.ListEventsAfterDateParams{
		TimelineID:     tl.ID,
		AfterDate:      pgtype.Date{Time: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		AfterSortOrder: -1,
		MaxResults:     50,
	})
	if err != nil {
		logger.Error("feed events", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	feedURL := fmt.Sprintf("%s/t/%s/feed.atom", baseURL, tl.Slug)
	tlURL := fmt.Sprintf("%s/t/%s", baseURL, tl.Slug)

	updated := tl.UpdatedAt.Time.Format(time.RFC3339)
	if len(events) > 0 {
		updated = events[0].CreatedAt.Time.Format(time.RFC3339)
	}

	feed := atomFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   tl.Name,
		ID:      tlURL,
		Updated: updated,
		Link: []atomLink{
			{Rel: "self", Href: feedURL, Type: "application/atom+xml"},
			{Rel: "alternate", Href: tlURL, Type: "text/html"},
		},
	}

	for _, e := range events {
		eventURL := fmt.Sprintf("%s/t/%s/event/%s", baseURL, tl.Slug, e.Slug)
		entry := atomEntry{
			Title:     e.Title,
			Link:      atomLink{Href: eventURL},
			ID:        eventURL,
			Published: e.CreatedAt.Time.Format(time.RFC3339),
			Updated:   e.UpdatedAt.Time.Format(time.RFC3339),
		}
		if e.Summary != "" {
			entry.Summary = &atomText{Type: "text", Body: e.Summary}
		}
		if e.CategoryName.Valid {
			entry.Category = []atomCat{{Term: e.CategoryName.String}}
		}
		feed.Entries = append(feed.Entries, entry)
	}

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		return
	}
	if err := xml.NewEncoder(w).Encode(feed); err != nil {
		// Response already started (headers + xml.Header written); record it.
		logger.Warn("encode feed", "err", err)
	}
}
