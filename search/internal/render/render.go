// Package render loads and executes the embedded html/template set, overlaying
// optional host-supplied overrides and parsing the infodancer/ui partials
// (ui/nav, ui/footer, ui/sidebar) into every page tree so base.gohtml can
// render the shared chrome. Modeled on faq/internal/render.
package render

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"sort"
	"strings"
)

// Templates is the loaded, parsed template set. Construct one with [New] at
// startup and reuse across requests; it is safe for concurrent reads.
type Templates struct {
	// pages maps a page's logical name ("pages/search.gohtml") to a tree
	// containing base.gohtml, the page, every partials/*.gohtml, and the ui
	// partials. ExecuteTemplate(w, "base", data) renders the full page;
	// ExecuteTemplate(w, "partials/x.gohtml", data) renders a fragment.
	pages map[string]*template.Template

	// partialIndex is any one parsed tree, used for partial-only renders.
	partialIndex *template.Template

	log *slog.Logger
}

// New parses the embedded template set, overlays any same-named templates in
// override (which may be nil), and parses the uiPartials fs (also nilable) into
// every page tree. Returns an error if base.gohtml is missing.
func New(embedded, override, uiPartials fs.FS, log *slog.Logger) (*Templates, error) {
	if log == nil {
		log = slog.Default()
	}
	if embedded == nil {
		return nil, fmt.Errorf("render: embedded fs is required")
	}

	names, err := walkTemplates(embedded)
	if err != nil {
		return nil, fmt.Errorf("render: walk embedded: %w", err)
	}

	sources := make(map[string]string, len(names))
	for _, name := range names {
		src, err := readOverlay(name, embedded, override)
		if err != nil {
			return nil, fmt.Errorf("render: read %s: %w", name, err)
		}
		sources[name] = src
	}

	baseSrc, ok := sources["base.gohtml"]
	if !ok {
		return nil, fmt.Errorf("render: base.gohtml missing")
	}

	uiSources, err := readUIPartials(uiPartials)
	if err != nil {
		return nil, fmt.Errorf("render: ui partials: %w", err)
	}

	var partialNames []string
	for n := range sources {
		if strings.HasPrefix(n, "partials/") {
			partialNames = append(partialNames, n)
		}
	}
	sort.Strings(partialNames)

	t := &Templates{pages: map[string]*template.Template{}, log: log}
	funcs := defaultFuncs()

	parsePartials := func(tpl *template.Template, page string) error {
		for _, pn := range partialNames {
			if _, err := tpl.New(pn).Parse(sources[pn]); err != nil {
				return fmt.Errorf("render: parse partial %s for %s: %w", pn, page, err)
			}
		}
		// ui partials define their own template names (ui/nav, ...), so parse
		// the raw source; the {{define}} blocks register under those names.
		for name, src := range uiSources {
			if _, err := tpl.New("__ui__/" + name).Parse(src); err != nil {
				return fmt.Errorf("render: parse ui partial %s for %s: %w", name, page, err)
			}
		}
		return nil
	}

	for n, src := range sources {
		if n == "base.gohtml" || strings.HasPrefix(n, "partials/") {
			continue
		}
		tpl := template.New("base").Funcs(funcs)
		if _, err := tpl.Parse(baseSrc); err != nil {
			return nil, fmt.Errorf("render: parse base for %s: %w", n, err)
		}
		// Parse the partials (module partials and the ui partials, including
		// ui/document) BEFORE the page, so a page's {{define "content"}} /
		// {{define "title"}} / {{define "head"}} overrides ui/document's
		// same-named {{block}} defaults rather than being clobbered by them. The
		// page must be the last source parsed for any block name it redefines.
		if err := parsePartials(tpl, n); err != nil {
			return nil, err
		}
		if _, err := tpl.New(n).Parse(src); err != nil {
			return nil, fmt.Errorf("render: parse %s: %w", n, err)
		}
		t.pages[n] = tpl
		t.partialIndex = tpl
	}

	if t.partialIndex == nil {
		return nil, fmt.Errorf("render: no page templates found")
	}
	return t, nil
}

// Render executes the named template against data and writes HTML to w. Pages
// render through base.gohtml; names with the "partials/" prefix render plain.
// On failure it logs and writes a 500; the caller must not write to w after.
func (t *Templates) Render(w http.ResponseWriter, name string, data any) {
	var tpl *template.Template
	var target string

	if strings.HasPrefix(name, "partials/") {
		tpl = t.partialIndex
		if tpl == nil || tpl.Lookup(name) == nil {
			t.log.Error("render: partial not found", "name", name)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		target = name
	} else {
		var ok bool
		tpl, ok = t.pages[name]
		if !ok {
			t.log.Error("render: page not found", "name", name)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		target = "base"
	}

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, target, data); err != nil {
		t.log.Error("render: execute", "name", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		t.log.Warn("render: write", "name", name, "err", err)
	}
}

// Has reports whether the named template is registered.
func (t *Templates) Has(name string) bool {
	if strings.HasPrefix(name, "partials/") {
		return t.partialIndex != nil && t.partialIndex.Lookup(name) != nil
	}
	_, ok := t.pages[name]
	return ok
}

func walkTemplates(root fs.FS) ([]string, error) {
	var out []string
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".gohtml") {
			return nil
		}
		name, _ := strings.CutPrefix(p, "templates/")
		out = append(out, name)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func readOverlay(name string, embedded, override fs.FS) (string, error) {
	if override != nil {
		if b, err := fs.ReadFile(override, name); err == nil {
			return string(b), nil
		}
	}
	b, err := fs.ReadFile(embedded, path.Join("templates", name))
	if err != nil {
		if b2, err2 := fs.ReadFile(embedded, name); err2 == nil {
			return string(b2), nil
		}
		return "", err
	}
	return string(b), nil
}

// readUIPartials reads every *.gohtml from the ui partials fs. Returns an empty
// map when uiPartials is nil (the standalone-without-chrome path).
func readUIPartials(uiPartials fs.FS) (map[string]string, error) {
	out := map[string]string{}
	if uiPartials == nil {
		return out, nil
	}
	entries, err := fs.Glob(uiPartials, "*.gohtml")
	if err != nil {
		return nil, err
	}
	for _, name := range entries {
		b, err := fs.ReadFile(uiPartials, name)
		if err != nil {
			return nil, err
		}
		out[name] = string(b)
	}
	return out, nil
}

func defaultFuncs() template.FuncMap {
	return template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
	}
}
