package httpsec

import (
	"fmt"
	"net/http"
)

// BodyLimit caps request bodies at n bytes. A request that declares a larger
// Content-Length is refused with 413 before the handler runs; a body with no
// declared length (chunked) is wrapped in [http.MaxBytesReader], so the
// handler's read fails with *http.MaxBytesError at the limit. It panics if n
// is not positive: that is a configuration mistake, caught at startup.
func BodyLimit(n int64) func(http.Handler) http.Handler {
	if n <= 0 {
		panic(fmt.Sprintf("httpsec: BodyLimit needs a positive limit, got %d", n))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > n {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}
