package handler

import (
	"net/http"

	"github.com/infodancer/web/blog/internal/feeds"
	"github.com/infodancer/web/blog/store"
)

type feedScope int

const (
	feedScopeAll feedScope = iota
	feedScopeAuthor
	feedScopeBlog
)

// feedHandler builds one (scope, format) feed endpoint. format is "rss",
// "atom", or "json".
func (s *Server) feedHandler(scope feedScope, format string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			posts []store.Post
			err   error
			feed  feeds.Feed
		)

		switch scope {
		case feedScopeAll:
			posts, _, err = s.store.ListPublished(r.Context(), s.feedSize, 0)
			feed.Title = s.siteName
			feed.Description = s.siteDesc
			feed.SiteURL = s.absURL("/")
			feed.FeedURL = s.absURL("/feed." + feedExt(format))

		case feedScopeAuthor:
			a, aerr := s.store.GetAuthorBySlug(r.Context(), r.PathValue("slug"))
			if aerr != nil {
				s.notFoundOr(w, "author feed", aerr)
				return
			}
			posts, _, err = s.store.ListPublishedByAuthor(r.Context(), a.Slug, s.feedSize, 0)
			feed.Title = s.siteName + " — " + a.DisplayName
			feed.Description = "Posts by " + a.DisplayName
			feed.SiteURL = s.absURL("/authors/" + a.Slug)
			feed.FeedURL = s.absURL("/authors/" + a.Slug + "/feed." + feedExt(format))

		case feedScopeBlog:
			b, berr := s.store.GetBlogBySlug(r.Context(), r.PathValue("slug"))
			if berr != nil {
				s.notFoundOr(w, "blog feed", berr)
				return
			}
			posts, _, err = s.store.ListPublishedByBlog(r.Context(), b.Slug, s.feedSize, 0)
			feed.Title = s.siteName + " — " + b.Name
			feed.Description = b.Description
			feed.SiteURL = s.absURL("/b/" + b.Slug)
			feed.FeedURL = s.absURL("/b/" + b.Slug + "/feed." + feedExt(format))
		}
		if err != nil {
			s.serverError(w, "feed", err)
			return
		}

		ids := make([]int64, 0, len(posts))
		for _, p := range posts {
			ids = append(ids, p.ID)
		}
		tagsByPost, err := s.store.TagsForPosts(r.Context(), ids)
		if err != nil {
			s.serverError(w, "feed tags", err)
			return
		}

		for _, p := range posts {
			var tagNames []string
			for _, t := range tagsByPost[p.ID] {
				tagNames = append(tagNames, t.Name)
			}
			item := feeds.Item{
				Tags:        tagNames,
				Title:       p.Title,
				URL:         s.absURL("/p/" + p.Slug),
				Summary:     displayExcerpt(p),
				ContentHTML: p.BodyHTML,
				AuthorName:  p.AuthorName,
				AuthorURL:   s.absURL("/authors/" + p.AuthorSlug),
				Updated:     p.UpdatedAt,
			}
			if p.PublishedAt != nil {
				item.Published = *p.PublishedAt
			}
			if feed.Updated.IsZero() || p.UpdatedAt.After(feed.Updated) {
				feed.Updated = p.UpdatedAt
			}
			feed.Items = append(feed.Items, item)
		}

		var (
			out         []byte
			contentType string
		)
		switch format {
		case "rss":
			out, err = feeds.RSS(feed)
			contentType = "application/rss+xml; charset=utf-8"
		case "atom":
			out, err = feeds.Atom(feed)
			contentType = "application/atom+xml; charset=utf-8"
		default:
			out, err = feeds.JSON(feed)
			contentType = "application/feed+json; charset=utf-8"
		}
		if err != nil {
			s.serverError(w, "feed encode", err)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(out)
	}
}

// feedExt maps a format to its URL extension ("rss" feeds live at
// /feed.xml by web convention).
func feedExt(format string) string {
	if format == "rss" {
		return "xml"
	}
	return format
}
