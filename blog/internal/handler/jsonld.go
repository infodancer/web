package handler

import (
	"html/template"
	"time"

	ui "github.com/infodancer/ui"

	"github.com/infodancer/web/blog/store"
)

// JSON-LD builders. Every payload is a typed map marshaled through
// ui.JSONLD (encoding/json escapes <,>,& so the script element cannot be
// broken out of); nothing is string-assembled.

// jsonldBlog describes a blog scope (the combined index or one named blog)
// as a schema.org Blog.
func (s *Server) jsonldBlog(name, description, rel string) template.HTML {
	doc := map[string]any{
		"@context":    "https://schema.org",
		"@type":       "Blog",
		"name":        name,
		"url":         s.absURL(rel),
		"description": description,
	}
	out, err := ui.JSONLD(doc)
	if err != nil {
		s.log.Warn("jsonld blog", "err", err)
		return ""
	}
	return out
}

// jsonldPost describes one published post as a schema.org BlogPosting.
func (s *Server) jsonldPost(p *store.Post) template.HTML {
	postURL := s.absURL("/p/" + p.Slug)
	doc := map[string]any{
		"@context":         "https://schema.org",
		"@type":            "BlogPosting",
		"headline":         p.Title,
		"url":              postURL,
		"mainEntityOfPage": map[string]any{"@type": "WebPage", "@id": postURL},
		"description":      displayExcerpt(*p),
		"dateModified":     p.UpdatedAt.UTC().Format(time.RFC3339),
		"author": map[string]any{
			"@type": "Person",
			"name":  p.AuthorName,
			"url":   s.absURL("/authors/" + p.AuthorSlug),
		},
	}
	if p.PublishedAt != nil {
		doc["datePublished"] = p.PublishedAt.UTC().Format(time.RFC3339)
	}
	if len(p.Tags) > 0 {
		keywords := make([]string, 0, len(p.Tags))
		for _, t := range p.Tags {
			keywords = append(keywords, t.Name)
		}
		doc["keywords"] = keywords
	}
	if p.BlogSlug != "" {
		doc["isPartOf"] = map[string]any{
			"@type": "Blog",
			"name":  p.BlogName,
			"url":   s.absURL("/b/" + p.BlogSlug),
		}
	}
	out, err := ui.JSONLD(doc)
	if err != nil {
		s.log.Warn("jsonld post", "err", err)
		return ""
	}
	return out
}

// jsonldAuthor describes an author page as a schema.org ProfilePage
// wrapping a Person.
func (s *Server) jsonldAuthor(a *store.Author, info *AuthorInfo) template.HTML {
	person := map[string]any{
		"@type": "Person",
		"name":  a.DisplayName,
		"url":   s.absURL("/authors/" + a.Slug),
	}
	if info != nil && info.AvatarURL != "" {
		person["image"] = info.AvatarURL
	}
	doc := map[string]any{
		"@context":   "https://schema.org",
		"@type":      "ProfilePage",
		"mainEntity": person,
	}
	out, err := ui.JSONLD(doc)
	if err != nil {
		s.log.Warn("jsonld author", "err", err)
		return ""
	}
	return out
}
