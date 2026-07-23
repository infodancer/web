package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/infodancer/web/search/internal/query"
)

// searchPage serves the full search page shell (GET /). When a ?q= is present
// it also runs the query and renders results inline, so the page is useful
// without JavaScript and a /?q=… link is shareable.
func (s *Server) searchPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	results, searched, err := s.run(r.Context(), q)
	if err != nil {
		s.deps.Logger.Error("search failed", "q", q, "err", err)
		// A full-page request: render the error through the site chrome.
		s.renderError(w, r, http.StatusInternalServerError, "Search failed",
			"Something went wrong running your search. Please try again.")
		return
	}
	view := s.newPageView(r, s.pageTitle(q), q, results, searched)
	s.deps.Templates.Render(w, "pages/search.gohtml", view)
}

// searchFrag serves the HTMX results partial (GET /search). For a non-htmx
// request (a direct visit or shared link) it falls back to the full page so
// the URL still renders something sensible.
func (s *Server) searchFrag(w http.ResponseWriter, r *http.Request) {
	if !isHTMX(r) {
		s.searchPage(w, r)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	results, searched, err := s.run(r.Context(), q)
	if err != nil {
		s.deps.Logger.Error("search failed", "q", q, "err", err)
		// htmx fragment: an error here stays a status-only response, not a
		// full chrome-wrapped document swapped into the results target.
		http.Error(w, "search failed", http.StatusInternalServerError)
		return
	}
	view := s.newPageView(r, s.pageTitle(q), q, results, searched)
	s.deps.Templates.Render(w, "partials/results.gohtml", view)
}

// run executes the query when non-blank and returns the results. searched
// reports whether a (non-blank) query was actually run; the caller decides how
// to render an error, since a full page and an htmx fragment differ.
func (s *Server) run(ctx context.Context, q string) (results []query.Result, searched bool, err error) {
	if q == "" {
		return nil, false, nil
	}
	res, err := s.deps.Engine.Search(ctx, q, s.deps.PageSize)
	if err != nil {
		return nil, false, err
	}
	return res, true, nil
}

func (s *Server) pageTitle(q string) string {
	if q == "" {
		return "Search"
	}
	return "Search: " + q
}
