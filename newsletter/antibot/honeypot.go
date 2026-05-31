// Package antibot provides composable anti-bot protections for newsletter
// subscription forms. Each protection is either HTTP middleware or a pure
// validation function. Stdlib-only — no external dependencies.
package antibot

import (
	"log"
	"net/http"
)

// Honeypot returns middleware that rejects requests where the named hidden
// form field is non-empty. Bots that auto-fill all fields will populate
// the honeypot; real users won't see it (it's hidden via CSS).
//
// Rejections return 200 with fakeHTML to avoid revealing detection to bots.
// The rejection is logged server-side.
func Honeypot(fieldName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
				r.ParseMultipartForm(1 << 20) //nolint:errcheck,gosec // body bounded above; non-multipart parse error is expected
				if val := r.FormValue(fieldName); val != "" {
					log.Printf("antibot: honeypot field %q filled by %s", fieldName, clientIP(r))
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					_, _ = w.Write([]byte(`<p class="success">Thanks for subscribing!</p>`))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
