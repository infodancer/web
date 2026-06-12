package handler

import (
	"html/template"
	"net/http"

	ui "github.com/infodancer/ui"
	"github.com/infodancer/ui/mdedit"

	"github.com/infodancer/web/profile/profileuser"
	"github.com/infodancer/web/profile/store"
)

// BaseView is the envelope every full page template receives. The embedded
// ui.DocumentData carries the chrome (nav, footer, meta, CSS, theme,
// sidebars) that base.gohtml renders through ui/document -- host-supplied via
// Deps.Chrome or the module default. The remaining fields are per-page
// context the content/title blocks reference.
type BaseView struct {
	ui.DocumentData

	// BasePath is the module's mount point (e.g. "/profile"); templates
	// prefix internal links with it.
	BasePath string

	// User is the resolved subject, or nil for anonymous.
	User *profileuser.User

	// SignInURL is the host login URL, surfaced to anonymous visitors.
	SignInURL string

	// EditorHead carries mdedit's <head> tags on the edit page only.
	EditorHead template.HTML

	// Page is the page-specific payload.
	Page any
}

// baseView builds a [BaseView] from the request, deps, and a page payload.
func (s *Server) baseView(r *http.Request, page any) BaseView {
	user := profileuser.UserFromContext(r.Context())

	var doc ui.DocumentData
	if s.chrome != nil {
		doc = s.chrome(r)
	} else {
		doc = s.defaultChrome()
	}
	if doc.Meta.SiteName == "" {
		doc.Meta.SiteName = orProfiles(s.siteName)
	}
	// profile's own layout CSS (card, form rows) is required under any
	// chrome, so it rides in ExtraCSS after ui's base sheets.
	doc.ExtraCSS = append(doc.ExtraCSS, s.basePath+"/static/style.css")

	return BaseView{
		DocumentData: doc,
		BasePath:     s.basePath,
		User:         user,
		SignInURL:    s.signInURL,
		Page:         page,
	}
}

// defaultChrome is the standalone chrome used when no host Chrome hook is
// set: ui CSS served from the module's own /static/ui mount, and a brand
// nav/footer from SiteName/HomeURL.
func (s *Server) defaultChrome() ui.DocumentData {
	assetBase := s.basePath + "/static/ui"
	name := orProfiles(s.siteName)
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

func orProfiles(name string) string {
	if name == "" {
		return "Profiles"
	}
	return name
}

// editView is the page payload for the edit form.
type editView struct {
	Profile       *store.Profile
	HandleLocked  bool   // claimed and the viewer may not change it
	AvatarUploads bool   // an IngestAvatar is configured
	AvatarSrc     string // current picture: upload or identicon fallback
	Errors        []string
	Saved         bool
	LinkRows      []store.Link  // existing links padded with blank rows
	Sections      []sectionView // host sections with edit markup
	BioField      mdedit.Field  // the bio textarea, editor-enhanced
}

// profileView is the page payload for the public profile page.
type profileView struct {
	Profile   *store.Profile
	AvatarSrc string
	Sections  []sectionView // host sections with view markup
}

// sectionView is one host section resolved for a specific page: the markup
// for that page's mode (view or edit) plus the title. Sections whose markup
// is empty for the mode are dropped before this type is built.
type sectionView struct {
	Title string
	HTML  template.HTML
}

// hostSections resolves the host's Sections hook for one profile and keeps
// the blocks that carry markup for the requested mode. A nil hook yields
// nil — the templates render nothing.
func (s *Server) hostSections(r *http.Request, userKey string, edit bool) []sectionView {
	if s.sections == nil {
		return nil
	}
	var out []sectionView
	for _, sec := range s.sections(r.Context(), r, userKey) {
		html := sec.ViewHTML
		if edit {
			html = sec.EditHTML
		}
		if html == "" {
			continue
		}
		out = append(out, sectionView{Title: sec.Title, HTML: html})
	}
	return out
}

// cardView is the payload for the embeddable card partial.
type cardView struct {
	BasePath    string
	Handle      string
	DisplayName string
	AvatarSrc   string
	ProfileURL  string // empty when the profile has no handle (no public page)
}

// avatarSrc picks the profile's picture: the uploaded canonical URL when
// present, the deterministic identicon route when a handle exists, otherwise
// empty (the template omits the image).
func (s *Server) avatarSrc(p *store.Profile) string {
	if p.AvatarURL != "" {
		return p.AvatarURL
	}
	if p.Handle != "" {
		return s.basePath + "/avatar/" + p.Handle
	}
	return ""
}

// displayLabel is the card/page fallback chain for a profile's visible name.
func displayLabel(p *store.Profile) string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	if p.Handle != "" {
		return p.Handle
	}
	return "user"
}
