package httpsec_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/infodancer/web/httpsec"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
}

func serve(h http.Handler) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec
}

func TestHeadersDefaults(t *testing.T) {
	rec := serve(httpsec.Headers(httpsec.HeaderConfig{})(okHandler()))
	h := rec.Header()

	if got, want := h.Get("Content-Security-Policy"), httpsec.DefaultPolicy().String(); got != want {
		t.Errorf("CSP = %q, want the default policy %q", got, want)
	}
	if got := h.Get("Strict-Transport-Security"); !strings.Contains(got, "max-age=63072000") || !strings.Contains(got, "includeSubDomains") {
		t.Errorf("HSTS = %q, want a two-year max-age with includeSubDomains", got)
	}
	if strings.Contains(h.Get("Strict-Transport-Security"), "preload") {
		t.Errorf("HSTS must not preload unless asked: %q", h.Get("Strict-Transport-Security"))
	}
	for name, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	} {
		if got := h.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got := h.Get("Permissions-Policy"); !strings.Contains(got, "camera=()") || !strings.Contains(got, "microphone=()") {
		t.Errorf("Permissions-Policy = %q, want camera and microphone denied", got)
	}
}

func TestDefaultPolicyIsStrict(t *testing.T) {
	csp := httpsec.DefaultPolicy().String()
	for _, banned := range []string{"unsafe-inline", "unsafe-eval", "*", "http:"} {
		if strings.Contains(csp, banned) {
			t.Errorf("default CSP contains %q: %s", banned, csp)
		}
	}
	for _, want := range []string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self'",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("default CSP missing %q: %s", want, csp)
		}
	}
}

func TestPolicyAllowAppendsAndDedupes(t *testing.T) {
	p := httpsec.DefaultPolicy().Allow("script-src", "https://stats.example", "https://stats.example")
	if got := p.String(); !strings.Contains(got, "script-src 'self' https://stats.example;") {
		t.Errorf("script-src not extended once: %s", got)
	}
}

func TestPolicyAllowOnNoneDropsNone(t *testing.T) {
	p := httpsec.DefaultPolicy().Allow("frame-ancestors", "https://parent.example")
	got := p.String()
	if strings.Contains(got, "frame-ancestors 'none'") {
		t.Errorf("'none' must not survive next to a real source: %s", got)
	}
	if !strings.Contains(got, "frame-ancestors https://parent.example") {
		t.Errorf("frame-ancestors not set to the allowed parent: %s", got)
	}
}

func TestPolicySetReplaces(t *testing.T) {
	p := httpsec.DefaultPolicy().Set("img-src", "'self'", "https://img.example")
	if got := p.String(); !strings.Contains(got, "img-src 'self' https://img.example") || strings.Contains(got, "data:") {
		t.Errorf("img-src not replaced: %s", got)
	}
}

func TestPolicyAllowNewDirective(t *testing.T) {
	p := httpsec.DefaultPolicy().Allow("media-src", "'self'")
	if got := p.String(); !strings.Contains(got, "media-src 'self'") {
		t.Errorf("new directive missing: %s", got)
	}
}

func TestPolicyStringIsDeterministic(t *testing.T) {
	a := httpsec.DefaultPolicy().Allow("media-src", "'self'").String()
	for range 20 {
		if b := httpsec.DefaultPolicy().Allow("media-src", "'self'").String(); a != b {
			t.Fatalf("policy order changed between calls:\n%s\n%s", a, b)
		}
	}
	if !strings.HasPrefix(a, "default-src ") {
		t.Errorf("default-src should lead the policy: %s", a)
	}
}

func TestDefaultPolicyReturnsIndependentCopies(t *testing.T) {
	a := httpsec.DefaultPolicy()
	a.Allow("script-src", "https://only-a.example")
	if strings.Contains(httpsec.DefaultPolicy().String(), "only-a.example") {
		t.Error("mutating one policy leaked into the next DefaultPolicy()")
	}
}

func TestPolicyRejectsInjection(t *testing.T) {
	for _, bad := range []string{
		"https://a.example; script-src *",
		"https://a.example\nX-Evil: 1",
		"a b",
		"",
		"https://a.example,https://b.example",
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("source %q accepted, want a panic at configuration time", bad)
				}
			}()
			httpsec.DefaultPolicy().Allow("script-src", bad)
		}()
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("directive with a semicolon accepted")
			}
		}()
		httpsec.DefaultPolicy().Allow("script-src; img-src", "'self'")
	}()
}

func TestHeadersCustomPolicy(t *testing.T) {
	cfg := httpsec.HeaderConfig{Policy: httpsec.DefaultPolicy().Allow("script-src", "https://stats.example")}
	rec := serve(httpsec.Headers(cfg)(okHandler()))
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "https://stats.example") {
		t.Errorf("custom policy not applied: %s", got)
	}
}

func TestHeadersHSTSOptions(t *testing.T) {
	off := serve(httpsec.Headers(httpsec.HeaderConfig{DisableHSTS: true})(okHandler()))
	if got := off.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS present despite DisableHSTS: %q", got)
	}
	custom := serve(httpsec.Headers(httpsec.HeaderConfig{HSTSMaxAge: 24 * time.Hour, HSTSPreload: true})(okHandler()))
	got := custom.Header().Get("Strict-Transport-Security")
	if !strings.Contains(got, "max-age=86400") || !strings.Contains(got, "preload") {
		t.Errorf("HSTS = %q, want max-age=86400 and preload", got)
	}
}

func TestHeadersHandlerMayOverride(t *testing.T) {
	h := httpsec.Headers(httpsec.HeaderConfig{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	}))
	if got := serve(h).Header().Get("Content-Security-Policy"); got != "default-src 'none'; sandbox" {
		t.Errorf("handler could not tighten the CSP for its own response: %q", got)
	}
}

func TestHeadersPresentOnErrorResponses(t *testing.T) {
	h := httpsec.Headers(httpsec.HeaderConfig{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	rec := serve(h)
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("status %d, nosniff %q: headers must ride on error responses too", rec.Code, rec.Header().Get("X-Content-Type-Options"))
	}
}

func TestChainRunsFirstMiddlewareOutermost(t *testing.T) {
	var order []string
	mk := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := httpsec.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		order = append(order, "handler")
	}), mk("a"), mk("b"), mk("c"))
	serve(h)
	if got := strings.Join(order, ","); got != "a,b,c,handler" {
		t.Errorf("order = %s, want a,b,c,handler", got)
	}
}
