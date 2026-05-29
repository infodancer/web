package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/infodancer/web/search/internal/query"
	"github.com/infodancer/web/search/internal/render"
)

// fakeEngine returns canned results (or an error) regardless of query.
type fakeEngine struct {
	results []query.Result
	err     error
	lastQ   string
}

func (f *fakeEngine) Search(_ context.Context, q string, _ int) ([]query.Result, error) {
	f.lastQ = q
	return f.results, f.err
}

// testTemplates builds a minimal template set exercising the handler's data
// contract without depending on the infodancer/ui partials.
func testTemplates(t *testing.T) *render.Templates {
	t.Helper()
	fsys := fstest.MapFS{
		"base.gohtml":             {Data: []byte(`<html><body>{{block "content" .}}{{end}}</body></html>`)},
		"pages/search.gohtml":     {Data: []byte(`{{define "content"}}PAGE q=[{{.Query}}] searched={{.Searched}} n={{len .Results}}{{range .Results}} title=[{{.Title}}]{{end}}{{template "partials/results.gohtml" .}}{{end}}`)},
		"partials/results.gohtml": {Data: []byte(`FRAG q=[{{.Query}}] searched={{.Searched}} n={{len .Results}}{{range .Results}} title=[{{.Title}}]{{end}}`)},
	}
	tpls, err := render.New(fsys, nil, nil, nil)
	if err != nil {
		t.Fatalf("render.New: %v", err)
	}
	return tpls
}

func newTestServer(t *testing.T, eng Searcher) *Server {
	t.Helper()
	s, err := New(Deps{Engine: eng, Templates: testTemplates(t), BasePath: "/search"})
	if err != nil {
		t.Fatalf("handler.New: %v", err)
	}
	return s
}

func do(t *testing.T, s *Server, method, target string, htmx bool) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	res := rec.Result()
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	return res.StatusCode, string(body)
}

func TestRoot_NoQuery_RendersShellNotSearched(t *testing.T) {
	s := newTestServer(t, &fakeEngine{})
	code, body := do(t, s, http.MethodGet, "/", false)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(body, "PAGE q=[]") || !strings.Contains(body, "searched=false") {
		t.Errorf("unexpected shell body: %q", body)
	}
}

func TestRoot_WithQuery_RendersResultsInline(t *testing.T) {
	eng := &fakeEngine{results: []query.Result{{Title: "Alpha"}, {Title: "Beta"}}}
	s := newTestServer(t, eng)
	code, body := do(t, s, http.MethodGet, "/?q=foo", false)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if eng.lastQ != "foo" {
		t.Errorf("engine got q=%q, want foo", eng.lastQ)
	}
	if !strings.Contains(body, "n=2") || !strings.Contains(body, "title=[Alpha]") {
		t.Errorf("results not rendered inline: %q", body)
	}
}

func TestSearchFrag_HTMX_RendersPartialOnly(t *testing.T) {
	eng := &fakeEngine{results: []query.Result{{Title: "Gamma"}}}
	s := newTestServer(t, eng)
	code, body := do(t, s, http.MethodGet, "/search?q=bar", true)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.HasPrefix(body, "FRAG ") {
		t.Errorf("htmx request should get the bare fragment, got: %q", body)
	}
	if strings.Contains(body, "<html>") {
		t.Errorf("fragment should not include the page chrome: %q", body)
	}
}

func TestSearchFrag_NonHTMX_FallsBackToFullPage(t *testing.T) {
	s := newTestServer(t, &fakeEngine{})
	_, body := do(t, s, http.MethodGet, "/search?q=baz", false)
	if !strings.Contains(body, "<html>") {
		t.Errorf("non-htmx /search should render the full page: %q", body)
	}
}

func TestSearch_EngineError_Returns500(t *testing.T) {
	s := newTestServer(t, &fakeEngine{err: errors.New("boom")})
	code, _ := do(t, s, http.MethodGet, "/?q=foo", false)
	if code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", code)
	}
}

func TestHealthz(t *testing.T) {
	s := newTestServer(t, &fakeEngine{})
	code, body := do(t, s, http.MethodGet, "/healthz", false)
	if code != http.StatusOK || !strings.Contains(body, "ok") {
		t.Errorf("healthz = %d %q", code, body)
	}
}

func TestUnknownPath_404(t *testing.T) {
	s := newTestServer(t, &fakeEngine{})
	code, _ := do(t, s, http.MethodGet, "/nope", false)
	if code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}
