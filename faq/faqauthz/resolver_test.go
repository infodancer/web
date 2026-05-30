package faqauthz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/infodancer/authz"
)

// memStore is an in-memory [authz.Store] for these unit tests. faq runs
// on Postgres in production, so the resolver only needs *some* Store to
// exercise — a real DB driver (sqlite) would pull a heavyweight,
// Postgres-irrelevant dependency into the module just for tests. This
// fake implements the same (issuer, subject, module, role) semantics
// authz documents: Roles returns module-scoped plus global ("") grants,
// deduplicated and sorted; Grant is idempotent.
type memStore struct {
	mu     sync.Mutex
	grants []authz.Grant
}

func (m *memStore) Roles(_ context.Context, issuer, subject, module string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]struct{}{}
	var roles []string
	for _, g := range m.grants {
		if g.Issuer != issuer || g.Subject != subject {
			continue
		}
		if g.Module != "" && g.Module != module {
			continue
		}
		if _, dup := seen[g.Role]; dup {
			continue
		}
		seen[g.Role] = struct{}{}
		roles = append(roles, g.Role)
	}
	sort.Strings(roles)
	return roles, nil
}

func (m *memStore) Grant(_ context.Context, g authz.Grant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.grants {
		if e.Issuer == g.Issuer && e.Subject == g.Subject && e.Module == g.Module && e.Role == g.Role {
			return nil // idempotent re-grant
		}
	}
	m.grants = append(m.grants, g)
	return nil
}

func (m *memStore) Revoke(_ context.Context, issuer, subject, module, role string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := make([]authz.Grant, 0, len(m.grants))
	for _, g := range m.grants {
		if g.Issuer == issuer && g.Subject == subject && g.Module == module && g.Role == role {
			continue
		}
		kept = append(kept, g)
	}
	m.grants = kept
	return nil
}

func (m *memStore) Holders(_ context.Context, role string) ([]authz.Holding, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var hs []authz.Holding
	for _, g := range m.grants {
		if g.Role == role {
			hs = append(hs, authz.Holding{
				Issuer: g.Issuer, Subject: g.Subject, Module: g.Module,
				Role: g.Role, GrantedBy: g.GrantedBy,
			})
		}
	}
	return hs, false, nil
}

func aliceID(*http.Request) (*authz.Identity, error) {
	return &authz.Identity{Issuer: "iss", Subject: "alice", DisplayName: "Alice", Email: "a@example.com"}, nil
}

func anonID(*http.Request) (*authz.Identity, error) { return nil, nil }

func newStore(t *testing.T) authz.Store {
	t.Helper()
	return &memStore{}
}

func grant(t *testing.T, st authz.Store, module, role string) {
	t.Helper()
	err := st.Grant(context.Background(), authz.Grant{
		Issuer: "iss", Subject: "alice", Module: module, Role: role, GrantedBy: "test",
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
}

func req() *http.Request { return httptest.NewRequest("GET", "/", nil) }

func TestAdminViaModuleRole(t *testing.T) {
	st := newStore(t)
	grant(t, st, Module, AdminRole) // admin in module "faq"

	u, err := NewResolver(aliceID, st).Resolve(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || !u.Admin {
		t.Fatalf("user = %+v, want non-nil with Admin=true", u)
	}
	if u.Subject != "alice" || u.DisplayName != "Alice" || u.Email != "a@example.com" {
		t.Errorf("identity fields not threaded: %+v", u)
	}
}

func TestAdminViaGlobalRole(t *testing.T) {
	st := newStore(t)
	grant(t, st, "", AdminRole) // global admin (module "")

	u, err := NewResolver(aliceID, st).Resolve(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || !u.Admin {
		t.Fatalf("user = %+v, want non-nil with Admin=true (global admin)", u)
	}
}

func TestAuthenticatedNonAdmin(t *testing.T) {
	st := newStore(t)
	// No grant: alice is a known, logged-in user with no roles.

	u, err := NewResolver(aliceID, st).Resolve(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if u == nil {
		t.Fatal("user = nil, want non-nil authenticated user")
	}
	if u.Admin {
		t.Error("Admin = true, want false for a user with no roles")
	}
}

func TestAnonymous(t *testing.T) {
	st := newStore(t)
	u, err := NewResolver(anonID, st).Resolve(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if u != nil {
		t.Errorf("user = %+v, want nil for anonymous request", u)
	}
}

func TestModuleIsolation(t *testing.T) {
	st := newStore(t)
	grant(t, st, "sf", AdminRole) // admin in a *different* module

	u, err := NewResolver(aliceID, st).Resolve(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if u == nil {
		t.Fatal("user = nil, want non-nil")
	}
	if u.Admin {
		t.Error("Admin = true, want false — an sf-module admin is not a faq admin")
	}
}
