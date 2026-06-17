# obs

Opt-in observability glue for the `infodancer/web` suite: request
correlation, HTTP failure capture, and the error-reporting seam. This is
**host-side middleware, not a component dependency**. A reusable component
depends only on its injected logger and db and logs structured `ERROR` at its
boundary; a host that wants correlation and error notices wraps the mounted
handlers with the middleware here.

Nothing in this package is required to mount a component. A third party can
mount `blog` or `profile` with no knowledge of `obs` and get full structured
logs. `obs` is what *your* host site adds on top.

See the suite [CONVENTIONS.md](../CONVENTIONS.md) "Logging and error
reporting" section for the contract this implements.

## The pieces

```
Middleware( Capture( componentHandler, opts ) )
   │              │
   │              └─ recovers panics, serves a clean 500, and reports any
   │                 5xx to an ErrorReporter + observes every request on a
   │                 Metric. The status code is the trigger -- no component
   │                 decides what is "report-worthy".
   │
   └─ assigns a request id (reuses an inbound X-Request-Id, else generates
      one), stashes it on the context, echoes it on the response. Compose it
      outside Capture so a recovered panic still has an id to correlate by.
```

| Symbol | Purpose |
|---|---|
| `Middleware(h)` | assign/propagate a request id; echo it on the response |
| `RequestID(ctx) string` | read the id back; `""` if unset. Hand this to a component as its request-id extractor so the component never imports `obs` |
| `WithRequestID(ctx, id)` | seed an id without the HTTP middleware (tests, custom middleware) |
| `Capture(h, CaptureOptions)` | recover panics, serve 500, report 5xx, observe requests |
| `ErrorReporter` / `Notice` | the record-a-failure seam; `NopReporter` is the default |
| `Metric` | the alerting seam (one observation per request); `NopMetric` is the default |

## What it is not

- **Not a logger.** Components log through their injected `*slog.Logger`. `obs`
  correlates and captures; it does not replace structured logging.
- **Not an alerter.** `Metric` is where a host plugs in a counter
  (Prometheus, OTel, statsd); the operator alert fires off that metric out of
  process. Throttling and routing live in the alerting tool (e.g.
  Alertmanager), never here.
- **Not a reporter implementation.** `ErrorReporter` is an interface. The
  suite's Postgres-backed `error_notices` reporter and `notify`-feed emission
  are separate, host-side code that implement it.

## Usage

```go
// In the host, wiring a mounted component:
h := blog.New(cfg).Handler() // cfg.RequestID = obs.RequestID

wrapped := obs.Middleware(obs.Capture(h, obs.CaptureOptions{
    Component: "blog",
    Reporter:  myReporter, // implements obs.ErrorReporter; defaults to no-op
    Metric:    myMetric,   // implements obs.Metric; defaults to no-op
}))
mux.Handle("/blog/", http.StripPrefix("/blog", wrapped))
```

A component reads the id with the extractor the host injected:

```go
logger := s.log.With("request_id", s.requestID(r.Context()))
```

If the host wires no middleware, the extractor returns `""` and the field is
simply empty -- the component still works.
