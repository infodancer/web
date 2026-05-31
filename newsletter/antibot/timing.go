package antibot

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// TimingToken returns a base64-encoded Unix timestamp for the current time.
// Call this at template render time and inject the result into a hidden field.
func TimingToken() string {
	return TimingTokenWith(time.Now)
}

// TimingTokenWith returns a timing token using the provided clock function.
func TimingTokenWith(now func() time.Time) string {
	ts := now().Unix()
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatInt(ts, 10)))
}

// Timing returns middleware that rejects submissions arriving faster than
// minDuration since the timing token was generated. The token is read from
// the named form field.
//
// Missing or unparseable tokens are treated as bot submissions.
// Rejections return 200 with fake success HTML.
func Timing(fieldName string, minDuration time.Duration) func(http.Handler) http.Handler {
	return TimingWith(fieldName, minDuration, time.Now)
}

// TimingWith is like Timing but accepts an injectable clock for testing.
func TimingWith(fieldName string, minDuration time.Duration, now func() time.Time) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
				r.ParseMultipartForm(1 << 20) //nolint:errcheck,gosec // body bounded above; non-multipart parse error is expected

				token := r.FormValue(fieldName)
				if token == "" {
					log.Printf("antibot: missing timing token from %s", clientIP(r))
					rejectTiming(w)
					return
				}

				raw, err := base64.StdEncoding.DecodeString(token)
				if err != nil {
					log.Printf("antibot: invalid timing token from %s: %v", clientIP(r), err)
					rejectTiming(w)
					return
				}

				ts, err := strconv.ParseInt(string(raw), 10, 64)
				if err != nil {
					log.Printf("antibot: unparseable timing token from %s: %v", clientIP(r), err)
					rejectTiming(w)
					return
				}

				elapsed := now().Sub(time.Unix(ts, 0))
				if elapsed < minDuration {
					log.Printf("antibot: timing check failed from %s (elapsed %s < %s)",
						clientIP(r), elapsed.Round(time.Millisecond), minDuration)
					rejectTiming(w)
					return
				}

				// Guard against tokens from the far future (clock skew or forged).
				if elapsed < 0 {
					log.Printf("antibot: future timing token from %s", clientIP(r))
					rejectTiming(w)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func rejectTiming(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, `<p class="error">Something went wrong. Please try again.</p>`)
}
