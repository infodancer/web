package obs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestRequestIDRoundTrip(t *testing.T) {
	ctx := WithRequestID(context.Background(), "abc123")
	if got := RequestID(ctx); got != "abc123" {
		t.Fatalf("RequestID = %q, want %q", got, "abc123")
	}
}

func TestRequestIDAbsent(t *testing.T) {
	if got := RequestID(context.Background()); got != "" {
		t.Fatalf("RequestID on bare context = %q, want empty", got)
	}
}

func TestMiddlewareGeneratesID(t *testing.T) {
	var seen string
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestID(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen == "" {
		t.Fatal("handler saw empty request id; middleware did not generate one")
	}
	if got := rec.Header().Get(RequestIDHeader); got != seen {
		t.Fatalf("response header %s = %q, want %q (the generated id)", RequestIDHeader, got, seen)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(seen) {
		t.Fatalf("generated id %q is not 32 hex chars", seen)
	}
}

func TestMiddlewareReusesInboundID(t *testing.T) {
	var seen string
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestID(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "upstream-id")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen != "upstream-id" {
		t.Fatalf("handler saw id %q, want the inbound %q", seen, "upstream-id")
	}
	if got := rec.Header().Get(RequestIDHeader); got != "upstream-id" {
		t.Fatalf("response header echoed %q, want inbound %q", got, "upstream-id")
	}
}

func TestNewRequestIDUnique(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	for range n {
		id := newRequestID()
		if _, dup := seen[id]; dup {
			t.Fatalf("newRequestID produced a duplicate: %q", id)
		}
		seen[id] = struct{}{}
	}
}
