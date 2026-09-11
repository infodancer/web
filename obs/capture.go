package obs

import (
	"io"
	"net/http"
	"runtime/debug"

	"github.com/felixge/httpsnoop"
)

// CaptureOptions configures [Capture]. The zero value is usable: with no
// Reporter or Metric, capture still recovers panics and serves a clean 500, it
// just records nothing.
type CaptureOptions struct {
	// Component names the mounted component these handlers belong to (e.g.
	// "blog"). It labels notices and metrics so a shared log/metric stream can
	// be split by component. Empty is allowed but discouraged.
	Component string

	// Reporter records captured failures. Defaults to [NopReporter].
	Reporter ErrorReporter

	// Metric observes every request. Defaults to [NopMetric].
	Metric Metric
}

// Capture wraps a mounted component handler with the suite's failure-capture
// boundary. It:
//
//   - recovers any panic, logs nothing itself (the host's recovery logging, if
//     any, is separate), serves a 500, and reports the panic with its stack;
//   - observes every request on the [Metric] so a 5xx-rate alert can fire off it;
//   - reports any 5xx response (panic or plain) to the [ErrorReporter].
//
// The status code is the trigger, so no component decides what is report-worthy.
// Capture sits outside [Middleware]; compose them as Middleware(Capture(h, opts))
// so a recovered panic still has a request id to correlate by.
//
// Capture does not log the request itself. The access log is
// github.com/infodancer/logging/httplog, which every server wires in whether or
// not a proxy sits in front of it; Capture adds the failure detail on top.
func Capture(next http.Handler, opts CaptureOptions) http.Handler {
	reporter := opts.Reporter
	if reporter == nil {
		reporter = NopReporter{}
	}
	metric := opts.Metric
	if metric == nil {
		metric = NopMetric{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var (
			status int
			wrote  bool
		)

		// httpsnoop.Wrap reproduces whichever optional interfaces w implements
		// (Flusher, Hijacker, ReaderFrom, Pusher) on the wrapper. A hand-written
		// wrapper that embeds http.ResponseWriter keeps the method set of the
		// interface, not of the value, so every one of those is hidden from the
		// handler -- and nothing fails loudly: streaming stops flushing, an
		// upgrade cannot hijack, a file copy leaves the sendfile path.
		sw := httpsnoop.Wrap(w, httpsnoop.Hooks{
			WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
				return func(code int) {
					if wrote {
						// net/http ignores a superfluous WriteHeader; swallow it
						// here too so the recorded status is the one sent.
						return
					}
					status, wrote = code, true
					next(code)
				}
			},
			Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
				return func(b []byte) (int, error) {
					if !wrote {
						status, wrote = http.StatusOK, true
					}
					return next(b)
				}
			},
			ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
				return func(src io.Reader) (int64, error) {
					if !wrote {
						status, wrote = http.StatusOK, true
					}
					return next(src)
				}
			},
		})

		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				// If the handler already wrote a header before panicking we
				// cannot change the status, but we still record 500 as the
				// effective failure. If nothing was written, send a 500 now.
				if !wrote {
					sw.WriteHeader(http.StatusInternalServerError)
				}
				metric.ObserveRequest(opts.Component, r.Method, status)
				reporter.Report(r.Context(), Notice{
					RequestID: RequestID(r.Context()),
					Component: opts.Component,
					Method:    r.Method,
					Path:      r.URL.Path,
					Status:    http.StatusInternalServerError,
					Panic:     rec,
					Stack:     stack,
				})
				return
			}

			// A handler that returns having written nothing still answered 200:
			// net/http sends it on the way out. Recording 0 would put a status
			// no server ever sent onto the metric.
			if !wrote {
				status = http.StatusOK
			}

			metric.ObserveRequest(opts.Component, r.Method, status)
			if status >= 500 {
				reporter.Report(r.Context(), Notice{
					RequestID: RequestID(r.Context()),
					Component: opts.Component,
					Method:    r.Method,
					Path:      r.URL.Path,
					Status:    status,
				})
			}
		}()

		next.ServeHTTP(sw, r)
	})
}
