package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/infodancer/web/profile/profileuser"
	"github.com/infodancer/web/profile/store"
)

// seedUnlisted stores a profile under handle "bob" with is_public=false.
func seedUnlisted(t *testing.T, fs *fakeStore) {
	t.Helper()
	ctx := context.Background()
	if _, err := fs.Upsert(ctx, store.UpsertParams{
		UserKey:     "u2",
		DisplayName: "Bob",
		IsPublic:    false,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fs.ClaimHandle(ctx, "u2", "bob"); err != nil {
		t.Fatal(err)
	}
}

func TestUnlistedProfile404sForAnonymousAndOthers(t *testing.T) {
	fs := newFakeStore()
	seedUnlisted(t, fs)

	for name, u := range map[string]*profileuser.User{
		"anonymous":  nil,
		"other user": {Key: "u9"},
	} {
		h := newServer(t, fs, u)
		if rec := do(t, h, http.MethodGet, basePath+"/bob", nil, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", name, rec.Code)
		}
	}
}

func TestUnlistedProfileVisibleToOwnerAndAdmin(t *testing.T) {
	fs := newFakeStore()
	seedUnlisted(t, fs)

	for name, u := range map[string]*profileuser.User{
		"owner": {Key: "u2"},
		"admin": {Key: "u9", Admin: true},
	} {
		h := newServer(t, fs, u)
		rec := do(t, h, http.MethodGet, basePath+"/bob", nil, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: got %d, want 200", name, rec.Code)
		}
	}
}

func TestEditSavePersistsVisibilityCheckbox(t *testing.T) {
	fs := newFakeStore()
	u := &profileuser.User{Key: "u1", DisplayName: "Alice"}
	h := newServer(t, fs, u)
	ctx := context.Background()

	// Checkbox present: listed.
	rec := do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{
		"display_name": {"Alice"}, "is_public": {"on"},
	}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: got %d", rec.Code)
	}
	p, err := fs.Get(ctx, "u1")
	if err != nil || !p.IsPublic {
		t.Fatalf("after listed save: profile %+v, err %v", p, err)
	}

	// Checkbox absent: unlisted.
	rec = do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{
		"display_name": {"Alice"},
	}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unlist save: got %d", rec.Code)
	}
	if p, _ := fs.Get(ctx, "u1"); p.IsPublic {
		t.Error("after unchecked save: profile still listed")
	}
}

func TestEditFormDefaultsToListed(t *testing.T) {
	fs := newFakeStore()
	h := newServer(t, fs, &profileuser.User{Key: "new-user", DisplayName: "New"})

	body := do(t, h, http.MethodGet, basePath+"/edit", nil, nil).Body.String()
	if !strings.Contains(body, `name="is_public" value="on" checked`) {
		t.Error("first-visit form does not default the visibility checkbox to checked")
	}
}

func TestIdenticonServesForUnlistedProfile(t *testing.T) {
	// Card consumers (bylines next to published content) need the identicon
	// regardless of listing, so the avatar route is deliberately ungated.
	fs := newFakeStore()
	seedUnlisted(t, fs)
	h := newServer(t, fs, nil)
	if rec := do(t, h, http.MethodGet, basePath+"/avatar/bob", nil, nil); rec.Code != http.StatusOK {
		t.Errorf("identicon for unlisted profile: got %d, want 200", rec.Code)
	}
}
