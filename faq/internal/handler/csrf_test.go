package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/internal/handler"
)

// postWithOrigin builds a request that hits the existing /q route via
// POST (which doesn't yet have a real implementation in M4.2 — the
// route table will accept it as 404 / 405 once write handlers land).
// For the CSRF guard tests we don't care about the underlying status;
// we care whether the guard short-circuited with 403.
func postWithOrigin(target, origin string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(""))
	r.Host = "example.com"
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func TestCSRFGetBypasses(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q", nil)
	req.Host = "example.com"
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "GET must bypass csrf guard")
}

func TestCSRFPostSameOriginAccepted(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postWithOrigin("/q", "https://example.com"))
	// Guard passes; the handler itself returns 404/405 because there's
	// no POST handler at /q in M4.2 yet. Any non-403 means the guard
	// let it through.
	require.NotEqual(t, http.StatusForbidden, rec.Code)
}

func TestCSRFPostCrossOriginRejected(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postWithOrigin("/q", "https://evil.example.net"))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "cross-origin")
}

func TestCSRFPostMissingOriginAndReferer(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodPost, "/q", strings.NewReader(""))
	rq.Host = "example.com"
	srv.Routes().ServeHTTP(rec, rq)
	require.Equal(t, http.StatusForbidden, rec.Code,
		"missing both Origin and Referer must fail closed")
}

func TestCSRFPostNullOrigin(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postWithOrigin("/q", "null"))
	require.Equal(t, http.StatusForbidden, rec.Code,
		`Origin="null" (opaque-origin context) must fail closed`)
}

func TestCSRFPostRefererFallback(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodPost, "/q", strings.NewReader(""))
	rq.Host = "example.com"
	rq.Header.Set("Referer", "https://example.com/some/page")
	srv.Routes().ServeHTTP(rec, rq)
	require.NotEqual(t, http.StatusForbidden, rec.Code,
		"Referer fallback must be honored when Origin is absent")
}

func TestCSRFPostRefererCrossOriginRejected(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodPost, "/q", strings.NewReader(""))
	rq.Host = "example.com"
	rq.Header.Set("Referer", "https://evil.example.net/some/page")
	srv.Routes().ServeHTTP(rec, rq)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCSRFDisabledBypasses(t *testing.T) {
	srv, _ := freshServer(t, func(d *handler.Deps) {
		d.CSRFDisabled = true
	})
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postWithOrigin("/q", "https://evil.example.net"))
	// Even cross-origin POST passes the guard when disabled. Handler
	// itself may still 404/405; the only thing we assert is not-403.
	require.NotEqual(t, http.StatusForbidden, rec.Code)
}
