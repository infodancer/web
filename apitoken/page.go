package apitoken

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/infodancer/ui"
)

//go:embed templates/*.gohtml
var templatesFS embed.FS

// pageData is what tokens.gohtml renders. ui.DocumentData is embedded so the
// page can hand itself straight to ui/document.
type pageData struct {
	ui.DocumentData
	Header   template.HTML
	BasePath string
	Prefix   string
	Tokens   []*Token
	Scopes   []Scope

	// NewToken is the plain text of a token just minted; set only on the
	// response to the mint POST, never stored or redirected.
	NewToken     string
	NewTokenName string
	Error        string
}

func parsePage() (*template.Template, error) {
	t := template.New("tokens").Funcs(template.FuncMap{
		"when": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.UTC().Format("2006-01-02 15:04 UTC")
		},
		"day":  func(t time.Time) string { return t.UTC().Format("2006-01-02") },
		"join": strings.Join,
	})
	partials := ui.PartialsFS()
	err := fs.WalkDir(partials, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".gohtml") {
			return err
		}
		src, err := fs.ReadFile(partials, path)
		if err != nil {
			return err
		}
		_, err = t.New("ui:" + path).Parse(string(src))
		return err
	})
	if err != nil {
		return nil, err
	}
	src, err := templatesFS.ReadFile("templates/tokens.gohtml")
	if err != nil {
		return nil, err
	}
	return t.New("page").Parse(string(src))
}

// Handler serves the account page at Config.BasePath: the user's tokens, a
// form to mint one, and a revoke button on each live one. Mount it at
// BasePath on the host's browser routes (never on the token [Routes]).
func (m *Module) Handler() (http.Handler, error) {
	if m.cfg.CurrentUser == nil {
		return nil, errors.New("apitoken: Handler needs Config.CurrentUser")
	}
	base := strings.TrimRight(m.cfg.BasePath, "/")
	if !strings.HasPrefix(base, "/") {
		return nil, errors.New("apitoken: Handler needs Config.BasePath, e.g. \"/account/tokens\"")
	}
	tpl, err := parsePage()
	if err != nil {
		return nil, err
	}
	p := &page{m: m, tpl: tpl, base: base}

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+base, p.list)
	mux.HandleFunc("POST "+base, p.mint)
	mux.HandleFunc("POST "+base+"/{id}/revoke", p.revoke)
	return p.csrf(mux), nil
}

type page struct {
	m    *Module
	tpl  *template.Template
	base string
}

func (p *page) list(w http.ResponseWriter, r *http.Request) {
	user, ok := p.m.cfg.CurrentUser(w, r)
	if !ok {
		return
	}
	p.render(w, r, user, http.StatusOK, pageData{})
}

func (p *page) mint(w http.ResponseWriter, r *http.Request) {
	user, ok := p.m.cfg.CurrentUser(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		p.render(w, r, user, http.StatusBadRequest, pageData{Error: "That form could not be read."})
		return
	}
	name := r.PostForm.Get("name")
	plain, tok, err := p.m.Mint(r.Context(), user, name, r.PostForm["scope"])
	if errors.Is(err, ErrInvalidRequest) {
		p.render(w, r, user, http.StatusUnprocessableEntity,
			pageData{Error: "Give the token a name (up to 100 characters) and at least one scope."})
		return
	}
	if err != nil {
		p.m.cfg.Logger.Error("apitoken: mint", "err", err)
		p.render(w, r, user, http.StatusServiceUnavailable, pageData{Error: "The token could not be created. Try again."})
		return
	}
	p.m.cfg.Logger.Info("apitoken: minted", "token_id", tok.ID)
	p.render(w, r, user, http.StatusOK, pageData{NewToken: plain, NewTokenName: tok.Name})
}

func (p *page) revoke(w http.ResponseWriter, r *http.Request) {
	user, ok := p.m.cfg.CurrentUser(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch err := p.m.Revoke(r.Context(), user, id); {
	case errors.Is(err, ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		p.m.cfg.Logger.Error("apitoken: revoke", "token_id", id, "err", err)
		p.render(w, r, user, http.StatusServiceUnavailable, pageData{Error: "The token could not be revoked. Try again."})
		return
	}
	p.m.cfg.Logger.Info("apitoken: revoked", "token_id", id)
	http.Redirect(w, r, p.base, http.StatusSeeOther)
}

func (p *page) render(w http.ResponseWriter, r *http.Request, user string, status int, d pageData) {
	tokens, err := p.m.List(r.Context(), user)
	if err != nil {
		p.m.cfg.Logger.Error("apitoken: list", "err", err)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if p.m.cfg.Chrome != nil {
		d.DocumentData = p.m.cfg.Chrome(r)
	}
	if p.m.cfg.Header != nil {
		d.Header = p.m.cfg.Header(r)
	}
	d.BasePath, d.Prefix, d.Tokens, d.Scopes = p.base, p.m.cfg.Prefix, tokens, p.m.cfg.Scopes

	var buf bytes.Buffer
	if err := p.tpl.ExecuteTemplate(&buf, "page", d); err != nil {
		p.m.cfg.Logger.Error("apitoken: render", "err", err)
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	// Private to the user, and on a mint response it carries a live secret.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// csrf rejects a state-changing request whose Origin (or, failing that,
// Referer) names a different host. Every modern browser sends one of them on
// a POST, so a request with neither fails closed, as does Origin "null".
// Config.CSRF.Disable turns it off for a host that enforces CSRF itself.
func (p *page) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.m.cfg.CSRF.Disable || r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "forbidden: cross-origin request rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	src := r.Header.Get("Origin")
	if src == "" {
		src = r.Header.Get("Referer")
	}
	if src == "" || src == "null" {
		return false
	}
	u, err := url.Parse(src)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Host == r.Host
}
