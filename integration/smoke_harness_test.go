package integration

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	smoke "github.com/infodancer/smoke"
	"github.com/jackc/pgx/v5/pgxpool"

	contacthandler "github.com/infodancer/web/contact/handler"
	"github.com/infodancer/web/contact/submissions"
	"github.com/infodancer/web/faq"
	"github.com/infodancer/web/media"
	nlhandler "github.com/infodancer/web/newsletter/handler"
	"github.com/infodancer/web/newsletter/subscribers"
	"github.com/infodancer/web/notify"
	"github.com/infodancer/web/notify/admin"
	"github.com/infodancer/web/search"
	"github.com/infodancer/web/timeline"
)

// smokeApp is a running httptest server with every component mounted on one
// mux, plus the smoke manifest describing the routes to probe.
type smokeApp struct {
	server   *httptest.Server
	manifest smoke.Manifest
}

// noopMailer satisfies contact's Mailer without sending anything — the contact
// write probe only needs the handler to accept the submission and return 200.
type noopMailer struct{}

func (noopMailer) Send(context.Context, contacthandler.Message) error { return nil }

// buildSmokeApp migrates and mounts every implemented web component against the
// shared test pool, seeds the minimal fixtures their probed routes need, and
// returns a running server plus the route manifest. It mirrors the osg/herald
// pattern (boot the real handlers, probe them) adapted to a multi-component
// mount: each component owns a private mux behind StripPrefix, so the routes
// are declared in a smoke.Registry here rather than recorded at registration.
func buildSmokeApp(t *testing.T, pool *pgxpool.Pool) *smokeApp {
	t.Helper()
	ctx := context.Background()
	mux := http.NewServeMux()
	reg := smoke.New()

	mountSearch(t, ctx, pool, mux, reg)
	mountFAQ(t, ctx, pool, mux, reg)
	mountTimeline(t, ctx, pool, mux, reg)
	mountMedia(t, ctx, pool, mux, reg)
	mountNotify(t, ctx, pool, mux, reg)
	mountContact(t, ctx, pool, mux, reg)
	mountNewsletter(t, ctx, pool, mux, reg)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &smokeApp{server: srv, manifest: reg.Manifest()}
}

func mountSearch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mux *http.ServeMux, reg *smoke.Registry) {
	t.Helper()
	m, err := search.New(search.Config{DB: pool, BasePath: "/search"})
	if err != nil {
		t.Fatalf("search.New: %v", err)
	}
	if err := m.Migrate(ctx); err != nil {
		t.Fatalf("search migrate: %v", err)
	}
	mux.Handle("/search/", http.StripPrefix("/search", m.Routes()))
	reg.Add("GET", "/search/")
	reg.Add("GET", "/search/healthz")
	reg.Add("GET", "/search/search") // HTMX results partial; empty query renders no results
}

func mountFAQ(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mux *http.ServeMux, reg *smoke.Registry) {
	t.Helper()
	m, err := faq.New(faq.Config{
		DB:       pool,
		BasePath: "/faq",
		Resolver: faq.UserResolverFunc(func(context.Context, *http.Request) (*faq.User, error) {
			return nil, nil // anonymous is enough for the read surface
		}),
	})
	if err != nil {
		t.Fatalf("faq.New: %v", err)
	}
	if err := m.Migrate(ctx); err != nil {
		t.Fatalf("faq migrate: %v", err)
	}
	mux.Handle("/faq/", http.StripPrefix("/faq", m.Routes()))
	reg.Add("GET", "/faq/")
	reg.Add("GET", "/faq/q")
	reg.Add("GET", "/faq/tags")
	reg.Add("GET", "/faq/search")
	reg.Add("GET", "/faq/sitemap.xml")
}

func mountTimeline(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mux *http.ServeMux, reg *smoke.Registry) {
	t.Helper()
	m, err := timeline.New(timeline.Config{
		DB:       pool,
		BasePath: "/timeline",
		Resolver: timeline.UserResolverFunc(func(context.Context, *http.Request) (*timeline.User, error) {
			return nil, nil
		}),
	})
	if err != nil {
		t.Fatalf("timeline.New: %v", err)
	}
	if err := m.Migrate(ctx); err != nil {
		t.Fatalf("timeline migrate: %v", err)
	}
	mux.Handle("/timeline/", http.StripPrefix("/timeline", m.Routes()))
	reg.Add("GET", "/timeline/")
}

