package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/infodancer/ui"

	"github.com/infodancer/web/search/internal/query"
)

// pageView is the data passed to ui/document and the search page/partial. The
// embedded ui.DocumentData carries the page chrome (nav, footer, meta, asset
// links, analytics, theme) — host-supplied via Deps.Chrome or the module's
// default — and ui/document reads it through the promoted fields. The remaining
// fields are the search-specific content the page/partial templates render.
type pageView struct {
	ui.DocumentData

	BasePath string
	// Query is the trimmed user query echoed back into the input.
	Query string
	// Searched is true once a non-blank query has been run, so the template
	// can distinguish "no query yet" from "query returned nothing".
	Searched bool
	Results  []query.Result
}

// newPageView assembles the chrome and the search content for a request. When
// Deps.Chrome is set the host supplies the full document chrome (so search
// matches the surrounding site); otherwise the module builds a complete default
// from SiteName/HomeURL so a standalone `search serve` still renders fully.
func (s *Server) newPageView(r *http.Request, title, q string, results []query.Result, searched bool) pageView {
	d := s.deps
	doc := s.resolveChrome(r)
	doc.Meta.Title = title
	if doc.Meta.SiteName == "" {
		doc.Meta.SiteName = d.SiteName
	}
	return pageView{
		DocumentData: doc,
		BasePath:     d.BasePath,
		Query:        q,
		Searched:     searched,
		Results:      results,
	}
}

// resolveChrome returns the host-supplied document chrome for a request, or
// the module default when no Chrome hook is set. It is the single place both
// the page view and the error page pull chrome from, so an error page matches
// the pages around it.
func (s *Server) resolveChrome(r *http.Request) ui.DocumentData {
	if s.deps.Chrome != nil {
		return s.deps.Chrome(r)
	}
	return s.defaultChrome()
}

// renderError writes a page-level error through the host chrome (or the module
// default) via ui.RenderError, so a missing or failed *page* looks like the
// site around it instead of a bare text/plain error. It is for navigational
// HTML responses only -- htmx fragment and API/form errors keep their
// status-only responses. On a render failure it falls back to a plain error so
// the client still gets the status.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, title, msg string) {
	doc := s.resolveChrome(r)
	if doc.Meta.SiteName == "" {
		doc.Meta.SiteName = s.deps.SiteName
	}
	if err := ui.RenderError(w, ui.ErrorData{
		DocumentData: doc,
		Status:       status,
		Title:        title,
		Message:      msg,
	}); err != nil {
		s.deps.Logger.Error("render error page", "err", err)
		http.Error(w, title, status)
	}
}

// notFound is the module's chrome-wrapped 404 for unknown navigational paths.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound, "Not found", "The page you are looking for does not exist.")
}

// defaultChrome is the standalone chrome used when no host Chrome hook is set:
// the ui CSS + htmx served from the module's own static mount, and a minimal
// brand nav/footer from SiteName/HomeURL.
func (s *Server) defaultChrome() ui.DocumentData {
	d := s.deps
	assetBase := d.BasePath + "/static/ui"
	return ui.DocumentData{
		AssetBase: assetBase,
		HeadTags:  ui.HeadTags(assetBase),
		Meta:      ui.Meta{SiteName: d.SiteName},
		Nav: ui.NavData{
			BrandText: d.SiteName,
			BrandURL:  d.HomeURL,
		},
		Footer: ui.FooterData{
			BrandText: d.SiteName,
			BrandURL:  d.HomeURL,
			Copyright: fmt.Sprintf("© %d %s", time.Now().Year(), d.SiteName),
		},
	}
}
