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
	var doc ui.DocumentData
	if d.Chrome != nil {
		doc = d.Chrome(r)
	} else {
		doc = s.defaultChrome()
	}
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
