package httpsec_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infodancer/web/httpsec"
)

func reportServer() (http.Handler, *bytes.Buffer) {
	var buf bytes.Buffer
	return httpsec.ReportHandler(slog.New(slog.NewJSONHandler(&buf, nil))), &buf
}

func post(h http.Handler, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/csp-report", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func logLines(buf *bytes.Buffer) []map[string]any {
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}

const legacyReport = `{"csp-report":{"document-uri":"https://example.com/page","violated-directive":"script-src-elem","effective-directive":"script-src-elem","blocked-uri":"inline","source-file":"https://example.com/page","line-number":12,"disposition":"report"}}`

func TestReportHandlerLogsALegacyReport(t *testing.T) {
	h, buf := reportServer()
	if rec := post(h, "application/csp-report", legacyReport); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	lines := logLines(buf)
	if len(lines) != 1 {
		t.Fatalf("%d log lines, want 1:\n%s", len(lines), buf)
	}
	l := lines[0]
	if l["msg"] != "csp violation" || l["level"] != "WARN" {
		t.Errorf("msg/level = %v / %v", l["msg"], l["level"])
	}
	for k, want := range map[string]string{
		"document_uri": "https://example.com/page",
		"directive":    "script-src-elem",
		"blocked_uri":  "inline",
		"disposition":  "report",
	} {
		if l[k] != want {
			t.Errorf("%s = %v, want %q", k, l[k], want)
		}
	}
}

func TestReportHandlerLogsReportingAPIBatches(t *testing.T) {
	h, buf := reportServer()
	body := `[
	  {"type":"csp-violation","url":"https://example.com/a","body":{"documentURL":"https://example.com/a","effectiveDirective":"style-src-elem","blockedURL":"https://cdn.example/x.css","disposition":"report"}},
	  {"type":"deprecation","url":"https://example.com/a","body":{"id":"x"}},
	  {"type":"csp-violation","url":"https://example.com/b","body":{"documentURL":"https://example.com/b","effectiveDirective":"img-src","blockedURL":"https://img.example/y.png","disposition":"report"}}
	]`
	if rec := post(h, "application/reports+json", body); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	lines := logLines(buf)
	if len(lines) != 2 {
		t.Fatalf("%d log lines, want the 2 csp-violation reports only:\n%s", len(lines), buf)
	}
	if lines[0]["directive"] != "style-src-elem" || lines[1]["blocked_uri"] != "https://img.example/y.png" {
		t.Errorf("fields not read from the Reporting API shape: %v", lines)
	}
}

func TestReportHandlerMethodAndContentType(t *testing.T) {
	h, buf := reportServer()
	req := httptest.NewRequest(http.MethodGet, "/csp-report", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
		t.Errorf("GET: %d, Allow %q, want 405 POST", rec.Code, rec.Header().Get("Allow"))
	}
	if rec := post(h, "text/html", legacyReport); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/html: %d, want 415", rec.Code)
	}
	if rec := post(h, "", legacyReport); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("no content type: %d, want 415", rec.Code)
	}
	if buf.Len() != 0 {
		t.Errorf("a rejected request logged something:\n%s", buf)
	}
}

func TestReportHandlerRejectsOversizeAndGarbage(t *testing.T) {
	h, buf := reportServer()
	if rec := post(h, "application/csp-report", `{"csp-report":{"blocked-uri":"`+strings.Repeat("a", 64<<10)+`"}}`); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize: %d, want 413", rec.Code)
	}
	for _, body := range []string{"", "not json", "{", "[1,2,3]", `{"csp-report": 5}`, `null`} {
		rec := post(h, "application/csp-report", body)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNoContent {
			t.Errorf("garbage %q: status %d, want 400 or 204", body, rec.Code)
		}
	}
	if strings.Contains(buf.String(), "not json") {
		t.Errorf("a malformed body was logged verbatim:\n%s", buf)
	}
}

// Report fields are attacker-controlled: anyone can POST. A value must not be
// able to forge a log line or flood one.
func TestReportHandlerSanitizesFields(t *testing.T) {
	h, buf := reportServer()
	evil := "x\n{\"level\":\"ERROR\",\"msg\":\"pwned\"}" + strings.Repeat("A", 5000)
	body, _ := json.Marshal(map[string]any{"csp-report": map[string]any{
		"blocked-uri": evil, "document-uri": "https://example.com/\x00\x1b[31mred", "violated-directive": "script-src",
	}})
	if rec := post(h, "application/csp-report", string(body)); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	lines := logLines(buf)
	if len(lines) != 1 {
		t.Fatalf("a hostile value split into %d log lines:\n%s", len(lines), buf)
	}
	if lines[0]["msg"] != "csp violation" {
		t.Errorf("a field forged the message: %v", lines[0]["msg"])
	}
	if got, _ := lines[0]["blocked_uri"].(string); len(got) > 300 || strings.ContainsAny(got, "\n\r") {
		t.Errorf("blocked_uri not truncated or still multi-line (len %d)", len(got))
	}
	if got, _ := lines[0]["document_uri"].(string); strings.ContainsAny(got, "\x00\x1b") {
		t.Errorf("control characters survived: %q", got)
	}
}

// The endpoint is public, so a flood must not become a log flood.
func TestReportHandlerRateLimits(t *testing.T) {
	h, buf := reportServer()
	for range 1000 {
		if rec := post(h, "application/csp-report", legacyReport); rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d: a dropped report must still answer 204", rec.Code)
		}
	}
	if n := len(logLines(buf)); n == 0 || n > 100 {
		t.Errorf("%d of 1000 instant reports were logged, want a small burst", n)
	}
}
