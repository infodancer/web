package antibot

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIPRateLimiter_AllowsUnderLimit(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	l := NewIPRateLimiter(RateLimiterConfig{
		Max:             3,
		Window:          time.Hour,
		CleanupInterval: time.Hour, // Won't fire during test.
		Now:             func() time.Time { return now },
	})
	defer l.Stop()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := l.Middleware()(inner)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader("email=test@example.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "1.2.3.4:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Body.String() != "ok" {
			t.Errorf("request %d: expected ok, got %q", i+1, rec.Body.String())
		}
	}
}

func TestIPRateLimiter_BlocksOverLimit(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	l := NewIPRateLimiter(RateLimiterConfig{
		Max:             2,
		Window:          time.Hour,
		CleanupInterval: time.Hour,
		Now:             func() time.Time { return now },
	})
	defer l.Stop()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := l.Middleware()(inner)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader("email=test@example.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "1.2.3.4:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Third request should be blocked.
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader("email=test@example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "1.2.3.4:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Too many requests") {
		t.Errorf("expected rate limit message, got %q", rec.Body.String())
	}
}

func TestIPRateLimiter_WindowExpiry(t *testing.T) {
	current := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	l := NewIPRateLimiter(RateLimiterConfig{
		Max:             1,
		Window:          time.Hour,
		CleanupInterval: time.Hour,
		Now:             func() time.Time { return current },
	})
	defer l.Stop()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := l.Middleware()(inner)

	// First request — allowed.
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader("email=test@example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "1.2.3.4:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "ok" {
		t.Fatalf("first request should pass, got %q", rec.Body.String())
	}

	// Advance past window.
	current = current.Add(61 * time.Minute)

	// Should be allowed again.
	req = httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader("email=test@example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "1.2.3.4:12345"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "ok" {
		t.Errorf("request after window should pass, got %q", rec.Body.String())
	}
}

func TestIPRateLimiter_DifferentIPs(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	l := NewIPRateLimiter(RateLimiterConfig{
		Max:             1,
		Window:          time.Hour,
		CleanupInterval: time.Hour,
		Now:             func() time.Time { return now },
	})
	defer l.Stop()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := l.Middleware()(inner)

	// IP 1 — allowed.
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader("email=a@example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "1.1.1.1:1111"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Body.String() != "ok" {
		t.Fatalf("IP1 should pass")
	}

	// IP 2 — also allowed (independent bucket).
	req = httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader("email=b@example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "2.2.2.2:2222"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Body.String() != "ok" {
		t.Fatalf("IP2 should pass")
	}
}

func TestIPRateLimiter_XForwardedFor(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	l := NewIPRateLimiter(RateLimiterConfig{
		Max:             1,
		Window:          time.Hour,
		CleanupInterval: time.Hour,
		Now:             func() time.Time { return now },
	})
	defer l.Stop()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := l.Middleware()(inner)

	// First request with XFF — allowed.
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader("email=test@example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
	req.RemoteAddr = "127.0.0.1:9999"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Body.String() != "ok" {
		t.Fatal("first XFF request should pass")
	}

	// Second request same XFF — blocked.
	req = httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader("email=test@example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
	req.RemoteAddr = "127.0.0.1:9999"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Too many requests") {
		t.Errorf("second XFF request should be blocked, got %q", rec.Body.String())
	}
}

func TestIPRateLimiter_GET_PassesThrough(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	l := NewIPRateLimiter(RateLimiterConfig{
		Max:             1,
		Window:          time.Hour,
		CleanupInterval: time.Hour,
		Now:             func() time.Time { return now },
	})
	defer l.Stop()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := l.Middleware()(inner)

	// Multiple GETs should all pass — rate limit only applies to POST.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/subscribe", nil)
		req.RemoteAddr = "1.2.3.4:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Body.String() != "ok" {
			t.Errorf("GET %d should pass, got %q", i+1, rec.Body.String())
		}
	}
}