func mountMedia(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mux *http.ServeMux, reg *smoke.Registry) {
	t.Helper()
	if err := media.Migrate(ctx, pool); err != nil {
		t.Fatalf("media migrate: %v", err)
	}
	blobs := media.NewMemoryBlobStore()
	h, err := media.NewHandler(media.NewStore(pool), blobs, media.Config{
		Category:      "test",
		PublicBaseURL: "/media/test",
	})
	if err != nil {
		t.Fatalf("media.NewHandler: %v", err)
	}
	// Seed one stored blob so the content-addressed serve path has something to
	// return. serve() reads only from the blob store, keyed by (category, hash).
	hash := seedMediaBlob(t, ctx, blobs)
	mux.Handle("/media/test/", http.StripPrefix("/media/test", h.Routes()))
	reg.Add("GET", "/media/test/{file}", smoke.Example("file", hash))
}

func mountNotify(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mux *http.ServeMux, reg *smoke.Registry) {
	t.Helper()
	if err := notify.Migrate(ctx, pool); err != nil {
		t.Fatalf("notify migrate: %v", err)
	}
	h, err := admin.New(admin.Config{
		Store:    notify.NewStore(pool),
		BasePath: "/notify-admin",
		Resolver: admin.ResolverFunc(func(*http.Request) (notify.Identity, bool) {
			return notify.Identity{Issuer: "test", Subject: "admin", Label: "admin"}, true
		}),
	})
	if err != nil {
		t.Fatalf("notify admin.New: %v", err)
	}
	mux.Handle("/notify-admin/", http.StripPrefix("/notify-admin", h.Routes()))
	reg.Add("GET", "/notify-admin/")
	reg.Add("GET", "/notify-admin/bell")
	reg.Add("GET", "/notify-admin/bell/count")
}

func mountContact(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mux *http.ServeMux, reg *smoke.Registry) {
	t.Helper()
	if err := submissions.Migrate(ctx, pool); err != nil {
		t.Fatalf("contact migrate: %v", err)
	}
	h := contacthandler.New(submissions.New(pool), noopMailer{}, "to@example.org", "from@example.org")
	mux.Handle("POST /contact", h)
	reg.Add("POST", "/contact", smoke.Form(map[string][]string{
		"name":    {"Smoke Tester"},
		"email":   {"smoke@example.org"},
		"message": {"smoke probe"},
	}))
}

func mountNewsletter(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mux *http.ServeMux, reg *smoke.Registry) {
	t.Helper()
	if err := subscribers.Migrate(ctx, pool); err != nil {
		t.Fatalf("newsletter migrate: %v", err)
	}
	// listUUID empty => the listmonk client is never touched, so a nil client is
	// safe; the subscribe handler still records the address and returns 200.
	h := nlhandler.New(subscribers.New(pool), nil, "")
	mux.Handle("POST /subscribe", h)
	reg.Add("POST", "/subscribe", smoke.Form(map[string][]string{
		"email": {"smoke@example.org"},
	}))
}

// seedMediaBlob runs a tiny generated PNG through the real Process pipeline and
// stores the canonical bytes, returning the content-address hash for the probe.
func seedMediaBlob(t *testing.T, ctx context.Context, blobs media.BlobStore) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 60), G: uint8(y * 60), B: 120, A: 255})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatalf("encode seed png: %v", err)
	}
	p, err := media.Process(&raw, media.Config{Category: "test", MaxPixels: 25_000_000})
	if err != nil {
		t.Fatalf("media.Process seed: %v", err)
	}
	if err := blobs.Put(ctx, "test", p.SHA256, p.ContentType, p.Bytes); err != nil {
		t.Fatalf("seed blob put: %v", err)
	}
	return p.SHA256
}
