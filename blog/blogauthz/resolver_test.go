package blogauthz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/infodancer/authz"
)

// fakeStore returns canned roles per subject.
type fakeStore struct {
	roles map[string][]string
}

func (f fakeStore) Roles(_ context.Context, _, subject, _ string) ([]string, error) {
	return f.roles[subject], nil
}
func (f fakeStore) Grant(context.Context, authz.Grant) error { return nil }
func (f fakeStore) Revoke(context.Context, string, string, string, string) error {
	return nil
}
func (f fakeStore) Holders(context.Context, string) ([]authz.Holding, bool, error) {
	return nil, false, nil
}

func identityFor(id *authz.Identity, err error) Identifier {
	return func(*http.Request) (*authz.Identity, error) { return id, err }
}

func TestNewResolverRoleMapping(t *testing.T) {
	store := fakeStore{roles: map[string][]string{
		"alice": {AuthorRole},
		"ada":   {AdminRole, AuthorRole},
		"bob":   {},
	}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	cases := []struct {
		subject       string
		author, admin bool
	}{
		{"alice", true, false},
		{"ada", true, true},
		{"bob", false, false},
	}
	for _, c := range cases {
		r := NewResolver(identityFor(&authz.Identity{
			Issuer: "https://idp.example", Subject: c.subject, DisplayName: "Name",
		}, nil), store, nil)
		u, err := r.Resolve(context.Background(), req)
		if err != nil {
			t.Fatalf("%s: %v", c.subject, err)
		}
		if u.Author != c.author || u.Admin != c.admin {
			t.Errorf("%s: author=%v admin=%v, want %v/%v", c.subject, u.Author, u.Admin, c.author, c.admin)
		}
		if u.Key != "https://idp.example|"+c.subject {
			t.Errorf("%s: default key = %q", c.subject, u.Key)
		}
	}
}

func TestNewResolverKeyFuncAndAnonymous(t *testing.T) {
	store := fakeStore{roles: map[string][]string{"alice": {AuthorRole}}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	r := NewResolver(identityFor(&authz.Identity{Issuer: "i", Subject: "alice"}, nil), store,
		func(id *authz.Identity) string { return "42" })
	u, err := r.Resolve(context.Background(), req)
	if err != nil || u.Key != "42" {
		t.Errorf("custom key: %v key=%q", err, u.Key)
	}

	// Anonymous passes through as (nil, nil).
	r = NewResolver(identityFor(nil, nil), store, nil)
	u, err = r.Resolve(context.Background(), req)
	if err != nil || u != nil {
		t.Errorf("anonymous: %v %v", u, err)
	}

	// A present-but-invalid credential propagates the error (→ 401).
	wantErr := errors.New("bad token")
	r = NewResolver(identityFor(nil, wantErr), store, nil)
	if _, err = r.Resolve(context.Background(), req); !errors.Is(err, wantErr) {
		t.Errorf("invalid credential err = %v", err)
	}
}
