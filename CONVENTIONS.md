# Conventions — infodancer/web

This is the shared standard for every component in the monorepo. The point of
keeping the components together is that they feel like one product when several
of them are mounted into the same site: same tool choices, same mount shape,
same database posture. New components and changes to existing ones follow these
conventions.

## Toolchain

- **Go** `1.26.3` (suite standard). Bump in lockstep across modules.
- **Build tool**: [Task](https://taskfile.dev/). Root `Taskfile.yml` fans out
  across modules; module-specific tasks live in the module's own `Taskfile.yml`.
- **Lint**: `golangci-lint` v2.10.1 (pinned in CI). Build it with a Go toolchain
  at least as new as the module's `go` directive.
- **Vulnerabilities**: `govulncheck` on every module.
- **Migrations**: [goose](https://github.com/pressly/goose). Each component owns
  a **distinct goose version table** (`<component>_goose_db_version`) so multiple
  components can run their migrations into one database without colliding.
- **SQL codegen** (Postgres components): [sqlc](https://sqlc.dev/) with `pgx/v5`.

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
- **Host owns the connection**: a component core never imports a database
  driver. It uses `database/sql` + goose and takes a `*sql.DB` from the host,
  which opens one pool (with the pgx stdlib driver) and passes the handle to
  each component. Cores expose `New(db)` to build a store and `Migrate(ctx, db)`
  to apply their migrations.
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

## Presentation

Page chrome comes from [`infodancer/ui`](https://github.com/infodancer/ui).
Components render their own fragments and let the host wrap them in `ui` chrome,
so a site built from several components looks like one product.

## Versioning

Path-prefixed tags per module (`search/vX.Y.Z`). Suite-wide `v0.N.X`: the minor
`N` is a human decision (never bumped without approval); patch `X` auto-increments
at tag time. All modules stay pre-1.0 until production-ready.
