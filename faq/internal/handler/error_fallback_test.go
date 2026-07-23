package handler_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	uiassets "github.com/infodancer/ui"
	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/faquser"
	"github.com/infodancer/web/faq/internal/handler"
	"github.com/infodancer/web/faq/internal/render"
	"github.com/infodancer/web/faq/store"
)

// nopResolver resolves every request anonymously; the not-found path never
// consults it, but Deps requires a resolver. Named to avoid colliding with
// the integration-tagged anonResolver in main_test.go.
type nopResolver struct{}

func (nopResolver) Resolve(_ context.Context, _ *http.Request) (*faquser.User, error) {
	return nil, nil
}

func nopLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fallbackTemplates builds a template set that deliberately lacks the
// pages/not_found.gohtml and pages/error.gohtml pages, so the handler's
// error helpers take their fallback path. This models a host that mounts faq
// without shipping the optional error pages.
func fallbackTemplates(t *testing.T) *render.Templates {
	t.Helper()
	fsys := fstest.MapFS{
		"templates/base.gohtml":            {Data: []byte(`<html><body>{{block "content" .}}{{end}}</body></html>`)},
		"templates/pages/directory.gohtml": {Data: []byte(`{{define "content"}}dir{{end}}`)},
	}
	tpls, err := render.New(fsys, nil, uiassets.PartialsFS(), nopLogger())
	require.NoError(t, err)
	require.False(t, tpls.Has("pages/not_found.gohtml"), "fallback templates must omit not_found")
	return tpls
}

// Without the module's own not_found page, a page-level 404 still renders a
// chrome-wrapped document (via ui.RenderError) rather than a bare text/plain
// "not found". A GET to /q/{id} with a non-numeric id reaches notFound through
// pathID before any store access, so a nil-pool store is fine here: "q" is a
// reserved slug (dispatch skips the subsite DB lookup) and pathID 404s before
// detail queries anything.
func TestNotFound_FallsBackToChromeError(t *testing.T) {
	srv, err := handler.New(handler.Deps{
		Store:     store.NewStore(nil),
		Templates: fallbackTemplates(t),
		Resolver:  nopResolver{},
		Logger:    nopLogger(),
		BasePath:  "/faq",
		SiteName:  "Help",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/q/not-a-number", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
	body := strings.ToLower(rec.Body.String())
	require.Contains(t, body, "<!doctype html",
		"page-level 404 fallback should be a chrome-wrapped document, not text/plain")
}
