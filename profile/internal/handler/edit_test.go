package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/infodancer/web/profile/profileuser"
	"github.com/infodancer/web/profile/store"
)

var alice = &profileuser.User{Key: "u1", DisplayName: "Alice Example"}

func TestEditRequiresUser(t *testing.T) {
	h := newServer(t, newFakeStore(), nil)
	if rec := do(t, h, http.MethodGet, basePath+"/edit", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /edit anonymous: got %d, want 401", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{"display_name": {"x"}}, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /edit anonymous: got %d, want 401", rec.Code)
	}
}

func TestEditFormSeedsDisplayName(t *testing.T) {
	h := newServer(t, newFakeStore(), alice)
	rec := do(t, h, http.MethodGet, basePath+"/edit", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Alice Example") {
		t.Error("first-visit form does not seed the resolver display name")
	}
}

func TestEditSavePersistsAndRedirects(t *testing.T) {
	fs := newFakeStore()
	h := newServer(t, fs, alice)
	rec := do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{
		"display_name": {"Alice"},
		"bio":          {"Hello *world*"},
		"handle":       {"Alice-99"},
		"link_label":   {"Blog", ""},
		"link_url":     {"https://example.org/blog", ""},
	}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("got %d, want 303; body: %s", rec.Code, rec.Body.String())
	}
	p, err := fs.Get(context.Background(), "u1")
	if err != nil {
		t.Fatalf("profile not stored: %v", err)
	}
	if p.DisplayName != "Alice" || p.BioMD != "Hello *world*" {
		t.Errorf("stored fields wrong: %+v", p)
	}
	if p.Handle != "alice-99" {
		t.Errorf("handle not normalized+claimed: %q", p.Handle)
	}
	if len(p.Links) != 1 || p.Links[0].URL != "https://example.org/blog" {
		t.Errorf("links wrong: %+v", p.Links)
	}
}

func TestEditSaveRejectsBadLinks(t *testing.T) {
	fs := newFakeStore()
	h := newServer(t, fs, alice)
	for _, bad := range []string{"javascript:alert(1)", "ftp://example.org/x", "not a url", "//example.org/proto-relative"} {
		rec := do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{
			"link_label": {"x"},
			"link_url":   {bad},
		}, nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("link %q: got %d, want 422", bad, rec.Code)
		}
	}
}

func TestHandleImmutableOnceClaimed(t *testing.T) {
	fs := newFakeStore()
	h := newServer(t, fs, alice)
	if rec := do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{"handle": {"alice"}}, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("claim: got %d", rec.Code)
	}
	rec := do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{"handle": {"alice2"}}, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("rename: got %d, want 422", rec.Code)
	}
	if p, _ := fs.Get(context.Background(), "u1"); p.Handle != "alice" {
		t.Errorf("handle changed despite rejection: %q", p.Handle)
	}
}

func TestAdminMayRenameHandle(t *testing.T) {
	fs := newFakeStore()
	admin := &profileuser.User{Key: "u1", Admin: true}
	h := newServer(t, fs, admin)
	if rec := do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{"handle": {"alice"}}, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("claim: got %d", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{"handle": {"alice2"}}, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("admin rename: got %d", rec.Code)
	}
	if p, _ := fs.Get(context.Background(), "u1"); p.Handle != "alice2" {
		t.Errorf("admin rename not applied: %q", p.Handle)
	}
}

func TestHandleTakenByAnotherUser(t *testing.T) {
	fs := newFakeStore()
	if _, err := fs.Upsert(context.Background(), store.UpsertParams{UserKey: "u2"}); err != nil {
		t.Fatal(err)
	}
	if err := fs.ClaimHandle(context.Background(), "u2", "taken"); err != nil {
		t.Fatal(err)
	}
	h := newServer(t, fs, alice)
	rec := do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{"handle": {"taken"}}, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "already taken") {
		t.Error("missing user-facing taken message")
	}
}

func TestReservedHandlesRejected(t *testing.T) {
	fs := newFakeStore()
	h := newServer(t, fs, alice, func(d *Deps) { d.Reserved = []string{"Gamemaster"} })
	for _, reserved := range []string{"admin", "edit", "static", "avatar", "gamemaster"} {
		rec := do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{"handle": {reserved}}, nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("handle %q: got %d, want 422", reserved, rec.Code)
		}
	}
}

func TestEditSaveCSRFRejectsCrossOrigin(t *testing.T) {
	h := newServer(t, newFakeStore(), alice)
	rec := do(t, h, http.MethodPost, basePath+"/edit",
		map[string][]string{"display_name": {"x"}},
		map[string]string{"Origin": "https://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST: got %d, want 403", rec.Code)
	}
}

func TestEditSaveCSRFFailsClosedWithoutOriginOrReferer(t *testing.T) {
	h := newServer(t, newFakeStore(), alice)
	rec := do(t, h, http.MethodPost, basePath+"/edit",
		map[string][]string{"display_name": {"x"}},
		map[string]string{"Origin": ""})
	// do() sets a same-origin Origin for POSTs; explicitly blank it out.
	if rec.Code != http.StatusForbidden {
		t.Fatalf("headerless POST: got %d, want 403", rec.Code)
	}
}

func TestIndexRedirectsSignedIn(t *testing.T) {
	h := newServer(t, newFakeStore(), alice)
	rec := do(t, h, http.MethodGet, basePath+"/", nil, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != basePath+"/edit" {
		t.Errorf("got %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := do(t, newServer(t, newFakeStore(), nil), http.MethodGet, basePath+"/", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("anonymous index: got %d, want 404", rec.Code)
	}
}
