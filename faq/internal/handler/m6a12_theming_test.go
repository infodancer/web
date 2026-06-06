//go:build integration

package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/internal/handler"
)

// faq renders every full page through infodancer/ui's ui/document. The host
// owns the page chrome via Deps.Chrome; absent a hook the module builds a
// complete default from SiteName/HomeURL and always overlays its own section
// nav (Tags / Search / Ask) as the left sidebar. These tests pin that contract
// (superseding the pre-ui/document overlay-partial theming hook).

func themeBody(t *testing.T, srv *handler.Server, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func themeHead(t *testing.T, body string) string {
	t.Helper()
	s := strings.Index(body, "<head>")
	e := strings.Index(body, "</head>")
	require.Greater(t, s, -1)
	require.Greater(t, e, s)
	return body[s:e]
}

// Default chrome (no host hook, no SiteName): a complete page with the ui nav +
// footer, the brand falling back to "FAQ", and the faq section sidebar.
func TestChrome_DefaultWithoutSiteName(t *testing.T) {
	srv, _ := freshServer(t)
	body := themeBody(t, srv, "/q")
	require.Contains(t, body, `class="app-nav"`, "ui nav rendered")
	require.Contains(t, body, `class="app-footer"`, "ui footer rendered")
	require.Contains(t, body, "FAQ", "brand falls back to FAQ")
	require.Contains(t, body, "app-sidebar-left", "faq section sidebar rendered")
	require.Contains(t, body, `href="/faq/tags"`)
	require.Contains(t, body, `href="/faq/search"`)
}

// Default chrome threads SiteName + HomeURL into the ui nav brand and footer.
func TestChrome_DefaultSiteNameThreaded(t *testing.T) {
	srv, _ := freshServer(t, func(d *handler.Deps) {
		d.SiteName = "TestSite"
		d.HomeURL = "/host-home"
	})
	body := themeBody(t, srv, "/q")
	require.Contains(t, body, "TestSite", "SiteName in nav/footer brand")
	require.Contains(t, body, `href="/host-home"`, "HomeURL in brand link")
}

// Self-hosted, SRI-pinned htmx renders in <head> via ui/document's HeadTags,
// and the module's own component CSS is linked by the faq head block.
func TestChrome_SelfHostedHTMX(t *testing.T) {
	srv, _ := freshServer(t)
	body := themeBody(t, srv, "/q")
	head := themeHead(t, body)
	require.Contains(t, head, "/faq/static/ui/js/htmx-", "self-hosted htmx in <head>")
	require.Contains(t, head, "integrity=", "htmx carries an SRI hash")
	require.NotContains(t, body, "unpkg", "no CDN reference")
	require.Contains(t, head, "/faq/static/faq.css", "faq component CSS linked in <head>")
}

// A host Chrome hook supplies the full document chrome; the FAQ wears the host
// nav + stylesheet, and the module's content + section sidebar still render.
func TestChrome_HostHookInjected(t *testing.T) {
	srv, _ := freshServer(t, func(d *handler.Deps) {
		d.Chrome = func(_ *http.Request) uiassets.DocumentData {
			return uiassets.DocumentData{
				AssetBase: "/static/ui",
				ExtraCSS:  []string{"/static/host/site.css"},
				Meta:      uiassets.Meta{SiteName: "HostSite"},
				Nav: uiassets.NavData{
					BrandText: "HostSite",
					Items: []uiassets.MenuItem{
						{Key: "campaigns", Label: "HOSTNAVITEM", URL: "/campaign/"},
					},
				},
				Footer: uiassets.FooterData{BrandText: "HostSite"},
			}
		}
	})
	body := themeBody(t, srv, "/q")
	require.Contains(t, body, "HOSTNAVITEM", "host nav item rendered — Chrome hook applied")
	require.Contains(t, body, "/static/host/site.css", "host ExtraCSS rendered in <head>")
	require.Contains(t, body, "app-sidebar-left", "faq section sidebar still rendered under host chrome")
	require.Contains(t, body, `href="/faq/tags"`)
}

// hostRightRail returns a Chrome hook whose right sidebar carries one host
// section ("Discover"/discover), so the placement tests can prove the merge.
func hostRightRail() func(*http.Request) uiassets.DocumentData {
	return func(_ *http.Request) uiassets.DocumentData {
		return uiassets.DocumentData{
			AssetBase: "/static/ui",
			Meta:      uiassets.Meta{SiteName: "HostSite"},
			SidebarRight: &uiassets.SidebarData{Sections: []uiassets.SidebarSection{
				{Key: "discover", Title: "Discover", Open: true, Items: []uiassets.SidebarItem{
					{Label: "HOSTRAILITEM", URL: "/campaign/"},
				}},
			}},
		}
	}
}

// SectionNavRight merges the FAQ section into the host's right rail: the page
// has no left sidebar, and the FAQ links and the host rail both live on the
// right.
func TestChrome_SectionNavRight(t *testing.T) {
	srv, _ := freshServer(t, func(d *handler.Deps) {
		d.Chrome = hostRightRail()
		d.SectionNavPlacement = "right"
	})
	body := themeBody(t, srv, "/q")
	require.NotContains(t, body, "app-sidebar-left", "no left sidebar when nav is placed right")
	require.Contains(t, body, "app-sidebar-right", "right rail rendered")
	require.Contains(t, body, "HOSTRAILITEM", "host's own right-rail section survives the merge")
	require.Contains(t, body, `href="/faq/tags"`, "FAQ section nav moved into the right rail")

	// The FAQ section is merged on top of the host's sections.
	require.Less(t, strings.Index(body, "/faq/tags"), strings.Index(body, "HOSTRAILITEM"),
		"FAQ section sits above the host's own right-rail section")
}

// SectionNavNone omits the module nav entirely — neither sidebar carries the
// FAQ section links, leaving the host to render that navigation itself.
func TestChrome_SectionNavNone(t *testing.T) {
	srv, _ := freshServer(t, func(d *handler.Deps) {
		d.Chrome = hostRightRail()
		d.SectionNavPlacement = "none"
	})
	body := themeBody(t, srv, "/q")
	require.NotContains(t, body, "app-sidebar-left", "no left sidebar when nav is omitted")
	require.NotContains(t, body, `href="/faq/tags"`, "module section nav omitted")
	require.Contains(t, body, "HOSTRAILITEM", "host's right rail is untouched")
}
