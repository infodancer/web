package httpsec_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infodancer/web/httpsec"
)

func TestBodyLimitRejectsDeclaredOversize(t *testing.T) {
	called := false
	h := httpsec.BodyLimit(10)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 11)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
	if called {
		t.Error("handler ran for a body declared larger than the limit")
	}
}

func TestBodyLimitAllowsBodyAtLimit(t *testing.T) {
	var got string
	h := httpsec.BodyLimit(10)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read: %v", err)
		}
		got = string(b)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789")))
	if got != "0123456789" {
		t.Errorf("body = %q", got)
	}
}

func TestBodyLimitCapsUndeclaredLength(t *testing.T) {
	var readErr error
	h := httpsec.BodyLimit(10)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(strings.NewReader(strings.Repeat("x", 100))))
	req.ContentLength = -1 // chunked: the length is not declared up front
	h.ServeHTTP(httptest.NewRecorder(), req)
	var tooBig *http.MaxBytesError
	if !errors.As(readErr, &tooBig) {
		t.Errorf("read error = %v, want *http.MaxBytesError", readErr)
	}
}

func TestBodyLimitWithNoBody(t *testing.T) {
	called := false
	h := httpsec.BodyLimit(10)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Body = nil
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !called {
		t.Error("GET without a body should pass through")
	}
}

func TestBodyLimitNeedsPositiveLimit(t *testing.T) {
	for _, n := range []int64{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("BodyLimit(%d) accepted, want a panic at configuration time", n)
				}
			}()
			httpsec.BodyLimit(n)
		}()
	}
}
