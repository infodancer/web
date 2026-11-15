package httpsec_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/infodancer/web/httpsec"
)

// The zero HeaderConfig must keep enforcing: existing callers get no change.
func TestCSPModeDefaultsToEnforce(t *testing.T) {
	h := serve(httpsec.Headers(httpsec.HeaderConfig{})(okHandler())).Header()
	if h.Get("Content-Security-Policy") == "" {
		t.Error("the default mode no longer enforces a CSP")
	}
	if h.Get("Content-Security-Policy-Report-Only") != "" {
		t.Error("the default mode also sent a report-only header")
	}
}

// Report-only lets a live site learn what a strict policy would block before
// it blocks anything.
func TestCSPReportOnly(t *testing.T) {
	h := serve(httpsec.Headers(httpsec.HeaderConfig{CSPMode: httpsec.CSPReportOnly})(okHandler())).Header()
	if h.Get("Content-Security-Policy") != "" {
		t.Errorf("report-only mode enforced a CSP: %q", h.Get("Content-Security-Policy"))
	}
	if got, want := h.Get("Content-Security-Policy-Report-Only"), httpsec.DefaultPolicy().String(); got != want {
		t.Errorf("report-only header = %q, want the policy %q", got, want)
	}
	// Everything else still applies: only the CSP is softened.
	for _, name := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Strict-Transport-Security", "Permissions-Policy"} {
		if h.Get(name) == "" {
			t.Errorf("report-only mode dropped %s", name)
		}
	}
}

func TestCSPOff(t *testing.T) {
	h := serve(httpsec.Headers(httpsec.HeaderConfig{CSPMode: httpsec.CSPOff})(okHandler())).Header()
	if h.Get("Content-Security-Policy") != "" || h.Get("Content-Security-Policy-Report-Only") != "" {
		t.Errorf("CSPOff still sent a CSP: %v", h)
	}
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") == "" {
		t.Error("CSPOff dropped the other headers")
	}
}

func TestCSPReportURI(t *testing.T) {
	for _, mode := range []httpsec.CSPMode{httpsec.CSPEnforce, httpsec.CSPReportOnly} {
		h := serve(httpsec.Headers(httpsec.HeaderConfig{CSPMode: mode, ReportURI: "/csp-report"})(okHandler())).Header()
		got := h.Get("Content-Security-Policy") + h.Get("Content-Security-Policy-Report-Only")
		if !strings.HasSuffix(got, "; report-uri /csp-report") {
			t.Errorf("mode %v: policy = %q, want it to end with the report-uri", mode, got)
		}
	}
}

func TestCSPReportURIIsAbsentByDefault(t *testing.T) {
	got := serve(httpsec.Headers(httpsec.HeaderConfig{})(okHandler())).Header().Get("Content-Security-Policy")
	if strings.Contains(got, "report-uri") {
		t.Errorf("a report-uri appeared with none configured: %q", got)
	}
}

func TestCSPReportURIRejectsInjection(t *testing.T) {
	for _, bad := range []string{"/r; script-src *", "/r\nX-Evil: 1", "/r r2", "/r,/s"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("ReportURI %q accepted, want a panic at configuration time", bad)
				}
			}()
			httpsec.Headers(httpsec.HeaderConfig{ReportURI: bad})
		}()
	}
}

// A CSP mode only changes the CSP header; a handler may still replace it.
func TestReportOnlyHandlerMayStillOverride(t *testing.T) {
	h := httpsec.Headers(httpsec.HeaderConfig{CSPMode: httpsec.CSPReportOnly})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy-Report-Only", "default-src 'none'")
	}))
	if got := serve(h).Header().Get("Content-Security-Policy-Report-Only"); got != "default-src 'none'" {
		t.Errorf("handler override lost: %q", got)
	}
}
