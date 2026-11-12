package apitoken_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/infodancer/ui"
	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/apitoken"
)

const base = "/account/tokens"

// pageFixture mounts the account page with a CurrentUser that reads the user
// from an X-Test-User header and answers 401 itself when there is none.
func pageFixture(t *testing.T) (*apitoken.Module, http.Handler) {
	t.Helper()
	m, _ := newModule(t, func(c *apitoken.Config) {
		c.BasePath = base
		c.CurrentUser = func(w http.ResponseWriter, r *http.Request) (string, bool) {
			if u := r.Header.Get("X-Test-User"); u != "" {
				return u, true
			}
			http.Error(w, "sign in", http.StatusUnauthorized)
			return "", false
		}
		c.Chrome = func(*http.Request) ui.DocumentData {
			return ui.DocumentData{AssetBase: "/static/ui", Meta: ui.Meta{SiteName: "Test Site"}}
		}
	})
	h, err := m.Handler()
	require.NoError(t, err)
	return m, h
}

func pageReq(h http.Handler, method, path, user string, form url.Values, origin string) *httptest.ResponseRecorder {
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if user != "" {
		r.Header.Set("X-Test-User", user)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

const sameOrigin = "http://example.com" // httptest.NewRequest's Host

func TestPage_MintShowsTheTokenOnce(t *testing.T) {
	m, h := pageFixture(t)

	rec := pageReq(h, http.MethodGet, base, "alice", nil, "")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "<!doctype html>", "renders through ui/document")
	require.Contains(t, body, "Test Site", "with the host's chrome")
	require.Contains(t, body, `value="notes:write"`)
	require.Contains(t, body, "You have no tokens.")

	rec = pageReq(h, http.MethodPost, base, "alice",
		url.Values{"name": {"laptop CLI"}, "scope": {"notes:write"}}, sameOrigin)
	require.Equal(t, http.StatusOK, rec.Code, "the token is in the response, not a redirect")
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Empty(t, rec.Header().Get("Location"))
	plain := regexp.MustCompile(prefix + `[A-Za-z0-9_-]{43}`).FindString(rec.Body.String())
	require.NotEmpty(t, plain, "mint response shows the token")

	tok, err := m.Authenticate(context.Background(), plain)
	require.NoError(t, err)
	require.Equal(t, "alice", tok.UserKey)
	require.Equal(t, "laptop CLI", tok.Name)

	// Afterwards the page lists it by its display prefix only.
	rec = pageReq(h, http.MethodGet, base, "alice", nil, "")
	require.NotContains(t, rec.Body.String(), plain)
	require.Contains(t, rec.Body.String(), prefix+tok.DisplayPrefix+"...")
}

func TestPage_MintRejectsBadInput(t *testing.T) {
	m, h := pageFixture(t)
	for name, form := range map[string]url.Values{
		"no name":       {"scope": {"notes:write"}},
		"no scope":      {"name": {"cli"}},
		"unknown scope": {"name": {"cli"}, "scope": {"root"}},
	} {
		rec := pageReq(h, http.MethodPost, base, "alice", form, sameOrigin)
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
	}
	list, err := m.List(context.Background(), "alice")
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestPage_CSRF(t *testing.T) {
	m, h := pageFixture(t)
	form := url.Values{"name": {"cli"}, "scope": {"notes:write"}}
	for name, origin := range map[string]string{
		"no origin":    "",
		"cross origin": "https://evil.example",
		"null origin":  "null",
	} {
		rec := pageReq(h, http.MethodPost, base, "alice", form, origin)
		require.Equal(t, http.StatusForbidden, rec.Code, name)
	}
	list, err := m.List(context.Background(), "alice")
	require.NoError(t, err)
	require.Empty(t, list, "a rejected post mints nothing")
}

func TestPage_Revoke(t *testing.T) {
	m, h := pageFixture(t)
	ctx := context.Background()
	_, mine, err := m.Mint(ctx, "alice", "mine", []string{"notes:write"})
	require.NoError(t, err)
	_, bobs, err := m.Mint(ctx, "bob", "bob's", []string{"notes:write"})
	require.NoError(t, err)

	rec := pageReq(h, http.MethodPost, base+"/"+strconv.FormatInt(bobs.ID, 10)+"/revoke", "alice", url.Values{}, sameOrigin)
	require.Equal(t, http.StatusNotFound, rec.Code, "someone else's token")

	rec = pageReq(h, http.MethodPost, base+"/"+strconv.FormatInt(mine.ID, 10)+"/revoke", "alice", url.Values{}, "")
	require.Equal(t, http.StatusForbidden, rec.Code, "revoke is CSRF-checked too")

	rec = pageReq(h, http.MethodPost, base+"/"+strconv.FormatInt(mine.ID, 10)+"/revoke", "alice", url.Values{}, sameOrigin)
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, base, rec.Header().Get("Location"))

	list, err := m.List(ctx, "alice")
	require.NoError(t, err)
	require.NotNil(t, list[0].RevokedAt)
	bobList, err := m.List(ctx, "bob")
	require.NoError(t, err)
	require.Nil(t, bobList[0].RevokedAt)

	rec = pageReq(h, http.MethodPost, base+"/nope/revoke", "alice", url.Values{}, sameOrigin)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPage_RequiresSignIn(t *testing.T) {
	_, h := pageFixture(t)
	require.Equal(t, http.StatusUnauthorized, pageReq(h, http.MethodGet, base, "", nil, "").Code)
	require.Equal(t, http.StatusUnauthorized,
		pageReq(h, http.MethodPost, base, "", url.Values{"name": {"x"}, "scope": {"notes:write"}}, sameOrigin).Code)
}

func TestHandler_NeedsItsConfig(t *testing.T) {
	m, _ := newModule(t)
	_, err := m.Handler()
	require.Error(t, err, "no CurrentUser")
	m, _ = newModule(t, func(c *apitoken.Config) {
		c.CurrentUser = func(http.ResponseWriter, *http.Request) (string, bool) { return "", false }
	})
	_, err = m.Handler()
	require.Error(t, err, "no BasePath")
}
