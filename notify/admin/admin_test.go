package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/infodancer/web/notify"
	"github.com/infodancer/web/notify/admin"
)

// fakeStore records what the handlers ask of it.
type fakeStore struct {
	feed       []notify.Event
	feedErr    error
	unread     int
	lastFilter notify.Filter
	markCalls  int
	markedUpTo time.Time
}

func (f *fakeStore) AdminFeed(_ context.Context, flt notify.Filter) ([]notify.Event, error) {
	f.lastFilter = flt
	return f.feed, f.feedErr
}

func (f *fakeStore) AdminUnreadCount(context.Context, notify.Identity) (int, error) {
	return f.unread, nil
}

func (f *fakeStore) AdminMarkSeen(_ context.Context, _ notify.Identity, upTo time.Time) error {
	f.markCalls++
	f.markedUpTo = upTo
	return nil
}

func allow(*http.Request) (notify.Identity, bool) {
	return notify.Identity{Issuer: "iss", Subject: "admin-1", Label: "a@x"}, true
}

func deny(*http.Request) (notify.Identity, bool) { return notify.Identity{}, false }

func newHandler(t *testing.T, store admin.Store, resolver admin.ResolverFunc) http.Handler {
	t.Helper()
	h, err := admin.New(admin.Config{
		Store:    store,
		Resolver: resolver,
		BasePath: "/n",
		SiteName: "OSG",
	})
	if err != nil {
		t.Fatalf("admin.New: %v", err)
	}
	return h.Routes()
}

func TestNewRequiresStoreAndResolver(t *testing.T) {
	t.Parallel()
	if _, err := admin.New(admin.Config{Resolver: admin.ResolverFunc(allow)}); err == nil {
		t.Error("expected error without Store")
	}
	if _, err := admin.New(admin.Config{Store: &fakeStore{}}); err == nil {
		t.Error("expected error without Resolver")
	}
}

func TestFeedForbiddenWhenResolverDenies(t *testing.T) {
	t.Parallel()
	h := newHandler(t, &fakeStore{}, deny)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}

func TestFeedError_FullPage_ChromeWrapped(t *testing.T) {
	t.Parallel()
	h := newHandler(t, &fakeStore{feedErr: errors.New("boom")}, allow)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "<!doctype html>") {
		t.Errorf("full-page feed error should be a chrome-wrapped document, got:\n%s", rr.Body.String())
	}
}

func TestFeedError_HTMXFragment_StatusOnly(t *testing.T) {
	t.Parallel()
	h := newHandler(t, &fakeStore{feedErr: errors.New("boom")}, allow)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("HX-Request", "true")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "<!doctype html>") {
		t.Errorf("htmx fragment error should stay status-only, got a document:\n%s", rr.Body.String())
	}
}

func TestFeedFullPageVsFragment(t *testing.T) {
	t.Parallel()
	store := &fakeStore{feed: []notify.Event{
		{ID: 1, Type: "user.created", Source: "osg", Title: "New account", Severity: notify.SeverityInfo, CreatedAt: time.Now()},
	}}
	h := newHandler(t, store, allow)

	// Direct navigation → full document.
	full := httptest.NewRecorder()
	h.ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/", nil))
	if full.Code != http.StatusOK {
		t.Fatalf("full status = %d", full.Code)
	}
	body := full.Body.String()
	if !strings.Contains(body, "<!doctype html>") {
		t.Error("full page missing doctype")
	}
	if !strings.Contains(body, "notify-toolbar") {
		t.Error("full page missing toolbar")
	}
	if !strings.Contains(body, "New account") {
		t.Error("full page missing event title")
	}

	// htmx request → bare fragment, no document chrome.
	frag := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("HX-Request", "true")
	h.ServeHTTP(frag, req)
	fbody := frag.Body.String()
	if strings.Contains(fbody, "<!doctype html>") {
		t.Error("fragment should not contain doctype")
	}
	if !strings.Contains(fbody, "New account") {
		t.Error("fragment missing event title")
	}
}

func TestFeedRendersLinkAsAnchor(t *testing.T) {
	t.Parallel()
	store := &fakeStore{feed: []notify.Event{
		{ID: 2, Type: "session.added", Source: "osg", Title: "Session added",
			Severity: notify.SeverityInfo, Link: "https://osg.example/campaigns/7", CreatedAt: time.Now()},
		{ID: 1, Type: "user.created", Source: "osg", Title: "New account",
			Severity: notify.SeverityInfo, CreatedAt: time.Now()},
	}}
	h := newHandler(t, store, allow)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	body := rr.Body.String()

	if !strings.Contains(body, `<a class="notify-event-title" href="https://osg.example/campaigns/7">Session added</a>`) {
		t.Error("linked event not rendered as anchor")
	}
	if !strings.Contains(body, `<span class="notify-event-title">New account</span>`) {
		t.Error("linkless event should stay a plain span")
	}
}

