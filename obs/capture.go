package obs

import (
	"net/http"
	"runtime/debug"
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
		sw := &statusWriter{ResponseWriter: w}

		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				// If the handler already wrote a header before panicking we
				// cannot change the status, but we still record 500 as the
				// effective failure. If nothing was written, send a 500 now.
				if !sw.wrote {
					sw.WriteHeader(http.StatusInternalServerError)
				}
				metric.ObserveRequest(opts.Component, r.Method, sw.status)
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

			metric.ObserveRequest(opts.Component, r.Method, sw.status)
			if sw.status >= 500 {
				reporter.Report(r.Context(), Notice{
					RequestID: RequestID(r.Context()),
					Component: opts.Component,
					Method:    r.Method,
					Path:      r.URL.Path,
					Status:    sw.status,
				})
			}
		}()

		next.ServeHTTP(sw, r)
	})
}

// statusWriter wraps an [http.ResponseWriter] to record the status code and
// whether a header was written. A handler that never calls WriteHeader and just
// writes a body implicitly sends 200, which WriteHeader-on-first-Write records.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusWriter) WriteHeader(code int) {
	if s.wrote {
		return
	}
	s.status = code
	s.wrote = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if !s.wrote {
		s.WriteHeader(http.StatusOK)
	}
	return s.ResponseWriter.Write(b)
}
