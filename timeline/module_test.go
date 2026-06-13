package timeline_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/media"
	"github.com/infodancer/web/timeline"
	"github.com/infodancer/web/timeline/internal/transfer"
	"github.com/infodancer/web/timeline/timelineuser"
)

// newPool connects to the package's testcontainers Postgres (see TestMain).
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), testDSN)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// resolver returns a fixed user (or nil for anonymous) regardless of request.
type resolver struct{ u *timelineuser.User }

func (r resolver) Resolve(context.Context, *http.Request) (*timelineuser.User, error) {
	return r.u, nil
}

const basePath = "/timeline"

// mount wires the module under basePath the way a host would.
func mount(m *timeline.Module) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(basePath+"/", http.StripPrefix(basePath, m.Routes()))
	return mux
}

func sampleTimeline() transfer.TimelineExport {
	return transfer.TimelineExport{
		Name: "Test Timeline", Slug: "test", IsPublic: true,
		Categories: []transfer.Category{{Slug: "politics", Name: "Politics"}},
		Events: []transfer.Event{
			{
				Slug: "e1", Title: "First Event", Summary: "the start",
				Article: "## Origins\n\nThe **first** event of note.",
				Date:    "2001-01-01", Category: "politics",
			},
			{Slug: "e2", Title: "Second Event", Summary: "the middle", Date: "2002-02-02"},
		},
	}
}

// TestModuleServesImportedTimeline is the M3 smoke test: construct → migrate →
// import → serve. It exercises the mount surface end to end against Postgres.
func TestModuleServesImportedTimeline(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)

	admin := &timelineuser.User{Subject: "s", Issuer: "i", DisplayName: "Admin", Admin: true}
	m, err := timeline.New(timeline.Config{
		DB:          pool,
		Resolver:    resolver{u: admin},
		BasePath:    basePath,
		Blobs:       media.NewMemoryBlobStore(),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	// Clean slate after migrate.
	if _, err := pool.Exec(ctx, "TRUNCATE timeline_timelines, timeline_events, timeline_categories, timeline_tags, timeline_users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	stats, err := m.Import(ctx, "test", sampleTimeline())
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.Events != 2 || stats.Categories != 1 {
		t.Fatalf("import stats: got %+v, want 2 events / 1 category", stats)
	}

	srv := mount(m)

	// Index lists the public timeline and renders through ui chrome (ui CSS +
	// timeline's own layout CSS via ExtraCSS).
	body, code := get(t, srv, basePath+"/")
	if code != 200 || !strings.Contains(body, "Test Timeline") {
		t.Fatalf("index: code=%d, contains-name=%v", code, strings.Contains(body, "Test Timeline"))
	}
	if !strings.Contains(body, basePath+"/static/ui/css/tokens.css") {
		t.Error("index: ui chrome tokens.css not linked")
	}
	if !strings.Contains(body, basePath+"/static/style.css") {
		t.Error("index: timeline layout CSS not linked via ExtraCSS")
	}
	// Timeline page renders.
	if _, code := get(t, srv, basePath+"/t/test"); code != 200 {
		t.Fatalf("timeline page: code=%d, want 200", code)
	}
	// Admin export succeeds (resolver returns an admin).
	if body, code := get(t, srv, basePath+"/t/test/export"); code != 200 || !strings.Contains(body, `"slug": "test"`) {
		t.Fatalf("export: code=%d", code)
	}
}

// TestEventDetailRendersMarkdown confirms the event Article is rendered through
// infodancer/ui/markdown (M5) rather than escaped.
func TestEventDetailRendersMarkdown(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	admin := &timelineuser.User{Subject: "s", Issuer: "i", Admin: true}
	m, err := timeline.New(timeline.Config{
		DB: pool, Resolver: resolver{u: admin}, BasePath: basePath, AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE timeline_timelines, timeline_events, timeline_categories, timeline_tags, timeline_users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if _, err := m.Import(ctx, "test", sampleTimeline()); err != nil {
		t.Fatalf("import: %v", err)
	}

	body, code := get(t, mount(m), basePath+"/t/test/event/e1")
	if code != 200 {
		t.Fatalf("event detail: code=%d, want 200", code)
	}
	if !strings.Contains(body, "<h2") || !strings.Contains(body, "<strong>first</strong>") {
		t.Errorf("article not rendered as markdown HTML; body lacked <h2>/<strong>")
	}
	if strings.Contains(body, "## Origins") {
		t.Errorf("article rendered as raw/escaped markdown, not HTML")
	}
}

// TestEmbedModeStripsChrome confirms ?embed=true renders the chrome-less
// document (M5): the embed body class is present and the ui footer is absent.
func TestEmbedModeStripsChrome(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	m, err := timeline.New(timeline.Config{
		DB: pool, Resolver: resolver{u: nil}, BasePath: basePath, AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE timeline_timelines, timeline_events, timeline_categories, timeline_tags, timeline_users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if _, err := m.Import(ctx, "test", sampleTimeline()); err != nil {
		t.Fatalf("import: %v", err)
	}
	srv := mount(m)

	embed, code := get(t, srv, basePath+"/t/test?embed=true")
	if code != 200 || !strings.Contains(embed, `class="embed-mode"`) {
		t.Fatalf("embed: code=%d, has embed-mode=%v", code, strings.Contains(embed, `class="embed-mode"`))
	}
	full, _ := get(t, srv, basePath+"/t/test")
	if strings.Contains(embed, "<footer") && !strings.Contains(full, "<footer") {
		t.Error("embed should not include the footer that the full page has")
	}
}

// TestExportForbiddenForAnonymous confirms the admin gate: an anonymous user
// gets 401 on export but can still read the public timeline.
func TestExportForbiddenForAnonymous(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)

	m, err := timeline.New(timeline.Config{
		DB:          pool,
		Resolver:    resolver{u: nil}, // anonymous
		BasePath:    basePath,
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE timeline_timelines, timeline_events, timeline_categories, timeline_tags, timeline_users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if _, err := m.Import(ctx, "test", sampleTimeline()); err != nil {
		t.Fatalf("import: %v", err)
	}

	srv := mount(m)
	if _, code := get(t, srv, basePath+"/t/test"); code != 200 {
		t.Errorf("anon timeline read: code=%d, want 200", code)
	}
	if _, code := get(t, srv, basePath+"/t/test/export"); code != http.StatusUnauthorized {
		t.Errorf("anon export: code=%d, want 401", code)
	}
}

func get(t *testing.T, h http.Handler, path string) (string, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Body.String(), rec.Code
}
