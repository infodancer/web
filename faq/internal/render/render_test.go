package render

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// embeddedTestFS mirrors what `//go:embed templates/*.gohtml ...` would
// produce: files are rooted at "templates/...".
func embeddedTestFS() fstest.MapFS {
	return fstest.MapFS{
		"templates/base.gohtml": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><title>{{.Title}}</title>{{template "content" .}}`),
		},
		"templates/pages/hello.gohtml": &fstest.MapFile{
			Data: []byte(`{{define "content"}}<h1>Hello, {{.Name}}.</h1>{{template "partials/footer.gohtml" .}}{{end}}`),
		},
		"templates/partials/footer.gohtml": &fstest.MapFile{
			Data: []byte(`<footer>by {{.Name}}</footer>`),
		},
	}
}

func TestRenderPageWithLayout(t *testing.T) {
	tpls, err := New(embeddedTestFS(), nil, nil, discardLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	tpls.Render(rec, "pages/hello.gohtml", map[string]string{"Title": "T", "Name": "Alice"})
	body := rec.Body.String()
	if !strings.Contains(body, "<title>T</title>") {
		t.Errorf("layout missing: %q", body)
	}
	if !strings.Contains(body, "Hello, Alice.") {
		t.Errorf("page content missing: %q", body)
	}
	if !strings.Contains(body, "<footer>by Alice</footer>") {
		t.Errorf("partial reuse missing: %q", body)
	}
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("content-type not set: %q", rec.Header().Get("Content-Type"))
	}
}

func TestRenderPartialPlain(t *testing.T) {
	tpls, err := New(embeddedTestFS(), nil, nil, discardLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	tpls.Render(rec, "partials/footer.gohtml", map[string]string{"Name": "Bob"})
	body := rec.Body.String()
	if body != "<footer>by Bob</footer>" {
		t.Errorf("partial output wrong: %q", body)
	}
	if strings.Contains(body, "<!DOCTYPE") {
		t.Errorf("partial leaked the layout: %q", body)
	}
}

func TestRenderOverrideWins(t *testing.T) {
	override := fstest.MapFS{
		"pages/hello.gohtml": &fstest.MapFile{
			Data: []byte(`{{define "content"}}<h1>OVERRIDE {{.Name}}</h1>{{end}}`),
		},
	}
	tpls, err := New(embeddedTestFS(), override, nil, discardLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	tpls.Render(rec, "pages/hello.gohtml", map[string]string{"Title": "T", "Name": "Alice"})
	body := rec.Body.String()
	if !strings.Contains(body, "OVERRIDE Alice") {
		t.Errorf("override didn't take: %q", body)
	}
}

func TestRenderUnknownPage500s(t *testing.T) {
	tpls, err := New(embeddedTestFS(), nil, nil, discardLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	tpls.Render(rec, "pages/missing.gohtml", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("missing template should 500, got %d", rec.Code)
	}
}

func TestRenderHas(t *testing.T) {
	tpls, err := New(embeddedTestFS(), nil, nil, discardLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !tpls.Has("pages/hello.gohtml") {
		t.Error("Has(page) = false, want true")
	}
	if !tpls.Has("partials/footer.gohtml") {
		t.Error("Has(partial) = false, want true")
	}
	if tpls.Has("pages/missing.gohtml") {
		t.Error("Has(missing) = true, want false")
	}
}

func TestNewMissingBase(t *testing.T) {
	bad := fstest.MapFS{
		"templates/pages/hello.gohtml": &fstest.MapFile{Data: []byte(`{{define "content"}}x{{end}}`)},
	}
	if _, err := New(bad, nil, nil, discardLogger()); err == nil {
		t.Error("New with no base.gohtml should error")
	}
}

func TestFuncSafe(t *testing.T) {
	embedded := fstest.MapFS{
		"templates/base.gohtml": &fstest.MapFile{
			Data: []byte(`{{template "content" .}}`),
		},
		"templates/pages/raw.gohtml": &fstest.MapFile{
			Data: []byte(`{{define "content"}}{{.HTML | safe}}{{end}}`),
		},
	}
	tpls, err := New(embedded, nil, nil, discardLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	tpls.Render(rec, "pages/raw.gohtml", map[string]string{"HTML": "<em>x</em>"})
	if !strings.Contains(rec.Body.String(), "<em>x</em>") {
		t.Errorf("safe should pass HTML through, got: %q", rec.Body.String())
	}
}

func TestFuncSlug(t *testing.T) {
	embedded := fstest.MapFS{
		"templates/base.gohtml": &fstest.MapFile{
			Data: []byte(`{{template "content" .}}`),
		},
		"templates/pages/sluggy.gohtml": &fstest.MapFile{
			Data: []byte(`{{define "content"}}{{slug .Title}}{{end}}`),
		},
	}
	tpls, err := New(embedded, nil, nil, discardLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	tpls.Render(rec, "pages/sluggy.gohtml", map[string]string{"Title": "How does counterspell work?"})
	if got := strings.TrimSpace(rec.Body.String()); got != "how-does-counterspell-work" {
		t.Errorf("slug func output: %q", got)
	}
}
