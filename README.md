# infodancer/web

Reusable web components for infodancer sites. Each component is a self-contained
Go module that a website mounts in-process — a set of HTTP handlers, its own
database migrations, and a small mount API — so that several sites share one
implementation instead of each reinventing search, notifications, contact
forms, and the like.

This repository is a **multi-module monorepo**: the modules live side by side so
they share conventions, tooling, and dependency choices, and so they can be
tested together against one database. The repository root is not itself a Go
module; `go.work` ties the modules together for local development only. CI
builds each module independently against its own `go.mod`.

## Modules

| Module | Path | Backend | Purpose |
|--------|------|---------|---------|
| [search](search/) | `github.com/infodancer/web/search` | Postgres + pgvector | Full-text + semantic search over a site's content, with reranking. |
| [notify](notify/) | `github.com/infodancer/web/notify` | Postgres | Notification emission and delivery. |
| [contact](contact/) | `github.com/infodancer/web/contact` | Postgres | Contact-form intake with anti-abuse and email/SMTP delivery. |
| [newsletter](newsletter/) | `github.com/infodancer/web/newsletter` | Postgres | Newsletter subscription management (Buttondown / Listmonk backends). |
| [blog](blog/) | `github.com/infodancer/web/blog` | — | Blog component (design stage). |
| [integration](integration/) | `github.com/infodancer/web/integration` | Postgres | Cross-component tests against one shared database. |

Every component is backed by **PostgreSQL**, so a site mounting several of them
runs one database — one pool, one backup, one migration story — and the
`integration` module can exercise them together. The component cores stay
driver-agnostic (`database/sql` + goose); the host opens one pool and passes the
handle to each component (see [CONVENTIONS.md](CONVENTIONS.md)).

## Foundation

These components build on [`infodancer/ui`](https://github.com/infodancer/ui)
for shared page chrome and the audited Markdown pipeline. `ui` is a separate
repository: things a component imports for presentation live there; the
components themselves — which sites import — live here.

## Development

Each module uses [Task](https://taskfile.dev/). From the repository root:

```bash
task build       # build every module
task test        # test every module
task lint        # lint every module
task all         # build, lint, vulncheck, test across every module
```

Run module-specific tasks (sqlc generation, migrations, local servers) from
within the module directory.

## Versioning

Modules are tagged independently with path-prefixed tags
(`search/vX.Y.Z`, `notify/vX.Y.Z`, …), following the suite-wide `v0.N.X` scheme.
