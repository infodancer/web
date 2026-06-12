package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/infodancer/web/blog/internal/slug"
	"github.com/infodancer/web/blog/store"
)

// handleAdminBlogs renders the named-blog management page.
func (s *Server) handleAdminBlogs(w http.ResponseWriter, r *http.Request) {
	s.renderAdminBlogs(w, r, nil, http.StatusOK)
}

func (s *Server) renderAdminBlogs(w http.ResponseWriter, r *http.Request, formErrors []string, status int) {
	blogs, err := s.store.ListBlogs(r.Context())
	if err != nil {
		s.serverError(w, "admin blogs", err)
		return
	}
	view := s.baseView(r, adminBlogsView{Blogs: blogs, Errors: formErrors})
	view.Meta.Robots = "noindex"
	s.renderPage(w, r, "admin_blogs.gohtml", status, view)
}

// blogForm extracts and validates the named-blog form fields.
func blogForm(r *http.Request) (slugVal, name, description string, errs []string) {
	name = strings.TrimSpace(r.PostFormValue("name"))
	description = strings.TrimSpace(r.PostFormValue("description"))
	slugVal = slug.Normalize(strings.TrimSpace(r.PostFormValue("slug")))
	if name == "" {
		errs = append(errs, "a name is required")
	}
	if slugVal == "" {
		slugVal = slug.Normalize(name)
	}
	if slugVal == "" {
		errs = append(errs, "a slug is required (letters and digits)")
	}
	return slugVal, name, description, errs
}

// handleAdminBlogCreate creates a named blog.
func (s *Server) handleAdminBlogCreate(w http.ResponseWriter, r *http.Request) {
	slugVal, name, description, errs := blogForm(r)
	if len(errs) > 0 {
		s.renderAdminBlogs(w, r, errs, http.StatusUnprocessableEntity)
		return
	}
	if _, err := s.store.CreateBlog(r.Context(), slugVal, name, description); err != nil {
		if errors.Is(err, store.ErrSlugTaken) {
			s.renderAdminBlogs(w, r, []string{"that slug is already in use"}, http.StatusUnprocessableEntity)
			return
		}
		s.serverError(w, "create blog", err)
		return
	}
	http.Redirect(w, r, s.basePath+"/admin/blogs", http.StatusSeeOther)
}

// handleAdminBlogUpdate updates a named blog's slug, name, and description.
func (s *Server) handleAdminBlogUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	slugVal, name, description, errs := blogForm(r)
	if len(errs) > 0 {
		s.renderAdminBlogs(w, r, errs, http.StatusUnprocessableEntity)
		return
	}
	if _, err := s.store.UpdateBlog(r.Context(), id, slugVal, name, description); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, "not found", http.StatusNotFound)
		case errors.Is(err, store.ErrSlugTaken):
			s.renderAdminBlogs(w, r, []string{"that slug is already in use"}, http.StatusUnprocessableEntity)
		default:
			s.serverError(w, "update blog", err)
		}
		return
	}
	http.Redirect(w, r, s.basePath+"/admin/blogs", http.StatusSeeOther)
}

// handleAdminBlogDelete deletes a named blog, refusing while posts still
// reference it.
func (s *Server) handleAdminBlogDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := s.store.DeleteBlog(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, "not found", http.StatusNotFound)
		case errors.Is(err, store.ErrBlogInUse):
			s.renderAdminBlogs(w, r,
				[]string{"that blog still has posts; reassign or delete them first"},
				http.StatusUnprocessableEntity)
		default:
			s.serverError(w, "delete blog", err)
		}
		return
	}
	http.Redirect(w, r, s.basePath+"/admin/blogs", http.StatusSeeOther)
}
