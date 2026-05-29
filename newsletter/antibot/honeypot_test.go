package antibot

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHoneypot_EmptyField_PassesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	handler := Honeypot("website")(inner)

	form := url.Values{"email": {"test@example.com"}, "website": {""}}
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "ok" {
		t.Errorf("expected inner handler response, got %q", rec.Body.String())
	}
}

func TestHoneypot_FilledField_Rejected(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called")
	})

	handler := Honeypot("website")(inner)

	form := url.Values{"email": {"bot@spam.com"}, "website": {"http://spam.example"}}
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "success") {
		t.Errorf("expected fake success response, got %q", rec.Body.String())
	}
}

func TestHoneypot_MissingField_PassesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	handler := Honeypot("website")(inner)

	form := url.Values{"email": {"test@example.com"}}
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "ok" {
		t.Errorf("expected inner handler response, got %q", rec.Body.String())
	}
}

func TestHoneypot_GET_PassesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	handler := Honeypot("website")(inner)

	req := httptest.NewRequest(http.MethodGet, "/subscribe?website=filled", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "ok" {
		t.Errorf("expected inner handler response, got %q", rec.Body.String())
	}
}
