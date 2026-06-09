package profile_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	profile "github.com/infodancer/web/profile"
	"github.com/infodancer/web/profile/profileuser"
)

const basePath = "/profile"

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type resolver struct{ u *profileuser.User }

func (r resolver) Resolve(context.Context, *http.Request) (*profileuser.User, error) {
	return r.u, nil
}

func TestNewValidatesConfig(t *testing.T) {
	if _, err := profile.New(profile.Config{}); err == nil {
		t.Error("New without DB should fail")
	}
}

func mount(m *profile.Module) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(basePath+"/", http.StripPrefix(basePath, m.Routes()))
	return mux
}

func post(t *testing.T, h http.Handler, target string, form string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://"+req.Host)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// TestModuleEndToEnd drives the real stack -- migrations, sqlc store, handlers
// -- through one save/view cycle against Postgres.
func TestModuleEndToEnd(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)

	alice := &profileuser.User{Key: "module-test-u1", DisplayName: "Alice"}
	m, err := profile.New(profile.Config{
		DB:          pool,
		Resolver:    resolver{u: alice},
		BasePath:    basePath,
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE profile_profiles"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	srv := mount(m)

	// Save a profile with a handle.
	rec := post(t, srv, basePath+"/edit",
		"display_name=Alice&bio=Hello+*world*&handle=module-alice&link_label=Blog&link_url=https%3A%2F%2Fexample.org%2Fblog")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: got %d: %s", rec.Code, rec.Body.String())
	}

	// Public page renders for anonymous visitors of a second module instance.
	anon, err := profile.New(profile.Config{DB: pool, Resolver: resolver{}, BasePath: basePath})
	if err != nil {
		t.Fatalf("anon module: %v", err)
	}
	body := get(t, mount(anon), basePath+"/module-alice").Body.String()
	for _, want := range []string{"Alice", "@module-alice", "<em>world</em>", "https://example.org/blog"} {
		if !strings.Contains(body, want) {
			t.Errorf("public page missing %q", want)
		}
	}

	// Handle uniqueness is enforced by the database across users.
	bob := &profileuser.User{Key: "module-test-u2", DisplayName: "Bob"}
	bm, err := profile.New(profile.Config{DB: pool, Resolver: resolver{u: bob}, BasePath: basePath})
	if err != nil {
		t.Fatalf("bob module: %v", err)
	}
	rec = post(t, mount(bm), basePath+"/edit", "handle=module-alice")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("duplicate handle: got %d, want 422", rec.Code)
	}

	// Card renders from the store.
	card, err := m.Card(ctx, "module-test-u1")
	if err != nil {
		t.Fatalf("card: %v", err)
	}
	if !strings.Contains(string(card), basePath+"/module-alice") {
		t.Errorf("card missing profile link: %s", card)
	}

	// Identicon fallback serves.
	if rec := get(t, srv, basePath+"/avatar/module-alice"); rec.Code != http.StatusOK {
		t.Errorf("identicon: got %d", rec.Code)
	}

	// Migrate is idempotent.
	if err := m.Migrate(ctx); err != nil {
		t.Errorf("re-migrate: %v", err)
	}
}
