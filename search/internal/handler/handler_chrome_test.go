package handler

import (
	"net/http"
	"os"
	"strings"
	"testing"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/search/internal/render"
)

// realTemplates builds the module's actual templates (base.gohtml +
// pages/search.gohtml + partials) against the real infodancer/ui partials, so
// these tests exercise the true ui/document render path — including the parse
// order that must let the page's content/title/head blocks win over
// ui/document's defaults. os.DirFS("../..") is rooted at the module root so the
// "templates/" tree resolves; PartialsFS supplies ui/document, ui/nav, etc.
func realTemplates(t *testing.T) *render.Templates {
	t.Helper()
	tpls, err := render.New(os.DirFS("../.."), nil, uiassets.PartialsFS(), nil)
	if err != nil {
		t.Fatalf("render.New(real): %v", err)
	}
	return tpls
}

// With no host Chrome hook, the page still renders a complete ui/document with
// the search form (content block not clobbered) and the SiteName-derived brand.
func TestRender_DefaultChrome(t *testing.T) {
	s, err := New(Deps{
		Engine:    &fakeEngine{},
		Templates: realTemplates(t),
		BasePath:  "/search",
		SiteName:  "Standalone Search",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	code, body := do(t, s, "GET", "/", false)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	// Full ui/document shell.
	if !strings.Contains(strings.ToLower(body), "<!doctype html") {
		t.Errorf("expected a full ui/document page, got:\n%s", body)
	}
	// Content block survived the parse order (the regression guard).
	if !strings.Contains(body, `class="app-search-form"`) || !strings.Contains(body, `name="q"`) {
		t.Errorf("search form missing — content block was clobbered:\n%s", body)
	}
	// Default chrome brand from SiteName.
	if !strings.Contains(body, "Standalone Search") {
		t.Errorf("default brand missing from nav/footer:\n%s", body)
	}
}

// A host Chrome hook supplies the full ui.DocumentData; the page renders the
// host's nav items and stylesheet, proving search owns content, not chrome.
func TestRender_HostChromeInjected(t *testing.T) {
	chrome := func(_ *http.Request) uiassets.DocumentData {
		return uiassets.DocumentData{
			AssetBase: "/static/ui",
			ExtraCSS:  []string{"/static/osg/css/site.css"},
			Meta:      uiassets.Meta{SiteName: "Old School Gamers"},
			Nav: uiassets.NavData{
				BrandText: "Old School Gamers",
				BrandURL:  "/",
				Items: []uiassets.MenuItem{
					{Key: "campaigns", Label: "HOSTCAMPAIGNS", URL: "/campaign/"},
				},
			},
			Footer: uiassets.FooterData{BrandText: "Old School Gamers"},
		}
	}
	s, err := New(Deps{
		Engine:    &fakeEngine{},
		Templates: realTemplates(t),
		BasePath:  "/search",
		SiteName:  "Search",
		Chrome:    chrome,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	code, body := do(t, s, "GET", "/", false)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(body, "HOSTCAMPAIGNS") {
		t.Errorf("host nav item missing — Chrome hook not applied:\n%s", body)
	}
	if !strings.Contains(body, "/static/osg/css/site.css") {
		t.Errorf("host ExtraCSS missing — Chrome hook not applied to <head>")
	}
	// The module's own content still renders.
	if !strings.Contains(body, `class="app-search-form"`) {
		t.Errorf("search form missing under host chrome")
	}
}
