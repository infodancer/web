// Package render loads timeline's template set and renders pages through
// infodancer/ui's Layer 2 ui/document, so chrome (nav, footer, meta, CSS,
// theme, sidebars) is owned by ui and skinnable per request by the host. It
// mirrors the faq module's render package.
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
	"time"
)

// Templates is the loaded, parsed template set. Construct once at startup with
// [New] and reuse across requests; it is safe for concurrent reads.
type Templates struct {
	pages        map[string]*template.Template
	partialIndex *template.Template
	log          *slog.Logger
}

// New parses the embedded template set and overlays any same-named templates in
// override (nilable). uiPartials (nilable) supplies the infodancer/ui partials
// (ui/document, ui/nav, ui/footer, ui/meta, ui/analytics, ui/sidebar), parsed
// into every page tree so base.gohtml can render through ui/document.
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

	t := &Templates{pages: map[string]*template.Template{}, log: log}
	funcs := defaultFuncs()

	// Parse order per page tree: base, then ui partials (ui/document and
	// friends define block defaults), then timeline's own partials, then the
	// page LAST so its {{define "content"}} wins over earlier block defaults.
	parseInto := func(tpl *template.Template, page, src string) error {
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

// Render executes the named template against data and writes the result. Pages
// (names without the "partials/" prefix) render through base.gohtml; partials
// render plain. On failure it logs and writes a 500.
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

func readOverlay(name string, embedded fs.FS, override fs.FS) (string, error) {
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

// formatDate renders a date according to precision (year / month / day).
func formatDate(t time.Time, precision string) string {
	switch precision {
	case "year":
		return t.Format("2006")
	case "month":
		return t.Format("January 2006")
	default:
		return t.Format("January 2, 2006")
	}
}

func defaultFuncs() template.FuncMap {
	return template.FuncMap{
		"formatDate": formatDate,
		"markdown":   Markdown,
	}
}
