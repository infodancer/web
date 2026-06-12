package handler

import (
	"errors"
	"html/template"
	"net/http"

	"github.com/infodancer/web/blog/bloguser"
	"github.com/infodancer/web/blog/store"
)

// handleIndex renders the combined index: most recent published posts
// across every named blog.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	posts, total, err := s.store.ListPublished(r.Context(), s.pageSize, (page-1)*s.pageSize)
	if err != nil {
		s.serverError(w, "index", err)
		return
	}
	blogs, err := s.store.ListBlogs(r.Context())
	if err != nil {
		s.serverError(w, "index blogs", err)
		return
	}

	view := s.baseView(r, indexView{
		Heading:     s.siteName,
		Description: s.siteDesc,
		Posts:       s.cards(posts),
		Pagination:  paginate(s.basePath+"/", page, s.pageSize, total),
		Blogs:       blogs,
	})
	view.Meta.Canonical = s.absURL("/")
	view.Meta.JSONLD = append(view.Meta.JSONLD, s.jsonldBlog(s.siteName, s.siteDesc, "/"))
	s.renderPage(w, r, "index.gohtml", http.StatusOK, view)
}

// handleBlogIndex renders one named blog's index.
func (s *Server) handleBlogIndex(w http.ResponseWriter, r *http.Request) {
	blogSlug := r.PathValue("slug")
	b, err := s.store.GetBlogBySlug(r.Context(), blogSlug)
	if err != nil {
		s.notFoundOr(w, "blog index", err)
		return
	}
	page := pageParam(r)
	posts, total, err := s.store.ListPublishedByBlog(r.Context(), blogSlug, s.pageSize, (page-1)*s.pageSize)
	if err != nil {
		s.serverError(w, "blog index", err)
		return
	}

	view := s.baseView(r, indexView{
		Heading:     b.Name,
		Description: b.Description,
		Blog:        b,
		Posts:       s.cards(posts),
		Pagination:  paginate(s.basePath+"/b/"+b.Slug, page, s.pageSize, total),
	})
	view.FeedLinks = s.feedLinks(b.Name, "", b.Slug)
	view.Meta.Canonical = s.absURL("/b/" + b.Slug)
	view.Meta.Title = b.Name
	view.Meta.JSONLD = append(view.Meta.JSONLD, s.jsonldBlog(b.Name, b.Description, "/b/"+b.Slug))
	s.renderPage(w, r, "index.gohtml", http.StatusOK, view)
}

// handlePost renders one post. Drafts and archived posts are visible only
// to their author and admins; everyone else sees 404, not 403, so the
// existence of unpublished work isn't leaked.
func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPostBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.notFoundOr(w, "post", err)
		return
	}
	user := bloguser.UserFromContext(r.Context())
	if p.Status != store.StatusPublished && !mayEditPost(user, p.AuthorKey) {
		http.NotFound(w, r)
		return
	}

	var card template.HTML
	if s.authorCard != nil {
		if c, err := s.authorCard(r.Context(), p.AuthorKey); err == nil {
			card = c
		}
	}

	view := s.baseView(r, postView{
		Post:       p,
		BodyHTML:   template.HTML(p.BodyHTML), //nolint:gosec // sanitized by ui/markdown at save time
		AuthorCard: card,
		CanEdit:    mayEditPost(user, p.AuthorKey),
		IsDraft:    p.Status != store.StatusPublished,
	})
	view.FeedLinks = s.feedLinks(p.BlogName, "", p.BlogSlug)
	view.Meta.Canonical = s.absURL("/p/" + p.Slug)
	view.Meta.Title = p.Title
	view.Meta.Description = displayExcerpt(*p)
	view.Meta.Type = "article"
	if p.Status == store.StatusPublished {
		view.Meta.JSONLD = append(view.Meta.JSONLD, s.jsonldPost(p))
	} else {
		view.Meta.Robots = "noindex"
	}
	s.renderPage(w, r, "post.gohtml", http.StatusOK, view)
}

// handleAuthors renders the author index (authors with published posts).
func (s *Server) handleAuthors(w http.ResponseWriter, r *http.Request) {
	authors, err := s.store.ListAuthors(r.Context())
	if err != nil {
		s.serverError(w, "authors", err)
		return
	}
	view := s.baseView(r, authorsView{Authors: authors})
	view.Meta.Canonical = s.absURL("/authors")
	s.renderPage(w, r, "authors.gohtml", http.StatusOK, view)
}

// handleAuthor renders one author's page with their published posts.
func (s *Server) handleAuthor(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetAuthorBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.notFoundOr(w, "author", err)
		return
	}
	page := pageParam(r)
	posts, total, err := s.store.ListPublishedByAuthor(r.Context(), a.Slug, s.pageSize, (page-1)*s.pageSize)
	if err != nil {
		s.serverError(w, "author posts", err)
		return
	}

	var info *AuthorInfo
	if s.authors != nil {
		if i, err := s.authors.Lookup(r.Context(), a.UserKey); err == nil {
			info = i
		} else {
			s.log.Warn("author directory lookup", "key", a.UserKey, "err", err)
		}
	}

	view := s.baseView(r, authorView{
		Author:     a,
		Info:       info,
		Posts:      s.cards(posts),
		Pagination: paginate(s.basePath+"/authors/"+a.Slug, page, s.pageSize, total),
	})
	view.FeedLinks = s.feedLinks(a.DisplayName, a.Slug, "")
	view.Meta.Canonical = s.absURL("/authors/" + a.Slug)
	view.Meta.Title = a.DisplayName
	view.Meta.JSONLD = append(view.Meta.JSONLD, s.jsonldAuthor(a, info))
	s.renderPage(w, r, "author.gohtml", http.StatusOK, view)
}

// handleTags renders the tag index.
func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListTags(r.Context())
	if err != nil {
		s.serverError(w, "tags", err)
		return
	}
	view := s.baseView(r, tagsView{Tags: tags})
	view.Meta.Canonical = s.absURL("/tags")
	s.renderPage(w, r, "tags.gohtml", http.StatusOK, view)
}

// handleTag renders one tag's page.
func (s *Server) handleTag(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTagBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.notFoundOr(w, "tag", err)
		return
	}
	page := pageParam(r)
	posts, total, err := s.store.ListPublishedByTag(r.Context(), t.Slug, s.pageSize, (page-1)*s.pageSize)
	if err != nil {
		s.serverError(w, "tag posts", err)
		return
	}
	view := s.baseView(r, tagView{
		Tag:        t,
		Posts:      s.cards(posts),
		Pagination: paginate(s.basePath+"/tags/"+t.Slug, page, s.pageSize, total),
	})
	view.Meta.Canonical = s.absURL("/tags/" + t.Slug)
	view.Meta.Title = t.Name
	s.renderPage(w, r, "tag.gohtml", http.StatusOK, view)
}

// notFoundOr writes 404 for ErrNotFound and 500 otherwise.
func (s *Server) notFoundOr(w http.ResponseWriter, what string, err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.serverError(w, what, err)
}

func (s *Server) serverError(w http.ResponseWriter, what string, err error) {
	s.log.Error(what, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
