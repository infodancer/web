# errnotice

The suite's collect-and-record half of the "beyond logs" error-reporting design
in [CONVENTIONS.md](../CONVENTIONS.md) ("Beyond logs: error notices and
alerting"). It implements [`obs.ErrorReporter`](../obs) by UPSERTing each
captured HTTP failure into a Postgres `error_notices` table, deduplicated by a
fingerprint so a crash loop is one row with a rising `count` -- not thousands of
rows.

```go
import "github.com/infodancer/web/errnotice"
```

## Where it sits (three tiers, dependencies pointing inward)

CONVENTIONS.md describes three tiers; this module is the third.

1. **The component (reusable).** Logger + db injected, returns an
   `http.Handler`, logs structured `ERROR` at its boundary. Knows nothing of
   reporters, `error_notices`, or this module. Mountable in any third-party
   site.
2. **Shared `obs` middleware (suite glue, optional).** Owns request-id
   assignment, the `recover()` + status-capture wrapper, the request metric, and
   the `ErrorReporter` *interface*. A 5xx or panic is the trigger -- the status
   code is ground truth, so no component decides what is "report-worthy."
3. **This reporter (ours alone).** An `ErrorReporter` that records a failure
   row. It lives in the host site and is never imported by a component.

`errnotice` is the tier-3 *table* implementation. It records only: it does not
page (alerting is a metric-driven, out-of-process concern -- Prometheus +
Alertmanager) and, for now, does not emit a `notify` event.

## What it records

One row per distinct failure in `error_notices`, keyed by a `fingerprint`:

| Column | Meaning |
|---|---|
| `fingerprint` | sha256 hex grouping key (see below); the UNIQUE dedup key |
| `request_id` | the last occurrence's correlation id (joins to the log line) |
| `component`, `method`, `path` | the failing handler, request method, last raw path |
| `status` | the captured HTTP status (>= 500, or 500 for a recovered panic) |
| `panic_value` | the recovered panic rendered to text (truncated to 1 KiB), `''` for a plain 5xx |
| `stack` | the captured goroutine stack (truncated to 8 KiB), `''` for a plain 5xx |
| `first_seen`, `last_seen`, `count` | first/last occurrence and the running count |

A repeat of the same fingerprint bumps `count` and advances `last_seen`,
refreshing the last-occurrence fields while leaving `first_seen` alone.

### The fingerprint heuristic

`fingerprint = sha256(component, method, normalizedPath, status, panicClass)`,
hex-encoded.

- **normalizedPath** collapses volatile id segments to `:id`, so `/posts/123`
  and `/posts/456` group into one row. A segment is treated as an id when it is
  all digits, a long hex string (>= 16 hex chars), or a canonical UUID.
- **panicClass** is empty for a plain 5xx (so all 5xx on the same route+status
  group together) and, for a recovered panic, the Go type of the panic value
  plus the top user stack frame (so different panics on the same route stay
  apart).

This is a grouping heuristic, not a security boundary: it only decides which
failures are "the same." See [SECURITY.md](SECURITY.md).

## Usage

The host owns the pool and the `obs` wiring. Migrate once, build a `Reporter`,
and hand it to `obs.Capture`:

```go
pool := /* the host's *pgxpool.Pool */
if err := errnotice.Migrate(ctx, pool); err != nil { /* ... */ }

reporter := errnotice.New(pool, errnotice.WithLogger(logger))

wrapped := obs.Middleware(obs.Capture(h, obs.CaptureOptions{
    Component: "blog",
    Reporter:  reporter, // implements obs.ErrorReporter
}))
mux.Handle("/blog/", http.StripPrefix("/blog", wrapped))
```

`Report` runs the UPSERT on a context detached from the request
(`context.WithoutCancel` + a 5s timeout) so a disconnected client cannot cancel
the write, and it never blocks the request meaningfully, never panics, and never
propagates an error -- a write failure is logged at `ERROR` and swallowed.

`List` (most recent N by `last_seen`) backs a future admin "recent errors" view.

## Develop

```
task generate          # regenerate db/ from db/queries.sql via sqlc (after schema/query edits)
task test              # all tests; Postgres-backed ones skip without WEB_TEST_PG
task check             # fmt, vet, lint, test, vulncheck
```

## Status

Pre-1.0 (tag `errnotice/vX.Y.Z`). `New`/`WithLogger`/`Migrate`, the `Reporter`,
and the `error_notices` schema are the public surface.

## License

Apache-2.0.
