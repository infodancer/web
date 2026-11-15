package httpsec_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/infodancer/web/httpsec"
)

func TestCSRF(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		host    string
		origin  string
		referer string
		cfg     httpsec.CSRFConfig
		want    int
	}{
		{"GET with no headers", http.MethodGet, "example.com", "", "", httpsec.CSRFConfig{}, 200},
		{"HEAD with no headers", http.MethodHead, "example.com", "", "", httpsec.CSRFConfig{}, 200},
		{"OPTIONS with no headers", http.MethodOptions, "example.com", "", "", httpsec.CSRFConfig{}, 200},
		{"POST same origin", http.MethodPost, "example.com", "https://example.com", "", httpsec.CSRFConfig{}, 200},
		{"POST same origin, host case differs", http.MethodPost, "Example.COM", "https://example.com", "", httpsec.CSRFConfig{}, 200},
		{"POST cross origin", http.MethodPost, "example.com", "https://evil.example", "", httpsec.CSRFConfig{}, 403},
		{"POST origin null", http.MethodPost, "example.com", "null", "", httpsec.CSRFConfig{}, 403},
		{"POST no origin, no referer", http.MethodPost, "example.com", "", "", httpsec.CSRFConfig{}, 403},
		{"POST referer same host", http.MethodPost, "example.com", "", "https://example.com/form", httpsec.CSRFConfig{}, 200},
		{"POST referer other host", http.MethodPost, "example.com", "", "https://evil.example/form", httpsec.CSRFConfig{}, 403},
		{"origin wins over a matching referer", http.MethodPost, "example.com", "https://evil.example", "https://example.com/form", httpsec.CSRFConfig{}, 403},
		{"malformed origin", http.MethodPost, "example.com", "::bad::", "", httpsec.CSRFConfig{}, 403},
		{"origin with no host", http.MethodPost, "example.com", "https://", "", httpsec.CSRFConfig{}, 403},
		{"userinfo cannot smuggle the host", http.MethodPost, "example.com", "https://example.com@evil.example", "", httpsec.CSRFConfig{}, 403},
		{"userinfo naming the host as a prefix", http.MethodPost, "example.com", "https://evil.example@example.com", "", httpsec.CSRFConfig{}, 403},
		{"port matches", http.MethodPost, "example.com:8080", "http://example.com:8080", "", httpsec.CSRFConfig{}, 200},
		{"port differs", http.MethodPost, "example.com:8080", "http://example.com", "", httpsec.CSRFConfig{}, 403},
		{"PUT is guarded", http.MethodPut, "example.com", "https://evil.example", "", httpsec.CSRFConfig{}, 403},
		{"PATCH is guarded", http.MethodPatch, "example.com", "https://evil.example", "", httpsec.CSRFConfig{}, 403},
		{"DELETE is guarded", http.MethodDelete, "example.com", "https://evil.example", "", httpsec.CSRFConfig{}, 403},
		{"Disable lets cross origin through", http.MethodPost, "example.com", "https://evil.example", "", httpsec.CSRFConfig{Disable: true}, 200},
		{"trusted origin allowed", http.MethodPost, "example.com", "https://admin.example.com", "", httpsec.CSRFConfig{TrustedOrigins: []string{"admin.example.com"}}, 200},
		{"untrusted sibling still rejected", http.MethodPost, "example.com", "https://other.example.com", "", httpsec.CSRFConfig{TrustedOrigins: []string{"admin.example.com"}}, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			h := httpsec.CSRF(tc.cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(tc.method, "/submit", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if tc.want == http.StatusForbidden && called {
				t.Fatal("handler ran for a rejected request")
			}
		})
	}
}
