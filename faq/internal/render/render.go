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

	"github.com/infodancer/web/faq/internal/slug"
)

// Templates is the loaded, parsed template set. Construct one with [New]
// once at startup and reuse across requests — it is safe for concurrent
// reads.
type Templates struct {
	// pages maps a page template's logical name ("pages/q_list.gohtml")
	// to a *template.Template tree that contains: base.gohtml, the page
	// itself, and every partials/*.gohtml. ExecuteTemplate(w, "base",
	// data) renders the full page; ExecuteTemplate(w, "partials/x.gohtml",
	// data) renders a fragment.
	pages map[string]*template.Template

	// partialIndex is any one of the parsed page trees, used as a
	// fallback for partial-only renders (the partial is parsed into every
	// page, so picking any tree works).
	partialIndex *template.Template

	log *slog.Logger
}

// New parses the embedded template set and overlays any same-named
// templates found in override (which may be nil). uiPartials (also nilable)
// supplies the infodancer/ui partials — ui/document, ui/nav, ui/footer,
// ui/meta, ui/analytics, ui/sidebar — parsed into every page tree so
// base.gohtml can render through ui/document. Returns an error if a required
// template (base.gohtml, or any pages/*.gohtml) is missing.
func New(embedded fs.FS, override fs.FS, uiPartials fs.FS, log *slog.Logger) (*Templates, error) {
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

	// Read each template's source, preferring the override fs when it
	// has a same-named file.
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

	// Partial sources — parsed into every page tree.
	var partialNames []string
	for n := range sources {
		if strings.HasPrefix(n, "partials/") {
			partialNames = append(partialNames, n)
		}
	}
	sort.Strings(partialNames)

	uiSources, err := readUIPartials(uiPartials)
	if err != nil {
		return nil, fmt.Errorf("render: ui partials: %w", err)
	}

	t := &Templates{
		pages: map[string]*template.Template{},
		log:   log,
	}
	funcs := defaultFuncs()

	// Parse order per page tree: base, then the ui partials (ui/document and
	// friends define block defaults), then faq's own partials (whose chrome
	// partial overrides ui/document's title/head defaults), then the page LAST
	// so its {{define "content"}} wins over every earlier block default. Get
	// this order wrong and ui/document's empty content block blanks the page.
	parseInto := func(tpl *template.Template, page, src string) error {
		// ui partials define their own template names (ui/document, ...), so
		// parse the raw source; the {{define}} blocks register under those names.
		for name, s := range uiSources {
			if _, err := tpl.New("__ui__/" + name).Parse(s); err != nil {
				return fmt.Errorf("render: parse ui partial %s for %s: %w", name, page, err)
			}
		}
		for _, pn := range partialNames {
			if _, err := tpl.New(pn).Parse(sources[pn]); err != nil {
				return fmt.Errorf("render: parse partial %s for %s: %w", pn, page, err)
			}
		}
		if _, err := tpl.New(page).Parse(src); err != nil {
			return fmt.Errorf("render: parse %s: %w", page, err)
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
		if err := parseInto(tpl, n, src); err != nil {
			return nil, err
		}
		t.pages[n] = tpl
		t.partialIndex = tpl
	}

	// Allow partial-only renderers to find the partial set even if no
	// page templates exist (unlikely, but it's cheap to handle).
	if t.partialIndex == nil && len(partialNames) > 0 {
		tpl := template.New("__partials__").Funcs(funcs)
		for _, pn := range partialNames {
			if _, err := tpl.New(pn).Parse(sources[pn]); err != nil {
				return nil, fmt.Errorf("render: parse partial %s: %w", pn, err)
			}
		}
		t.partialIndex = tpl
	}

	return t, nil
}

// Render executes the named template against data and writes the resulting
// HTML to w. Pages (names without the "partials/" prefix) render through
// base.gohtml; partials render plain.
//
// On failure, Render logs the error and writes a 500 Internal Server
// Error. The handler should not write to w after calling Render.
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

// Has reports whether the named template is registered. Useful in handlers
// that branch on htmx vs full page rendering and want a safety check.
func (t *Templates) Has(name string) bool {
	if strings.HasPrefix(name, "partials/") {
		return t.partialIndex != nil && t.partialIndex.Lookup(name) != nil
	}
	_, ok := t.pages[name]
	return ok
}

// walkTemplates returns every *.gohtml file in root, with the leading
// "templates/" stripped so names are independent of the embed prefix.
// Names use forward slashes regardless of host OS.
func walkTemplates(root fs.FS) ([]string, error) {
	var out []string
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".gohtml") {
			return nil
		}
		// The embed root is usually "templates", but we strip any
		// leading "templates/" segment to allow caller flexibility.
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

// readOverlay returns the source of the named template, preferring
// override when it has a same-named file. Override is matched against the
// same logical name (no "templates/" prefix needed in the override fs).
func readOverlay(name string, embedded fs.FS, override fs.FS) (string, error) {
	if override != nil {
		if b, err := fs.ReadFile(override, name); err == nil {
			return string(b), nil
		}
	}
	// Embedded files live under "templates/<name>".
	b, err := fs.ReadFile(embedded, path.Join("templates", name))
	if err != nil {
		// Some embed.FS roots are already at "templates/" — try the
		// bare name as a fallback.
		if b2, err2 := fs.ReadFile(embedded, name); err2 == nil {
			return string(b2), nil
		}
		return "", err
	}
	return string(b), nil
}

// readUIPartials reads every *.gohtml from the infodancer/ui partials fs.
// Returns an empty map when uiPartials is nil (a standalone build with no ui
// chrome — base.gohtml must then not reference ui/document).
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

// defaultFuncs is the FuncMap available in every template.
func defaultFuncs() template.FuncMap {
	return template.FuncMap{
		// safe converts a string into template.HTML — used for body_html
		// fields, which have already been sanitized by bluemonday at
		// import / edit time.
		"safe": func(s string) template.HTML { return template.HTML(s) }, //nolint:gosec // intentional: input is sanitized upstream
		// slug normalizes a string for inclusion in a URL path segment.
		"slug": slug.Normalize,
		// add/sub are small integer math helpers used by the pager
		// templates ({{add .Page 1}} for "next", {{sub .Page 1}} for
		// "prev"). Kept tiny on purpose — bigger logic belongs in Go.
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
	}
}
