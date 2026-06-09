package integration

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/infodancer/web/contact/submissions"
	profile "github.com/infodancer/web/profile"
	"github.com/infodancer/web/profile/profileuser"
	profilestore "github.com/infodancer/web/profile/store"
)

// profileStubResolver is an always-anonymous resolver -- enough to construct
// the profile module for migration and store checks.
type profileStubResolver struct{}

func (profileStubResolver) Resolve(context.Context, *http.Request) (*profileuser.User, error) {
	return nil, nil
}

// TestProfileCoexistsInSharedDatabase verifies the profile component migrates
// into the same database as the others without colliding: its prefixed table
// and its dedicated goose version table sit alongside contact's.
func TestProfileCoexistsInSharedDatabase(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	if err := submissions.Migrate(ctx, pool); err != nil {
		t.Fatalf("contact migrate: %v", err)
	}

	m, err := profile.New(profile.Config{DB: pool, Resolver: profileStubResolver{}})
	if err != nil {
		t.Fatalf("profile new: %v", err)
	}
	if err := m.Migrate(ctx); err != nil {
		t.Fatalf("profile migrate: %v", err)
	}

	for _, table := range []string{
		"contact_submissions", "profile_profiles",
		"contact_goose_db_version", "profile_goose_db_version",
	} {
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = $1)", table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %q: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %q to exist", table)
		}
	}
}

// TestProfileStorePostgres exercises the sqlc-backed store against the real
// database: upsert round-trips, the claim-once handle rules, cross-user
// uniqueness, and avatar updates.
func TestProfileStorePostgres(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	m, err := profile.New(profile.Config{DB: pool, Resolver: profileStubResolver{}, AutoMigrate: true})
	if err != nil {
		t.Fatalf("profile new: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE profile_profiles"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	s := m.Store()

	// Upsert round-trips all editable fields, including links JSON.
	p, err := s.Upsert(ctx, profilestore.UpsertParams{
		UserKey:     "it-u1",
		DisplayName: "Alice",
		BioMD:       "Hello *world*",
		Links:       []profilestore.Link{{Label: "Blog", URL: "https://example.org"}},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if p.DisplayName != "Alice" || len(p.Links) != 1 || p.Links[0].Label != "Blog" {
		t.Errorf("round-trip mismatch: %+v", p)
	}

	// Second upsert updates in place.
	if _, err := s.Upsert(ctx, profilestore.UpsertParams{UserKey: "it-u1", DisplayName: "Alice2"}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	p, err = s.Get(ctx, "it-u1")
	if err != nil || p.DisplayName != "Alice2" {
		t.Errorf("update not applied: %+v err=%v", p, err)
	}
	if len(p.Links) != 0 {
		t.Errorf("links not replaced: %+v", p.Links)
	}

	// Claim-once handle semantics.
	if err := s.ClaimHandle(ctx, "it-u1", "it-alice"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := s.ClaimHandle(ctx, "it-u1", "it-other"); !errors.Is(err, profilestore.ErrHandleAlreadySet) {
		t.Errorf("re-claim: err = %v, want ErrHandleAlreadySet", err)
	}

	// Cross-user uniqueness via the DB constraint.
	if _, err := s.Upsert(ctx, profilestore.UpsertParams{UserKey: "it-u2"}); err != nil {
		t.Fatalf("upsert u2: %v", err)
	}
	if err := s.ClaimHandle(ctx, "it-u2", "it-alice"); !errors.Is(err, profilestore.ErrHandleTaken) {
		t.Errorf("duplicate claim: err = %v, want ErrHandleTaken", err)
	}

	// Lookup by handle; admin rename; avatar set/clear; missing rows.
	if p, err = s.GetByHandle(ctx, "it-alice"); err != nil || p.UserKey != "it-u1" {
		t.Errorf("get by handle: %+v err=%v", p, err)
	}
	if err := s.SetHandle(ctx, "it-u1", "it-renamed"); err != nil {
		t.Errorf("admin rename: %v", err)
	}
	if err := s.SetAvatarURL(ctx, "it-u1", "/media/avatar/x.webp"); err != nil {
		t.Errorf("set avatar: %v", err)
	}
	if _, err := s.Get(ctx, "it-missing"); !errors.Is(err, profilestore.ErrNotFound) {
		t.Errorf("missing get: err = %v, want ErrNotFound", err)
	}
	if err := s.SetAvatarURL(ctx, "it-missing", "x"); !errors.Is(err, profilestore.ErrNotFound) {
		t.Errorf("missing avatar: err = %v, want ErrNotFound", err)
	}
}
