//go:build integration

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

func TestAdminSubsites_AnonGets401(t *testing.T) {
	srv, _ := freshServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/subsites", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAdminSubsites_NonAdminGets403(t *testing.T) {
	srv, _ := freshServer(t, withResolver(regularUser("alice")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/subsites", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAdminSubsites_IndexEmpty(t *testing.T) {
	srv, _ := freshServer(t, withResolver(adminUser("root")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/subsites", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No subsites yet")
}

func TestAdminSubsites_CreateRejectsReservedSlug(t *testing.T) {
	srv, _ := freshServer(t, withResolver(adminUser("root")))

	form := url.Values{
		"slug":         {"q"},
		"display_name": {"Q"},
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/admin/subsites", form))

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "slug is reserved: q")
}

func TestAdminSubsites_CreateRejectsBadFormat(t *testing.T) {
	srv, _ := freshServer(t, withResolver(adminUser("root")))

	form := url.Values{
		"slug":         {"Brandon_Sanderson"}, // uppercase + underscore
		"display_name": {"Brandon Sanderson"},
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/admin/subsites", form))

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "slug must be lowercase")
}

func TestAdminSubsites_CreateHappyPath(t *testing.T) {
	srv, _ := freshServer(t, withResolver(adminUser("root")))

	form := url.Values{
		"slug":         {"brandon-sanderson"},
		"display_name": {"Brandon Sanderson"},
		"description":  {"FAQ for Sanderson works."},
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/admin/subsites", form))

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Contains(t, rec.Header().Get("Location"), "/faq/admin/subsites/")
}

func TestAdminSubsites_EditAttachAndDetachTag(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)

	// Attach a tag rule.
	attachForm := url.Values{"tag": {"wheel-of-time"}}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/admin/subsites/"+strconv.FormatInt(sub.ID, 10)+"/tags", attachForm))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	rules, err := st.ListTagRulesForSubsite(ctx, sub.ID)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, "wheel-of-time", rules[0].Name)
	tagID := rules[0].ID

	// Detach it.
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/subsites/"+strconv.FormatInt(sub.ID, 10)+
			"/tags/"+strconv.FormatInt(tagID, 10)+"/delete",
		url.Values{},
	))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	rules, err = st.ListTagRulesForSubsite(ctx, sub.ID)
	require.NoError(t, err)
	require.Empty(t, rules)
}

func TestAdminSubsites_AttachReservedTagRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	ctx := context.Background()
	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "wot", DisplayName: "WoT"})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/subsites/"+strconv.FormatInt(sub.ID, 10)+"/tags",
		url.Values{"tag": {"ask"}}, // reserved second-segment word
	))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAdminMoveQuestion_NativeReScoped(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	ctx := context.Background()

	wot, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "wot", DisplayName: "WoT"})
	require.NoError(t, err)
	bs, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "bs", DisplayName: "BS"})
	require.NoError(t, err)

	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title: "Q", BodyMd: "b", BodyHtml: "<p>b</p>",
		AuthorUserID: &alice.ID, SubsiteID: &wot.ID,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/q/"+strconv.FormatInt(q.ID, 10)+"/move",
		url.Values{"subsite_id": {strconv.FormatInt(bs.ID, 10)}},
	))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	got, err := st.GetQuestion(ctx, q.ID)
	require.NoError(t, err)
	require.NotNil(t, got.SubsiteID)
	require.Equal(t, bs.ID, *got.SubsiteID)
}

func TestAdminMoveQuestion_ImportedNoOp(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	ctx := context.Background()

	src := mustHandlerSource(t, st, "forum.example.com")
	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)
	q := mustHandlerImportedQuestion(t, st, src, "Imported")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/q/"+strconv.FormatInt(q.ID, 10)+"/move",
		url.Values{"subsite_id": {strconv.FormatInt(sub.ID, 10)}},
	))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	// SQL guard (source_id IS NULL) means the imported question's
	// subsite_id remains NULL even after the admin "move".
	got, err := st.GetQuestion(ctx, q.ID)
	require.NoError(t, err)
	require.Nil(t, got.SubsiteID, "imported questions cannot be re-scoped")
}
