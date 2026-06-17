package obs

import "context"

// Notice is a single captured HTTP failure handed to an [ErrorReporter]. It
// carries enough to record and correlate the failure without the reporter
// needing access to the request: the [Request id] joins it to the structured
// ERROR log the component already emitted.
type Notice struct {
	// RequestID is the correlation id from [RequestID]; "" if no middleware
	// seeded one.
	RequestID string

	// Component names the mounted component the failing handler belongs to
	// (e.g. "blog"), taken from [CaptureOptions.Component].
	Component string

	// Method and Path are the request method and URL path.
	Method string
	Path   string

	// Status is the HTTP status written by the handler (>= 500 for a reported
	// failure), or 500 when a panic was recovered.
	Status int

	// Panic is the recovered panic value, or nil if the failure was a plain
	// 5xx response.
	Panic any

	// Stack is the captured goroutine stack at the point of a recovered panic,
	// or nil for a plain 5xx response.
	Stack []byte
}

// ErrorReporter records a captured HTTP failure. Implementations collect and
// record -- persist a notice row, emit an in-app event -- but never page: alert
// routing is a metric-driven, out-of-process concern (Prometheus + Alertmanager)
// so no reporter hand-rolls alert throttling. Report must be safe for concurrent
// use and must not block the request: do the durable work quickly or hand off to
// a buffer. A nil [CaptureOptions.Reporter] disables reporting; callers should
// use [NopReporter] rather than a nil interface to avoid nil checks.
type ErrorReporter interface {
	Report(ctx context.Context, n Notice)
}

// NopReporter is an [ErrorReporter] that does nothing. It is the default when a
// host wires no reporter, so capture still recovers panics and serves a clean
// 500 -- it just records nothing beyond what the component logged.
type NopReporter struct{}

// Report implements [ErrorReporter].
func (NopReporter) Report(context.Context, Notice) {}
