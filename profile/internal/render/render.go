// Package render loads profile's template set and renders pages through
// infodancer/ui's Layer 2 ui/document, so chrome (nav, footer, meta, CSS,
// theme, sidebars) is owned by ui and skinnable per request by the host. It
// mirrors the timeline and faq modules' render packages.
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

	"github.com/infodancer/ui/markdown"
)

// bioRenderer is the single sanitization pipeline for user-authored bio
// markdown: the untrusted-comment preset with safe-scheme links only
// (http/https/mailto, rel="nofollow"). Only its output may be cast to
// template.HTML.
var bioRenderer = markdown.New(markdown.Comment(markdown.LinkSafeSchemes))

// Bio renders untrusted bio markdown to sanitized HTML.
func Bio(md string) template.HTML {
	return bioRenderer.Render(md)
}

// Templates is the loaded, parsed template set. Construct once at startup
// with [New] and reuse across requests; it is safe for concurrent reads.
type Templates struct {
	pages        map[string]*template.Template
	partialIndex *template.Template
	log          *slog.Logger
}

// New parses the embedded template set. uiPartials (nilable) supplies the
// infodancer/ui partials (ui/document, ui/nav, ui/footer, ui/meta,
// ui/analytics, ui/sidebar), parsed into every page tree so base.gohtml can
// render through ui/document. mdeditPartials (nilable) supplies the
// ui/mdedit editor partials the edit form embeds for the bio field.
func New(embedded fs.FS, uiPartials fs.FS, mdeditPartials fs.FS, log *slog.Logger) (*Templates, error) {
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
		src, err := readSource(name, embedded)
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

	uiSources, err := readGlobPartials(uiPartials)
	if err != nil {
		return nil, fmt.Errorf("render: ui partials: %w", err)
	}
	mdeditSources, err := readGlobPartials(mdeditPartials)
	if err != nil {
		return nil, fmt.Errorf("render: mdedit partials: %w", err)
	}

	t := &Templates{pages: map[string]*template.Template{}, log: log}
	funcs := template.FuncMap{"bio": Bio}

	// Parse order per page tree: ui + mdedit partials first (their {{block}}
	// defaults, e.g. ui/document's empty "head", must lose to later
	// definitions), then base (which defines the shared "head"), then
	// profile's own partials, then the page LAST so its {{define "content"}}
	// wins over every earlier block default.
	parseInto := func(tpl *template.Template, page, src string) error {
		for name, s := range uiSources {
			if _, err := tpl.New("__ui__/" + name).Parse(s); err != nil {
				return fmt.Errorf("render: parse ui partial %s for %s: %w", name, page, err)
			}
		}
		for name, s := range mdeditSources {
			if _, err := tpl.New("__mdedit__/" + name).Parse(s); err != nil {
				return fmt.Errorf("render: parse mdedit partial %s for %s: %w", name, page, err)
			}
		}
		if _, err := tpl.Parse(baseSrc); err != nil {
			return fmt.Errorf("render: parse base for %s: %w", page, err)
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

// Render executes the named template against data and writes the result.
// Pages (names without the "partials/" prefix) render through base.gohtml;
// partials render plain. On failure it logs and writes a 500.
func (t *Templates) Render(w http.ResponseWriter, name string, data any) {
	out, err := t.Execute(name, data)
	if err != nil {
		t.log.Error("render", "name", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	if _, err := w.Write(out); err != nil {
		t.log.Warn("render: write", "name", name, "err", err)
	}
}

// Execute renders the named template against data and returns the bytes.
// It backs both [Templates.Render] and callers that embed fragments in other
// surfaces (the card renderer).
func (t *Templates) Execute(name string, data any) ([]byte, error) {
	var tpl *template.Template
	var target string

	if strings.HasPrefix(name, "partials/") {
		tpl = t.partialIndex
		if tpl == nil || tpl.Lookup(name) == nil {
			return nil, fmt.Errorf("render: partial %s not found", name)
		}
		target = name
	} else {
		var ok bool
		tpl, ok = t.pages[name]
		if !ok {
			return nil, fmt.Errorf("render: page %s not found", name)
		}
		target = "base"
	}

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, target, data); err != nil {
		return nil, fmt.Errorf("render: execute %s: %w", name, err)
	}
	return buf.Bytes(), nil
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

func readSource(name string, embedded fs.FS) (string, error) {
	b, err := fs.ReadFile(embedded, path.Join("templates", name))
	if err != nil {
		if b2, err2 := fs.ReadFile(embedded, name); err2 == nil {
			return string(b2), nil
		}
		return "", err
	}
	return string(b), nil
}

// readGlobPartials reads every top-level .gohtml in a partials FS (the shape
// ui.PartialsFS and mdedit.PartialsFS share). nil yields an empty set.
func readGlobPartials(partials fs.FS) (map[string]string, error) {
	out := map[string]string{}
	if partials == nil {
		return out, nil
	}
	entries, err := fs.Glob(partials, "*.gohtml")
	if err != nil {
		return nil, err
	}
	for _, name := range entries {
		b, err := fs.ReadFile(partials, name)
		if err != nil {
			return nil, err
		}
		out[name] = string(b)
	}
	return out, nil
}
