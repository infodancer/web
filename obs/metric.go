package obs

// Metric receives one observation per request that flows through [Capture]. It
// is the alerting seam: a host backs it with whatever counter system it runs
// (Prometheus, OpenTelemetry, statsd) and the operator alert fires off that
// metric out of process. Keeping it an interface means obs forces no telemetry
// dependency on a third-party consumer, and alert throttling stays in the tool
// built for it (e.g. Alertmanager) rather than in this package.
//
// ObserveRequest is called for every request, not only failures, so a host can
// compute a 5xx rate (the natural alert) rather than just a raw error count.
// Implementations must be safe for concurrent use.
type Metric interface {
	ObserveRequest(component, method string, status int)
}

// NopMetric is a [Metric] that records nothing. It is the default when a host
// wires no telemetry.
type NopMetric struct{}

// ObserveRequest implements [Metric].
func (NopMetric) ObserveRequest(component, method string, status int) {}
