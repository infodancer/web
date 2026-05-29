package antibot

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestTimingToken_RoundTrips(t *testing.T) {
	fixed := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	token := TimingTokenWith(func() time.Time { return fixed })

	if token == "" {
		t.Fatal("token should not be empty")
	}

	// A different time produces a different token.
	later := TimingTokenWith(func() time.Time { return fixed.Add(time.Second) })
	if token == later {
		t.Error("different times should produce different tokens")
	}
}

func TestTiming_SlowSubmission_PassesThrough(t *testing.T) {
	renderTime := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	submitTime := renderTime.Add(5 * time.Second)

	token := TimingTokenWith(func() time.Time { return renderTime })

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	handler := TimingWith("__ts", 3*time.Second, func() time.Time { return submitTime })(inner)

	form := url.Values{"email": {"test@example.com"}, "__ts": {token}}
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "ok" {
		t.Errorf("expected inner handler, got %q", rec.Body.String())
	}
}

func TestTiming_FastSubmission_Rejected(t *testing.T) {
	renderTime := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	submitTime := renderTime.Add(1 * time.Second) // Only 1s, min is 3s.

	token := TimingTokenWith(func() time.Time { return renderTime })

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called")
	})

	handler := TimingWith("__ts", 3*time.Second, func() time.Time { return submitTime })(inner)

	form := url.Values{"email": {"test@example.com"}, "__ts": {token}}
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "error") {
		t.Errorf("expected error response, got %q", rec.Body.String())
	}
}

func TestTiming_MissingToken_Rejected(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called")
	})

	handler := Timing("__ts", 3*time.Second)(inner)

	form := url.Values{"email": {"test@example.com"}}
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestTiming_InvalidToken_Rejected(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called")
	})

	handler := Timing("__ts", 3*time.Second)(inner)

	form := url.Values{"email": {"test@example.com"}, "__ts": {"not-valid-base64!!!"}}
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestTiming_GET_PassesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	handler := Timing("__ts", 3*time.Second)(inner)

	req := httptest.NewRequest(http.MethodGet, "/subscribe", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "ok" {
		t.Errorf("expected inner handler, got %q", rec.Body.String())
	}
}
