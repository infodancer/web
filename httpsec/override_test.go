package httpsec_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infodancer/web/httpsec"
)

// Expected must describe exactly what Headers sends, so a checker given
// Expected can never disagree with the middleware.
func TestExpectedMatchesWhatHeadersSends(t *testing.T) {
	cfgs := map[string]httpsec.HeaderConfig{
		"zero":        {},
		"report-only": {CSPMode: httpsec.CSPReportOnly, ReportURI: "/csp-report"},
		"csp-off":     {CSPMode: httpsec.CSPOff},
		"no-hsts":     {DisableHSTS: true},
		"custom": {
			FrameOptions:      "SAMEORIGIN",
			ReferrerPolicy:    "no-referrer",
			PermissionsPolicy: "camera=(self)",
			HSTSPreload:       true,
		},
	}
	for name, cfg := range cfgs {
		t.Run(name, func(t *testing.T) {
			got := serve(httpsec.Headers(cfg)(okHandler())).Header()
			want := cfg.Expected()
			for k, v := range want {
				if got.Get(k) != v {
					t.Errorf("%s = %q, Expected says %q", k, got.Get(k), v)
				}
			}
			for _, k := range []string{
				"Content-Security-Policy", "Content-Security-Policy-Report-Only",
				"Strict-Transport-Security", "X-Content-Type-Options",
				"X-Frame-Options", "Referrer-Policy", "Permissions-Policy",
			} {
				if _, listed := want[k]; !listed && got.Get(k) != "" {
					t.Errorf("%s is sent (%q) but Expected omits it", k, got.Get(k))
				}
			}
		})
	}
}

func TestExpectedDefaultDeniesPowerfulFeatures(t *testing.T) {
	pp := httpsec.HeaderConfig{}.Expected()["Permissions-Policy"]
	for _, f := range []string{"camera", "microphone", "geolocation", "payment", "usb"} {
		if !strings.Contains(pp, f+"=()") {
			t.Errorf("default Permissions-Policy %q must deny %s", pp, f)
		}
	}
}

func TestOverrideReplacesOnlyThatRoute(t *testing.T) {
	cfg := httpsec.HeaderConfig{}
	mux := http.NewServeMux()
	vtt := httpsec.Override(map[string]string{"Permissions-Policy": "camera=(self), microphone=(self)"})
	mux.Handle("/vtt", vtt.Wrap(okHandler()))
	mux.Handle("/", okHandler())
	h := httpsec.Headers(cfg)(mux)

	get := func(path string) http.Header {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Header()
	}
	if got := get("/vtt").Get("Permissions-Policy"); got != "camera=(self), microphone=(self)" {
		t.Errorf("/vtt Permissions-Policy = %q, want the override", got)
	}
	if got := get("/").Get("Permissions-Policy"); got != cfg.Expected()["Permissions-Policy"] {
		t.Errorf("/ Permissions-Policy = %q, want the site default", got)
	}
	// An override leaves every other header at the site default.
	if got := get("/vtt").Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("/vtt X-Frame-Options = %q, want DENY", got)
	}
}

func TestOverrideExpectIsACopyOfWhatItSets(t *testing.T) {
	in := map[string]string{"Referrer-Policy": "no-referrer"}
	o := httpsec.Override(in)
	in["Referrer-Policy"] = "unsafe-url" // caller mutates its map afterwards
	if got := o.Expect()["Referrer-Policy"]; got != "no-referrer" {
		t.Errorf("Expect = %q, want the value captured at Override time", got)
	}
	o.Expect()["Referrer-Policy"] = "unsafe-url" // mutating the result must not leak back
	if got := o.Expect()["Referrer-Policy"]; got != "no-referrer" {
		t.Errorf("Expect result aliases internal state: %q", got)
	}
	rec := httptest.NewRecorder()
	o.Wrap(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Wrap sent %q, want no-referrer", got)
	}
}

func TestOverrideRejectsBadInput(t *testing.T) {
	bad := map[string]map[string]string{
		"unmanaged header":  {"X-Custom": "1"},
		"csp not yet":       {"Content-Security-Policy": "default-src *"},
		"hsts":              {"Strict-Transport-Security": "max-age=0"},
		"misspelled name":   {"Permission-Policy": "camera=()"},
		"empty value":       {"Permissions-Policy": ""},
		"header injection":  {"Permissions-Policy": "camera=()\r\nSet-Cookie: x=1"},
		"empty override":    {},
		"lowercase variant": {"permissions-policy": "camera=()"},
	}
	for name, m := range bad {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Override(%v) did not panic", m)
				}
			}()
			httpsec.Override(m)
		})
	}
}

func TestOverrideHandlerMayStillOverwrite(t *testing.T) {
	o := httpsec.Override(map[string]string{"X-Frame-Options": "SAMEORIGIN"})
	h := o.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want the handler's own value", got)
	}
}
