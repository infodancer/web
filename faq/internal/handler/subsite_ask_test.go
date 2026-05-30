//go:build integration

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

func TestSubsiteAsk_GetFormAnonRequiresAuth(t *testing.T) {
	srv, st := freshServer(t)
	_, err := st.CreateSubsite(context.Background(), store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wot/ask", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSubsiteAsk_GetFormAuthenticated(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	_, err := st.CreateSubsite(context.Background(), store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "Wheel of Time",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wot/ask", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Ask in Wheel of Time")
	require.Contains(t, body, `action="/faq/wot/ask"`)
}

func TestSubsiteAsk_SubmitCreatesSubsiteScopedQuestion(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	ctx := context.Background()
	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)

	form := url.Values{
		"title": {"Why does the Dark One want chaos?"},
		"body":  {"Asking for a friend."},
		"tags":  {"wheel-of-time, lore"},
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/wot/ask", form))

	require.Equal(t, http.StatusSeeOther, rec.Code)
	loc := rec.Header().Get("Location")
	require.Contains(t, loc, "/faq/wot/q/")
	require.Contains(t, loc, "why-does-the-dark-one-want-chaos")

	// Confirm the new row was created with the right subsite_id.
	idStr := loc[len("/faq/wot/q/"):]
	idStr = idStr[:strings.IndexByte(idStr, '/')]
	id, err := strconv.ParseInt(idStr, 10, 64)
	require.NoError(t, err)
	q, err := st.GetQuestion(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, q.SubsiteID)
	require.Equal(t, sub.ID, *q.SubsiteID, "subsite_id must be set from the URL")
}

func TestSubsiteAsk_404OnUnknownSubsite(t *testing.T) {
	srv, _ := freshServer(t, withResolver(regularUser("alice")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/no-such-subsite/ask", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSubsiteAsk_ValidationErrorRedisplaysForm(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	_, err := st.CreateSubsite(context.Background(), store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)

	form := url.Values{
		"title": {""},
		"body":  {""},
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/wot/ask", form))

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "title is required")
	require.Contains(t, body, "body is required")
	// Action still points to subsite ask URL.
	require.Contains(t, body, `action="/faq/wot/ask"`)
}
