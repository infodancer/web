package antibot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func getToken(t *testing.T, h http.Handler, method string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, "/antibot/token/contact", nil))
	return rec
}

// A static page cannot embed a token when it is built, so it fetches one when
// it loads. The token served must be the same kind Check accepts, for that
// form and no other.
func TestTokenHandlerServesATokenForItsForm(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), nil)
	rec := getToken(t, g.TokenHandler("contact"), http.MethodGet)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	tok := strings.TrimSpace(rec.Body.String())
	c.Advance(10 * time.Second)

	if v := checkToken(t, g, "contact", tok); !v.Allowed {
		t.Fatalf("served token rejected for its own form: %+v", v)
	}
	if v := checkToken(t, g, "newsletter", tok); v.Allowed || v.Reason != ReasonTokenInvalid {
		t.Fatalf("served token for contact accepted by newsletter: %+v", v)
	}
}

// The token carries the time it was issued. A shared cache would hand one
// visitor's token to the next, already aged, until it expired for everyone.
func TestTokenHandlerResponseIsNeverCached(t *testing.T) {
	g, _ := testGuard(t, newFakeStore(), nil)
	rec := getToken(t, g.TokenHandler("contact"), http.MethodGet)

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestTokenHandlerAllowsOnlyGetAndHead(t *testing.T) {
	g, _ := testGuard(t, newFakeStore(), nil)
	h := g.TokenHandler("contact")

	if rec := getToken(t, h, http.MethodHead); rec.Code != http.StatusOK {
		t.Errorf("HEAD status = %d, want 200", rec.Code)
	}
	rec := getToken(t, h, http.MethodPost)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q, want %q", got, "GET, HEAD")
	}
}

// Serving a token counts nothing: the budget is spent by submitting, so a
// visitor who reloads the page does not use up their own limit.
func TestTokenHandlerTouchesNoStore(t *testing.T) {
	store := newFakeStore()
	g, _ := testGuard(t, store, nil)
	for range 10 {
		getToken(t, g.TokenHandler("contact"), http.MethodGet)
	}
	if store.calls != 0 {
		t.Fatalf("serving tokens made %d store calls, want 0", store.calls)
	}
}

func TestSweepEveryRunsAtOnceAndUntilCancelled(t *testing.T) {
	store := newFakeStore()
	g, _ := testGuard(t, store, nil)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		g.SweepEvery(ctx, time.Millisecond)
		close(done)
	}()

	deadline := time.After(5 * time.Second)
	for store.Sweeps() < 3 {
		select {
		case <-deadline:
			t.Fatalf("got %d sweeps in 5s, want at least 3", store.Sweeps())
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SweepEvery did not return after its context was cancelled")
	}
}

// Nobody calls SweepEvery and waits for its answer, so a failure has to reach
// the log or it reaches nobody.
func TestSweepEveryLogsAStoreError(t *testing.T) {
	store := newFakeStore()
	store.err = errStore
	logger, buf := logBuffer()
	g, _ := testGuard(t, store, func(c *Config) { c.Logger = logger })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go g.SweepEvery(ctx, time.Hour)
	deadline := time.After(5 * time.Second)
	for store.Sweeps() < 1 {
		select {
		case <-deadline:
			t.Fatal("SweepEvery never swept")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()

	var line map[string]any
	for i := 0; ; i++ {
		if first, _, _ := strings.Cut(buf.String(), "\n"); first != "" {
			if err := json.Unmarshal([]byte(first), &line); err != nil {
				t.Fatalf("log line is not JSON: %v", err)
			}
			break
		}
		if i > 5000 {
			t.Fatal("a failed sweep logged nothing")
		}
		time.Sleep(time.Millisecond)
	}
	if line["level"] != "WARN" || line["msg"] != "antibot_sweep_failed" || line["err"] == nil {
		t.Fatalf("log line = %v, want WARN antibot_sweep_failed with err", line)
	}
}
