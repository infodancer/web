package handler

import (
	"net/http"

	ui "github.com/infodancer/ui"

	"github.com/infodancer/web/timeline/timelineuser"
)

// BaseView is the envelope every full page template receives. The embedded
// ui.DocumentData carries the chrome (nav, footer, meta, CSS, theme, sidebars)
// that base.gohtml renders through ui/document — host-supplied via Deps.Chrome
// or the module default. The remaining fields are per-page context the
// content/title/head blocks reference.
type BaseView struct {
	ui.DocumentData

	// BasePath is the module's mount point (e.g. "/timeline"); templates prefix
	// internal links with it.
	BasePath string

	// BaseURL is the absolute origin used for canonical/og links in page head
	// blocks; empty falls back to a relative URL.
	BaseURL string

	// User is the resolved subject, or nil for anonymous.
	User *timelineuser.User

	// SignInURL is the host login URL, surfaced to anonymous visitors.
	SignInURL string

	// Embed is true for ?embed=true requests; base.gohtml then renders a
	// chrome-less document for iframe embedding.
	Embed bool

	// Page is the page-specific payload.
	Page any
}

// baseView builds a [BaseView] from the request, deps, and a page payload.
func (s *Server) baseView(r *http.Request, page any) BaseView {
	user := timelineuser.UserFromContext(r.Context())

	var doc ui.DocumentData
	if s.chrome != nil {
		doc = s.chrome(r)
	} else {
		doc = s.defaultChrome()
	}
	if doc.Meta.SiteName == "" {
		doc.Meta.SiteName = orTimelines(s.siteName)
	}
	// timeline's own layout CSS (lanes, axis, cards) is required under any
	// chrome, so it rides in ExtraCSS after ui's base sheets.
	doc.ExtraCSS = append(doc.ExtraCSS, s.basePath+"/static/style.css")

	return BaseView{
		DocumentData: doc,
		BasePath:     s.basePath,
		BaseURL:      s.baseURL,
		User:         user,
		SignInURL:    s.signInURL,
		Embed:        r.URL.Query().Get("embed") == "true",
		Page:         page,
	}
}

// defaultChrome is the standalone chrome used when no host Chrome hook is set:
// ui CSS + htmx served from the module's own /static/ui mount, and a brand
// nav/footer from SiteName/HomeURL.
func (s *Server) defaultChrome() ui.DocumentData {
	assetBase := s.basePath + "/static/ui"
	name := orTimelines(s.siteName)
	home := s.homeURL
	if home == "" {
		home = "/"
	}
	return ui.DocumentData{
		AssetBase: assetBase,
		HeadTags:  ui.HeadTags(assetBase),
		Meta:      ui.Meta{SiteName: name},
		Nav:       ui.NavData{BrandText: name, BrandURL: home},
		Footer:    ui.FooterData{BrandText: name, BrandURL: home},
	}
}

func orTimelines(name string) string {
	if name == "" {
		return "Timelines"
	}
	return name
}
