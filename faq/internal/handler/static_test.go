package handler_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/internal/handler"
)

func TestStaticServesFaqCSS(t *testing.T) {
	srv, _ := freshServer(t, func(d *handler.Deps) {
		d.Static = os.DirFS("../../static")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/faq.css", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "var(--app-color-bg")
	ct := rec.Header().Get("Content-Type")
	require.Contains(t, ct, "text/css")
}

func TestStaticAbsentReturns404(t *testing.T) {
	// Server constructed without Static — /static path must 404 cleanly.
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/faq.css", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}
