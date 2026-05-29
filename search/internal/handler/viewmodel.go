package handler

import (
	"fmt"
	"time"

	"github.com/infodancer/ui"

	"github.com/infodancer/web/search/internal/query"
)

// pageView is the data passed to base.gohtml and the search page/partial. One
// struct serves the full page and the HTMX fragment; the fragment template
// reads only the result-related fields.
type pageView struct {
	PageTitle string
	BasePath  string
	SiteName  string
	Nav       ui.NavData
	Footer    ui.FooterData

	// Query is the trimmed user query echoed back into the input.
	Query string
	// Searched is true once a non-blank query has been run, so the template
	// can distinguish "no query yet" from "query returned nothing".
	Searched bool
	Results  []query.Result
}

// newPageView builds the chrome (nav/footer) and base fields for a request.
func (s *Server) newPageView(title, q string, results []query.Result, searched bool) pageView {
	d := s.deps
	return pageView{
		PageTitle: title,
		BasePath:  d.BasePath,
		SiteName:  d.SiteName,
		Nav: ui.NavData{
			BrandText: d.SiteName,
			BrandURL:  d.HomeURL,
		},
		Footer: ui.FooterData{
			BrandText: d.SiteName,
			BrandURL:  d.HomeURL,
			Copyright: fmt.Sprintf("© %d %s", time.Now().Year(), d.SiteName),
		},
		Query:    q,
		Searched: searched,
		Results:  results,
	}
}
