// Package obs is the infodancer/web suite's opt-in observability glue: request
// correlation, HTTP failure capture, and error reporting. It is host-side
// middleware, not a component dependency. A reusable component depends only on
// its injected logger and db and logs structured ERROR at its boundary; a host
// that wants correlation and error notices wraps the mounted handlers with the
// middleware here. Nothing in this package is required to mount a component.
//
// The pieces:
//
//   - [Middleware] assigns a request id (honoring an inbound header) and stashes
//     it on the request context. [RequestID] reads it back. A component reads the
//     id through its own injectable extractor; obs never forces a component to
//     import it -- a host that uses [Middleware] can hand [RequestID] to the
//     component as that extractor.
//   - [Capture] wraps a mounted handler, recovers panics, and reports 5xx
//     responses to an [ErrorReporter] and a [Metric]. The status code is the
//     trigger, so no component has to decide what is report-worthy.
//
// See CONVENTIONS.md ("Logging and error reporting") for the full contract.
package obs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// RequestIDHeader is the HTTP header the middleware reads an inbound request id
// from and echoes the resolved id back on. It matches the de-facto standard
// used by most reverse proxies and tracing setups.
const RequestIDHeader = "X-Request-Id"

// contextKey is unexported so no other package can collide with or overwrite the
// value stored under it.
type contextKey struct{}

var requestIDKey = contextKey{}

// WithRequestID returns a copy of ctx carrying id as the request id. It is
// exported so tests and hosts with their own middleware can seed the context
// without importing the HTTP middleware.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID returns the request id carried by ctx, or "" if none is set. Its
// signature is func(context.Context) string deliberately: a host hands it to a
// component as the component's request-id extractor, so the component never
// depends on this package. An absent id yields "", and callers log an empty
// field rather than failing.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// Middleware assigns a request id to every inbound request and stashes it on the
// request context for [RequestID] to read. If the inbound request already
// carries a non-empty [RequestIDHeader] (set by a trusted upstream proxy), that
// value is reused so a single id spans the whole hop; otherwise a fresh id is
// generated. The resolved id is echoed back on the response [RequestIDHeader] so
// a client that reports "I got a 500, id abc123" maps straight to the log line.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id)))
	})
}

// newRequestID returns a 128-bit random id as 32 lowercase hex characters.
// crypto/rand.Read never returns an error on the platforms we target, but if it
// ever did we must not hand back a predictable or empty id, so we surface it as
// a sentinel the operator can spot rather than silently degrading correlation.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "rng-unavailable"
	}
	return hex.EncodeToString(b[:])
}
