package handler

import (
	"net/http"
	"strings"

	"github.com/infodancer/web/search/internal/query"
)

// searchPage serves the full search page shell (GET /). When a ?q= is present
// it also runs the query and renders results inline, so the page is useful
// without JavaScript and a /?q=… link is shareable.
func (s *Server) searchPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	results, searched, ok := s.run(w, r, q)
	if !ok {
		return // run already wrote an error response
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
	results, searched, ok := s.run(w, r, q)
	if !ok {
		return
	}
	view := s.newPageView(r, s.pageTitle(q), q, results, searched)
	s.deps.Templates.Render(w, "partials/results.gohtml", view)
}

// run executes the query when non-blank. ok is false when an error response has
// already been written and the caller must stop. searched reports whether a
// (non-blank) query was actually run.
func (s *Server) run(w http.ResponseWriter, r *http.Request, q string) (results []query.Result, searched, ok bool) {
	if q == "" {
		return nil, false, true
	}
	res, err := s.deps.Engine.Search(r.Context(), q, s.deps.PageSize)
	if err != nil {
		s.deps.Logger.Error("search failed", "q", q, "err", err)
		http.Error(w, "search failed", http.StatusInternalServerError)
		return nil, false, false
	}
	return res, true, true
}

func (s *Server) pageTitle(q string) string {
	if q == "" {
		return "Search"
	}
	return "Search: " + q
}
