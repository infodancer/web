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
- **Postgres components** (`search`, `notify`) are the ones that share one
  database today; `integration/` exercises them together.
- **Driver isolation**: a component core never imports a database driver. The
  store interface and the dialect-specific SQL live in the core module; each
  concrete backend that pulls a driver lives in its **own nested module** at its
  natural path (`contact/submissions/sqlite`, `newsletter/subscribers/sqlite`).
  A subtree with its own `go.mod` is excluded from the parent module, so the
  driver's dependency closure (for SQLite, the heavy `modernc.org/sqlite` +
  `libc`/`memory`/`mathutil` C-port packages) is compiled only by consumers that
  choose that backend. The handler depends on a small store *interface*, so it
  is tested with an in-memory fake and pulls no driver. Nested backend modules
  carry a relative `replace` to their parent for local/CI builds while the
  parent is unpublished.
- **SQLite components** (`contact`, `newsletter`) ship a SQLite backend module
  today. Their Postgres migrations are already scaffolded (`migrations/postgres`),
  but no Postgres store implementation exists yet. Completing the Postgres
  backend, so contact submissions and newsletter state can live next to search
  and notify in one database, is an open decision, not yet made. Until it is,
  the integration suite covers only the Postgres components.

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
