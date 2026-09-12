# Conventions — infodancer/web

This is the shared standard for every component in the monorepo. The point of
keeping the components together is that they feel like one product when several
of them are mounted into the same site: same tool choices, same mount shape,
same database posture. New components and changes to existing ones follow these
conventions.

## Toolchain

- **Go** `1.26.6` (suite standard). Bump in lockstep across modules, and update
  this line in the same change -- the `go` directive in every `go.mod` is the
  source of truth, and these docs have drifted from it three times (#86, #111).
  `git ls-files '*go.mod' | xargs grep -h '^go '` should report one version.
- **Build tool**: [Task](https://taskfile.dev/). Root `Taskfile.yml` fans out
  across modules; module-specific tasks live in the module's own `Taskfile.yml`.
- **Lint**: `golangci-lint` v2.10.1 (pinned in CI). Build it with a Go toolchain
  at least as new as the module's `go` directive. **One config governs the whole
  suite**: a single root `.golangci.yml` (golangci-lint discovers it by walking
  up from each module dir). Do not add per-module `.golangci.yml` files -- tune
  the root one so every module lints identically. The enabled set includes
  `gosec` everywhere; the rationale for any disabled rule or exclusion lives in
  that file.
- **Vulnerabilities**: `govulncheck` on every module.
- **Migrations**: [goose](https://github.com/pressly/goose). Each component owns
  a **distinct goose version table** (`<component>_goose_db_version`) so multiple
  components can run their migrations into one database without colliding.
- **SQL codegen**: [sqlc](https://sqlc.dev/) with `pgx/v5` for every component's
  queries — type-safe generated code, one way to write and review SQL. The
  generated `db` package is committed; regenerate with `sqlc generate`.

## Module shape (the mount contract)

A component is a library that a website mounts in-process. The common shape:

- A constructor that takes a config struct and a database handle.
- A method that returns the component's `http.Handler` / routes, to be mounted
  under a path the host chooses.
- A method (or CLI) that applies the component's migrations.
- No `main` for library-only components; components that ship an operator CLI
  put it under `cmd/` (`search` → `cmd/search`, `notify` → `cmd/notifyctl`).

Components never assume they own the whole site. They take their database handle
and mount path from the host.

## Database posture

- **Table prefixes**: every component prefixes its tables with the component
  name (`search_documents`, `search_chunks`, …) so a single database can host
  several components.
- **Postgres everywhere**: every component is Postgres-backed. A site mounting
  several components runs one database, and the `integration` module can test
  them side by side. There is no SQLite backend — standardizing on one engine is
  deliberate: it keeps the components consistent and removes the maintenance and
  drift cost of a second store implementation per component.
- **Host owns the pool**: every component takes a `*pgxpool.Pool` from the host,
  which opens one pool and passes that same handle to each component. Cores
  expose `New(pool)` to build a store and `Migrate(ctx, pool)` to apply their
  migrations (migrations run through `stdlib.OpenDBFromPool` internally, since
  goose speaks `database/sql`). Each component scopes its migration history to a
  dedicated `<component>_goose_db_version` table so several can share one
  database without colliding.
- **Handlers depend on interfaces, not the concrete store**: each handler
  declares the small store interface it needs (e.g. `Save`/`MarkSent`/
  `MarkFailed`), so it pulls in no driver and is tested with an in-memory fake.
  The concrete `*Store` satisfies the interface, so call sites are unchanged.

## Security

- Untrusted, user-authored content is sanitized through `infodancer/ui/markdown`
  (the single audited goldmark + bluemonday pipeline). Components store reduced
  text, never raw HTML.
- Read and write database roles are separated where a component both serves and
  ingests: the serving path uses a read-only pool.
- Components are visibly secure by design — CSRF protection on state-changing
  form posts, anti-abuse on public intake, no secrets in the repo.

## Logging and error reporting

Several components mount into one site and share a single log stream, so they
log the same way or the stream is useless. The rules:

- **`log/slog` only.** No stdlib `log`, no `fmt.Print*` for diagnostics. A
  component never calls `slog.Default()` directly in its logic -- it takes a
  `*slog.Logger` on its `Config` and falls back to `slog.Default()` once, in the
  constructor, if the host passed nil.
- **Inject, don't reach.** The host owns logger setup (handler, level, output).
  Components receive the logger through `Config`/`Deps` and pass it down to
  sub-components; no package-level or ad-hoc logger fields.
- **Log at the boundary, wrap below it.** Library and store code returns wrapped
  errors (`fmt.Errorf("component: doing x: %w", err)`) and stays silent. The
  HTTP handler -- the boundary -- is where an error gets logged, exactly once,
  then turned into a sanitized client response. Don't log-and-return the same
  error at multiple layers; that produces duplicate lines for one failure.
- **Structured fields, never interpolation.** `s.log.Error("save submission",
  "err", err, "ip", ip)`, not `log.Printf("save failed for %s: %v", ip, err)`.
  The message is a short stable operation name; everything variable is a
  key/value field so it is filterable in aggregation.
- **Levels mean something.**
  - `ERROR` -- the request failed or a write was lost; an operator needs to know.
  - `WARN` -- degraded but the request still succeeded (a best-effort sync
    failed, a confirmation email bounced, a buffer dropped an event). Comment the
    intentional swallow at the call site.
  - `INFO` -- notable operational or security events (honeypot trip, delivery
    success), not routine per-request chatter.
- **Every handler log carries the request id.** Handlers derive a scoped logger
  at entry (`logger := s.log.With("request_id", reqID)`) so every line for one
  request shares an id across components. A component reads the id from the
  request context through an injectable extractor (`func(context.Context)
  string`) defaulting to a documented key -- it never requires a specific
  middleware. If the host sets nothing, the id is empty and everything still
  works. This is the only request-scoped contract a reusable component carries.
- **Never leak internals to the client.** The client gets a generic message and,
  where practical, the request id (so "I got a 500, id abc123" maps to the log
  line). Error detail, SQL, and stack context stay in the log.
- **Sanctioned swallows are explicit.** `defer func() { _ = tx.Rollback(ctx) }()`
  and `_ = w.Write(body)` after headers are sent are fine. Any other ignored
  error needs a comment saying why.

Domain-level sentinel errors (`ErrNotFound`, `ErrSlugTaken`, ...) live in the
store package and are matched with `errors.Is`; handlers branch on them to pick
a status code rather than string-matching messages.

### Beyond logs: error notices and alerting

Logs that nobody reads do not tell a solo operator the site is throwing 500s.
Recording a failure and waking someone up about it are separate jobs with
separate mechanisms. Crucially, **none of this lives in a component.** A
reusable component depends only on its injected logger and db; it logs
structured `ERROR` at its boundary and is done. Everything below is opt-in host
glue -- a consumer who just mounts the handler gets full logs and owes nothing.

Three tiers, dependencies pointing inward only:

1. **The component (reusable).** Logger + db injected, returns an
   `http.Handler`. Logs structured `ERROR` with operation, error, and request id.
   Knows nothing of reporters, the `notify` module, `error_notices`, ntfy, or
   Prometheus. Mountable standalone in any third-party site.
2. **Shared obs middleware (suite glue, optional for outsiders).** A separate
   package -- not any component's `Config`. It owns the request-id assignment,
   the `recover()` + response-status capture that wraps a mounted handler, the
   Prometheus request counter, and the `ErrorReporter` *interface*. A 5xx or
   panic is the trigger -- the status code is ground truth, so no component has
   to decide what is "report-worthy." A consumer opts in by wrapping the
   handler; a third party can use it, substitute their own, or skip it.
3. **Suite reporter implementation (ours alone).** An `ErrorReporter` that
   UPSERTs an `error_notices` row keyed by a fingerprint
   (`hash(component, operation, normalized error)`) -- so a crash loop is one row
   with a rising count, not thousands -- and emits a `notify` event so the admin
   feed surfaces recent errors in-app. Lives in the host site, never visible to a
   component.

Alerting stays out of process entirely: the tier-2 counter feeds a Prometheus
alert on the 5xx rate, routed through Alertmanager to a push transport
(ntfy/Pushover). Throttling, grouping, and inhibition are Alertmanager's job --
nothing here hand-rolls alert rate limiting. The push says "web is 500ing"; the
`error_notices` row (joined by request id) is where you triage.

## Presentation

Page chrome comes from [`infodancer/ui`](https://github.com/infodancer/ui).
Components render their own fragments and let the host wrap them in `ui` chrome,
so a site built from several components looks like one product.

## Versioning

Path-prefixed tags per module (`search/vX.Y.Z`). Suite-wide `v0.N.X`: the minor
`N` is a human decision (never bumped without approval); patch `X` auto-increments
at tag time. All modules stay pre-1.0 until production-ready.