func TestFeedParsesFilters(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	h := newHandler(t, store, allow)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet,
		"/?type=user.created&source=osg&severity=warn&since=2026-05-01&before=42", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}

	got := store.lastFilter
	if got.Type != "user.created" || got.Source != "osg" {
		t.Errorf("type/source = %q/%q", got.Type, got.Source)
	}
	if got.Severity != notify.SeverityWarn {
		t.Errorf("severity = %q", got.Severity)
	}
	if got.BeforeID != 42 {
		t.Errorf("beforeID = %d, want 42", got.BeforeID)
	}
	if got.Limit != 25 {
		t.Errorf("limit = %d, want default 25", got.Limit)
	}
	want := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if !got.Since.Equal(want) {
		t.Errorf("since = %v, want %v", got.Since, want)
	}
}

func TestFeedScopeAllAudiences(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	h := newHandler(t, store, allow)

	// Default scope → curated admin stream.
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if store.lastFilter.AllAudiences {
		t.Errorf("default scope should not widen audiences")
	}

	// scope=all → full activity firehose.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/?scope=all", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if !store.lastFilter.AllAudiences {
		t.Errorf("scope=all should set Filter.AllAudiences")
	}
}

func TestFeedRendersScopeToggle(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	h := newHandler(t, store, allow)

	rr := httptest.NewRecorder()
	// Full-page request (boosted/false) so the page chrome, not just the
	// fragment, renders.
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rr.Body.String()
	if !strings.Contains(body, "All activity") || !strings.Contains(body, "?scope=all") {
		t.Errorf("feed page should render the scope toggle, got:\n%s", body)
	}
}

func TestBellShowsUnread(t *testing.T) {
	t.Parallel()
	h := newHandler(t, &fakeStore{unread: 3}, allow)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/bell", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "notify-bell-unread") {
		t.Error("bell missing unread class")
	}
	if !strings.Contains(body, ">3<") {
		t.Errorf("bell missing count 3: %s", body)
	}
}

func TestBellCountIsBareNumber(t *testing.T) {
	t.Parallel()
	h := newHandler(t, &fakeStore{unread: 7}, allow)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/bell/count", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	// The nav badge swaps this into .app-nav-badge's innerHTML, so it must be the
	// bare count with no anchor markup.
	body := strings.TrimSpace(rr.Body.String())
	if body != "7" {
		t.Errorf("count body = %q, want \"7\"", body)
	}
	if strings.Contains(body, "<") {
		t.Errorf("count body should carry no markup: %q", body)
	}
}

func TestBellCountEmptyWhenZero(t *testing.T) {
	t.Parallel()
	h := newHandler(t, &fakeStore{unread: 0}, allow)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/bell/count", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != "" {
		t.Errorf("zero count body = %q, want empty", got)
	}
}

func TestBellCountForbiddenWhenResolverDenies(t *testing.T) {
	t.Parallel()
	h := newHandler(t, &fakeStore{unread: 3}, deny)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/bell/count", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}

func TestSeenAdvancesCursorAndTriggers(t *testing.T) {
	t.Parallel()
	store := &fakeStore{unread: 5}
	h := newHandler(t, store, allow)

	upTo := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	form := "up_to=" + upTo.Format(time.RFC3339Nano)
	req := httptest.NewRequest(http.MethodPost, "/seen", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if rr.Header().Get("HX-Trigger") != "notify:read" {
		t.Errorf("HX-Trigger = %q, want notify:read", rr.Header().Get("HX-Trigger"))
	}
	if store.markCalls != 1 {
		t.Fatalf("markCalls = %d, want 1", store.markCalls)
	}
	if !store.markedUpTo.Equal(upTo) {
		t.Errorf("markedUpTo = %v, want %v", store.markedUpTo, upTo)
	}
}

func TestStaticAssetsServed(t *testing.T) {
	t.Parallel()
	h := newHandler(t, &fakeStore{}, allow)
	cases := []struct {
		path  string
		sniff string // substring expected in the body, "" to skip
	}{
		{"/static/ui/css/tokens.css", "--app-"},  // from infodancer/ui
		{"/static/notify.css", "notify-bell"},    // notify's own
		{"/static/ui/js/htmx-2.0.10.min.js", ""}, // vendored htmx via ui
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rr.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", c.path, rr.Code)
			continue
		}
		if rr.Body.Len() == 0 {
			t.Errorf("%s served empty body", c.path)
		}
		if c.sniff != "" && !strings.Contains(rr.Body.String(), c.sniff) {
			t.Errorf("%s body missing %q", c.path, c.sniff)
		}
	}
}

func TestSeenForbiddenWhenResolverDenies(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	h := newHandler(t, store, deny)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/seen", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
	if store.markCalls != 0 {
		t.Error("mark should not be called when denied")
	}
}
