package admin

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

// templates is the parsed template set: a base layout, page templates (each
// defining a "content" block), and partials. Construct once with newTemplates
// and reuse; it is safe for concurrent reads.
type templates struct {
	pages        map[string]*template.Template
	partialIndex *template.Template
	log          *slog.Logger
}

// newTemplates parses the embedded template set, overlaying any same-named file
// found in override (which may be nil). Pages render through base; partials
// render plain and are looked up by their "partials/<name>.gohtml" logical name.
func newTemplates(embedded, override fs.FS, log *slog.Logger) (*templates, error) {
	if log == nil {
		log = slog.Default()
	}
	if embedded == nil {
		return nil, fmt.Errorf("admin: embedded template fs is required")
	}

	names, err := walkTemplates(embedded)
	if err != nil {
		return nil, fmt.Errorf("admin: walk templates: %w", err)
	}

	sources := make(map[string]string, len(names))
	for _, name := range names {
		src, err := readOverlay(name, embedded, override)
		if err != nil {
			return nil, fmt.Errorf("admin: read %s: %w", name, err)
		}
		sources[name] = src
	}

	baseSrc, ok := sources["base.gohtml"]
	if !ok {
		return nil, fmt.Errorf("admin: base.gohtml missing")
	}

	var partialNames []string
	for n := range sources {
		if strings.HasPrefix(n, "partials/") {
			partialNames = append(partialNames, n)
		}
	}
	sort.Strings(partialNames)

	t := &templates{pages: map[string]*template.Template{}, log: log}
	funcs := defaultFuncs()

	for n, src := range sources {
		if n == "base.gohtml" || strings.HasPrefix(n, "partials/") {
			continue
		}
		tpl := template.New("base").Funcs(funcs)
		if _, err := tpl.Parse(baseSrc); err != nil {
			return nil, fmt.Errorf("admin: parse base for %s: %w", n, err)
		}
		if _, err := tpl.New(n).Parse(src); err != nil {
			return nil, fmt.Errorf("admin: parse %s: %w", n, err)
		}
		for _, pn := range partialNames {
			if _, err := tpl.New(pn).Parse(sources[pn]); err != nil {
				return nil, fmt.Errorf("admin: parse partial %s for %s: %w", pn, n, err)
			}
		}
		t.pages[n] = tpl
		t.partialIndex = tpl
	}

	if t.partialIndex == nil && len(partialNames) > 0 {
		tpl := template.New("__partials__").Funcs(funcs)
		for _, pn := range partialNames {
			if _, err := tpl.New(pn).Parse(sources[pn]); err != nil {
				return nil, fmt.Errorf("admin: parse partial %s: %w", pn, err)
			}
		}
		t.partialIndex = tpl
	}

	return t, nil
}

// render executes the named template against data and writes HTML to w. Pages
// render through base; partials ("partials/...") render plain. On failure it
// logs and writes a 500; the caller must not write to w afterwards.
func (t *templates) render(w http.ResponseWriter, name string, data any) {
	var tpl *template.Template
	var target string

	if strings.HasPrefix(name, "partials/") {
		tpl = t.partialIndex
		if tpl == nil || tpl.Lookup(name) == nil {
			t.log.Error("admin: partial not found", "name", name)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		target = name
	} else {
		var ok bool
		tpl, ok = t.pages[name]
		if !ok {
			t.log.Error("admin: page not found", "name", name)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		target = "base"
	}

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, target, data); err != nil {
		t.log.Error("admin: execute", "name", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		t.log.Warn("admin: write", "name", name, "err", err)
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

func defaultFuncs() template.FuncMap {
	return template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
	}
}
