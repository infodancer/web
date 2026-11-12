package apitoken_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/apitoken"
)

type hostUserKey struct{}

// dispatchFixture wires a module the way a host would: a browser chain that
// reports itself, and a token route set with one route per scope. The host's
// Authenticated hook stores the user key in the context, refuses the user
// "ghost" as gone and fails for "broken".
func dispatchFixture(t *testing.T) (*apitoken.Module, http.Handler) {
	t.Helper()
	m, _ := newModule(t, func(c *apitoken.Config) {
		c.Authenticated = func(ctx context.Context, tok *apitoken.Token) (context.Context, error) {
			switch tok.UserKey {
			case "ghost":
				return nil, apitoken.ErrNoUser
			case "broken":
				return nil, errors.New("user store down")
			}
			return context.WithValue(ctx, hostUserKey{}, tok.UserKey), nil
		}
	})

	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Chain", "browser")
		w.WriteHeader(http.StatusTeapot)
	})
	routes := m.Routes()
	report := func(w http.ResponseWriter, r *http.Request) {
		tok := apitoken.FromContext(r.Context())
		user, _ := r.Context().Value(hostUserKey{}).(string)
		var wrapped string
		if v, ok := r.Context().Value(wrapKey{}).(string); ok {
			wrapped = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user": user, "token_id": tok.ID, "cookie": r.Header.Get("Cookie"), "wrapped": wrapped,
		})
	}
	routes.HandleFunc("GET /api/whoami", "notes:write", report)
	routes.HandleFunc("GET /api/admin", "admin:read", report)
	routes.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), wrapKey{}, "yes")))
		})
	})
	return m, m.Dispatch(browser, routes)
}

type wrapKey struct{}

func do(h http.Handler, method, path, auth string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	return body["error"]
}

func TestDispatch_NonTokenRequestsGoToBrowser(t *testing.T) {
	_, h := dispatchFixture(t)
	for _, auth := range []string{"", "Bearer some-other-token", "Basic dXNlcjpwYXNz", "Bearer"} {
		rec := do(h, http.MethodPost, "/campaign/new", auth)
		require.Equal(t, "browser", rec.Header().Get("X-Chain"), "auth %q", auth)
	}
}

func TestDispatch_ValidToken(t *testing.T) {
	m, h := dispatchFixture(t)
	plain, tok, err := m.Mint(context.Background(), "alice", "cli", []string{"notes:write"})
	require.NoError(t, err)

	for _, auth := range []string{"Bearer " + plain, "bearer " + plain, "Bearer  " + plain + " "} {
		rec := do(h, http.MethodGet, "/api/whoami", auth, "Cookie", "session=abc")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Equal(t, "alice", got["user"], "the host hook's context reaches the route")
		require.EqualValues(t, tok.ID, got["token_id"])
		require.Empty(t, got["cookie"], "no session rides along on a token request")
		require.Equal(t, "yes", got["wrapped"], "host middleware runs after authentication")
	}
}

func TestDispatch_BadTokensAre401AndNeverReachBrowser(t *testing.T) {
	m, h := dispatchFixture(t)
	ctx := context.Background()
	revokedPlain, revoked, err := m.Mint(ctx, "alice", "old", []string{"notes:write"})
	require.NoError(t, err)
	require.NoError(t, m.Revoke(ctx, "alice", revoked.ID))
	ghost, _, err := m.Mint(ctx, "ghost", "cli", []string{"notes:write"})
	require.NoError(t, err)

	for name, plain := range map[string]string{
		"garbage":  prefix + "not-a-real-token",
		"revoked":  revokedPlain,
		"no user":  ghost,
		"too long": revokedPlain + "x",
	} {
		rec := do(h, http.MethodGet, "/api/whoami", "Bearer "+plain, "Cookie", "session=valid")
		require.Equal(t, http.StatusUnauthorized, rec.Code, name)
		require.Empty(t, rec.Header().Get("X-Chain"), "%s: must not fall through to the session", name)
		require.Equal(t, `Bearer error="invalid_token"`, rec.Header().Get("WWW-Authenticate"), name)
		require.Equal(t, "invalid_token", errorCode(t, rec), name)
	}
}

func TestDispatch_HostHookFailureIs503(t *testing.T) {
	m, h := dispatchFixture(t)
	plain, _, err := m.Mint(context.Background(), "broken", "cli", []string{"notes:write"})
	require.NoError(t, err)
	rec := do(h, http.MethodGet, "/api/whoami", "Bearer "+plain)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Equal(t, "unavailable", errorCode(t, rec))
}

func TestDispatch_TokenCannotReachUnregisteredRoutes(t *testing.T) {
	m, h := dispatchFixture(t)
	plain, _, err := m.Mint(context.Background(), "alice", "cli", []string{"notes:write", "admin:read"})
	require.NoError(t, err)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/campaign/new"},
		{http.MethodGet, "/account/tokens"},
		{http.MethodPost, "/api/whoami"}, // registered for GET only
		{http.MethodGet, "/"},
	} {
		rec := do(h, c.method, c.path, "Bearer "+plain)
		require.Empty(t, rec.Header().Get("X-Chain"), "%s %s", c.method, c.path)
		require.Contains(t, []int{http.StatusForbidden, http.StatusMethodNotAllowed}, rec.Code, "%s %s", c.method, c.path)
		if rec.Code == http.StatusForbidden {
			require.Equal(t, "token_not_allowed", errorCode(t, rec))
		}
	}
}

func TestDispatch_ScopeIsEnforced(t *testing.T) {
	m, h := dispatchFixture(t)
	plain, _, err := m.Mint(context.Background(), "alice", "cli", []string{"notes:write"})
	require.NoError(t, err)

	rec := do(h, http.MethodGet, "/api/admin", "Bearer "+plain)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "insufficient_scope", errorCode(t, rec))
	require.Contains(t, rec.Header().Get("WWW-Authenticate"), `scope="admin:read"`)

	rec = do(h, http.MethodGet, "/api/whoami", "Bearer "+plain)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireScope_PassesRequestsWithoutAToken(t *testing.T) {
	h := apitoken.RequireScope("admin:read", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	require.Equal(t, http.StatusNoContent, rec.Code, "browser requests are left to the host's own authz")
}

func TestRoutes_WiringMistakesPanic(t *testing.T) {
	m, _ := newModule(t, func(c *apitoken.Config) {
		c.Authenticated = func(ctx context.Context, _ *apitoken.Token) (context.Context, error) { return ctx, nil }
	})
	noop := func(http.ResponseWriter, *http.Request) {}

	routes := m.Routes()
	require.Panics(t, func() { routes.HandleFunc("GET /x", "root", noop) }, "unknown scope")

	routes.HandleFunc("GET /x", "notes:write", noop)
	_ = m.Dispatch(http.NotFoundHandler(), routes)
	require.Panics(t, func() { routes.HandleFunc("GET /y", "notes:write", noop) }, "registered after Dispatch")

	other, _ := newModule(t, func(c *apitoken.Config) {
		c.Authenticated = func(ctx context.Context, _ *apitoken.Token) (context.Context, error) { return ctx, nil }
	})
	require.Panics(t, func() { m.Dispatch(http.NotFoundHandler(), other.Routes()) }, "another module's routes")

	bare, _ := newModule(t)
	require.Panics(t, func() { bare.Dispatch(http.NotFoundHandler(), bare.Routes()) }, "no Authenticated hook")
}
