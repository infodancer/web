package httpsec

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxReportBytes = 16 << 10 // a real report is well under 1 KiB
	maxFieldBytes  = 300      // each logged value
	reportBurst    = 50.0     // reports accepted at once
	reportRate     = 20.0     // sustained reports per second
)

// ReportHandler receives Content-Security-Policy violation reports, the
// legacy report-uri form (application/csp-report) and the Reporting API form
// (application/reports+json), and logs each as one structured WARN line.
//
// Anyone can POST to it, so it treats every field as hostile: values are cut
// to 300 bytes with control characters replaced, so a report cannot forge or
// split a log line; the body is capped at 16 KiB; and a token bucket drops
// reports past a small burst (still answering 204) so a flood is not a log
// flood. It changes no state beyond logging.
//
// Mount it outside the CSRF guard: browsers send reports without a reliable
// Origin header.
func ReportHandler(log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	lim := &bucket{tokens: reportBurst, last: time.Now()}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || (mt != "application/csp-report" && mt != "application/reports+json" && mt != "application/json") {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		}
		var raw json.RawMessage
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxReportBytes)).Decode(&raw); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		for _, rep := range parseReports(raw) {
			if !lim.allow() {
				break
			}
			log.LogAttrs(context.Background(), slog.LevelWarn, "csp violation",
				slog.String("document_uri", rep.documentURI),
				slog.String("directive", rep.directive),
				slog.String("blocked_uri", rep.blockedURI),
				slog.String("source_file", rep.sourceFile),
				slog.Int("line", rep.line),
				slog.String("disposition", rep.disposition),
			)
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

type report struct {
	documentURI, directive, blockedURI, sourceFile, disposition string
	line                                                        int
}

// parseReports accepts both wire formats and ignores anything else.
func parseReports(raw json.RawMessage) []report {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		if body, ok := obj["csp-report"]; ok {
			if m := asMap(body); m != nil {
				return []report{{
					documentURI: str(m, "document-uri"),
					directive:   firstNonEmpty(str(m, "effective-directive"), str(m, "violated-directive")),
					blockedURI:  str(m, "blocked-uri"),
					sourceFile:  str(m, "source-file"),
					disposition: str(m, "disposition"),
					line:        num(m, "line-number"),
				}}
			}
		}
		return nil
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	var out []report
	for _, item := range arr {
		m := asMap(item)
		if m == nil || str(m, "type") != "csp-violation" {
			continue
		}
		body := asMap(m["body"])
		if body == nil {
			continue
		}
		out = append(out, report{
			documentURI: firstNonEmpty(str(body, "documentURL"), str(m, "url")),
			directive:   firstNonEmpty(str(body, "effectiveDirective"), str(body, "violatedDirective")),
			blockedURI:  firstNonEmpty(str(body, "blockedURL"), str(body, "blockedURI")),
			sourceFile:  str(body, "sourceFile"),
			disposition: str(body, "disposition"),
			line:        num(body, "lineNumber"),
		})
	}
	return out
}

func asMap(raw json.RawMessage) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

func str(m map[string]json.RawMessage, key string) string {
	var s string
	if json.Unmarshal(m[key], &s) != nil {
		return ""
	}
	return clean(s)
}

func num(m map[string]json.RawMessage, key string) int {
	var n int
	if json.Unmarshal(m[key], &n) != nil || n < 0 {
		return 0
	}
	return n
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// clean makes an attacker-supplied value safe to log: control characters
// become '?', and it is cut to maxFieldBytes at a rune boundary.
func clean(s string) string {
	out := make([]rune, 0, len(s))
	size := 0
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == utf8.RuneError {
			r = '?'
		}
		if size+utf8.RuneLen(r) > maxFieldBytes {
			break
		}
		size += utf8.RuneLen(r)
		out = append(out, r)
	}
	return string(out)
}

// bucket is a token bucket: reportBurst tokens, refilled at reportRate a second.
type bucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func (b *bucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * reportRate
	if b.tokens > reportBurst {
		b.tokens = reportBurst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
