package antibot

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18")
	req.RemoteAddr = "127.0.0.1:9999"

	if got := clientIP(req); got != "203.0.113.50" {
		t.Errorf("expected 203.0.113.50, got %q", got)
	}
}

func TestClientIP_FallbackRemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:4321"

	if got := clientIP(req); got != "10.0.0.5" {
		t.Errorf("expected 10.0.0.5, got %q", got)
	}
}

func TestClientIP_RemoteAddrNoPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5"

	if got := clientIP(req); got != "10.0.0.5" {
		t.Errorf("expected 10.0.0.5, got %q", got)
	}
}
