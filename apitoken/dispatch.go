package apitoken

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type tokenKey struct{}

// FromContext returns the token a request authenticated with, or nil for a
// request that did not come through [Module.Dispatch] with a token.
func FromContext(ctx context.Context) *Token {
	t, _ := ctx.Value(tokenKey{}).(*Token)
	return t
}

// Routes holds the routes a token may reach. A route is token-capable only by
// being registered here, each with the scope it requires; anything else a
// token asks for is a 403 token_not_allowed. Build one with [Module.Routes].
type Routes struct {
	m       *Module
	mux     *http.ServeMux
	wrap    []func(http.Handler) http.Handler
	handler http.Handler // built once, by Dispatch
}

// Routes returns an empty route set for [Module.Dispatch].
func (m *Module) Routes() *Routes {
	r := &Routes{m: m, mux: http.NewServeMux()}
	r.mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusForbidden, "token_not_allowed")
	})
	return r
}

// Handle registers h for pattern (http.ServeMux syntax), callable by a token
// that carries scope. It panics on a scope the module was not configured
// with, as ServeMux does on a bad pattern: both are wiring mistakes, and
// failing at startup beats a route nobody can call.
func (rs *Routes) Handle(pattern, scope string, h http.Handler) {
	rs.mustBeOpen()
	if !rs.m.knownScope(scope) {
		panic("apitoken: route " + pattern + " requires unknown scope " + scope)
	}
	rs.mux.Handle(pattern, RequireScope(scope, h))
}

// HandleFunc is [Routes.Handle] for a function.
func (rs *Routes) HandleFunc(pattern, scope string, h http.HandlerFunc) {
	rs.Handle(pattern, scope, h)
}

// Use adds host middleware run after authentication and before the route,
// e.g. the host's own layer that loads roles into the context. The first
// added runs outermost.
func (rs *Routes) Use(mw func(http.Handler) http.Handler) {
	rs.mustBeOpen()
	rs.wrap = append(rs.wrap, mw)
}

// mustBeOpen panics once the set has been handed to Dispatch: the chain is
// built then, so a later registration would silently never be served.
func (rs *Routes) mustBeOpen() {
	if rs.handler != nil {
		panic("apitoken: routes changed after Dispatch")
	}
}

// seal builds the served chain. Called once, by Dispatch, before any request.
func (rs *Routes) seal() http.Handler {
	if rs.handler == nil {
		var h http.Handler = rs.mux
		for i := len(rs.wrap) - 1; i >= 0; i-- {
			h = rs.wrap[i](h)
		}
		rs.handler = h
	}
	return rs.handler
}

// RequireScope serves h only to a token that carries scope. A request with no
// token passes through, so the same handler can sit on the host's browser
// routes as well; the host's own authorization applies there.
func RequireScope(scope string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t := FromContext(r.Context()); t != nil && !t.HasScope(scope) {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="`+scope+`"`)
			writeError(w, http.StatusForbidden, "insufficient_scope")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// bearer returns the credential in r's Authorization header when it is one of
// this module's tokens.
func (m *Module) bearer(r *http.Request) (string, bool) {
	scheme, cred, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	cred = strings.TrimSpace(cred)
	return cred, strings.HasPrefix(cred, m.cfg.Prefix)
}

// Dispatch sends a request carrying one of the module's tokens to routes, and
// every other request to browser, untouched.
//
// Mount it outside the host's CSRF guard and session middleware, with browser
// being that chain. A token request skips both: CSRF works by abusing
// credentials the browser attaches on its own, and a bearer header is never
// attached that way. To keep any session from riding along, the Cookie header
// is removed before routes sees the request, and a bad token is a 401 -- it
// never falls through to browser.
func (m *Module) Dispatch(browser http.Handler, routes *Routes) http.Handler {
	if m.cfg.Authenticated == nil {
		panic("apitoken: Dispatch needs Config.Authenticated")
	}
	if routes == nil || routes.m != m {
		panic("apitoken: Dispatch needs a route set from this module's Routes")
	}
	served := routes.seal()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plain, ok := m.bearer(r)
		if !ok {
			browser.ServeHTTP(w, r)
			return
		}
		log := m.cfg.Logger

		tok, err := m.Authenticate(r.Context(), plain)
		if errors.Is(err, ErrInvalidToken) {
			log.Warn("apitoken: rejected", "path", r.URL.Path)
			unauthorized(w)
			return
		}
		if err != nil {
			log.Error("apitoken: authenticate", "err", err)
			writeError(w, http.StatusServiceUnavailable, "unavailable")
			return
		}

		ctx, err := m.cfg.Authenticated(context.WithValue(r.Context(), tokenKey{}, tok), tok)
		if errors.Is(err, ErrNoUser) {
			log.Warn("apitoken: token has no user", "token_id", tok.ID)
			unauthorized(w)
			return
		}
		if err != nil {
			log.Error("apitoken: host authentication hook", "token_id", tok.ID, "err", err)
			writeError(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		// The hook builds on the context it was given, but make sure the
		// token is in what goes forward whatever it returned.
		if FromContext(ctx) != tok {
			ctx = context.WithValue(ctx, tokenKey{}, tok)
		}
		log.Debug("apitoken: authenticated", "token_id", tok.ID)

		r = r.WithContext(ctx)
		r.Header.Del("Cookie")
		served.ServeHTTP(w, r)
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	writeError(w, http.StatusUnauthorized, "invalid_token")
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
