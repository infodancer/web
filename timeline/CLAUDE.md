# CLAUDE.md — timeline

Guidance for Claude Code working in the `timeline` module of `infodancer/web`.

## What this is

`timeline` is a **mountable full-UI component** in the infodancer/web suite — an
interactive event-timeline tool a host application mounts in-process. It is not
a standalone server. Read the repo-root [CONVENTIONS.md](../CONVENTIONS.md) for
the suite-wide contract; `faq` is the closest analog and the template this
module follows.

## Mount surface (package `timeline`)

- `New(Config) (*Module, error)` — `DB` (`*pgxpool.Pool`) and `Resolver`
  (`timelineuser.UserResolver`) are required; the host owns both.
- `Module.Routes() http.Handler` — mount under `Config.BasePath` with
  `http.StripPrefix`.
- `Module.Migrate(ctx)` — embedded goose migrations in the dedicated
  `timeline_goose_db_version` table.
- `Module.Sync(ctx, dir)` — ingest a CUE directory tree.
- `Module.Import(ctx, slug, data)` — materialize the JSON interchange format
  (atomic via `store.WithTx`).

## Conventions specific to this module

- **Tables are prefixed `timeline_`** so the component shares one database with
  the rest of the suite. The sqlc model types carry the prefix (`TimelineEvent`,
  `TimelinePerson`, …).
- **Owns no auth.** Identity arrives through the host's `UserResolver`; anonymous
  reads public timelines, `Admin` gates export and non-public timelines. Never
  add a login flow or credential store here.
- **Chrome from `infodancer/ui`.** Pages render through `ui/document` (see
  `internal/render`); the host can override via `Config.Chrome`. Timeline's own
  layout CSS rides in `ExtraCSS`; htmx comes from ui's `HeadTags`.
- **Images via the `media` module.** Portraits are stored content-addressed in a
  `media.BlobStore` and served from `{BasePath}/portrait/{hash}`. Do not write
  images to the host filesystem.
- **Markdown via `infodancer/ui/markdown`.** Event articles and person bios go
  through `render.Markdown` (goldmark + bluemonday). Never cast unsanitized
  strings to `template.HTML`.
- **Links are `BasePath`-relative.** Pages use the `BaseView` envelope
  (`.BasePath`, `.Page`); partials carry a `BasePath` field; `timeline.js` builds
  fetch URLs from `data-base`. Absolute SEO links use `Config.BaseURL`.

## Layout

- `timeline.go` / `config.go` / `user.go` — mount surface and re-exports
- `internal/handler/` — HTTP layer; `templates/pages/*.gohtml`,
  `templates/partials/*.gohtml`, `static/`
- `internal/render/` — template loader + markdown
- `internal/sync/` — CUE directory importer
- `internal/transfer/` — JSON interchange + import
- `internal/images/` — portrait processing (bytes handed to `media`)
- `store/` — thin wrapper over the generated `db` package
- `db/migrations/`, `db/queries/` — goose + sqlc

## Commands

- `task check` — fmt, vet, lint, test, vulncheck (the gating set).
- `WEB_TEST_PG=postgres://… task test` — DB-backed tests take a private database
  from `pgtest` and skip when it is unset. CI sets it through the shared
  workflow's `test_postgres_env`.
- `task generate` — regenerate the `db` package via the pinned sqlc.
- Build the suite-wide tasks from the repo root (`task build` / `task test`).

## Workflow

Open a GitHub issue before a distinct change; branch `feature/<n>` or
`bug/<n>`; reference the issue URL in commits; small, logical commits; PRs merge
to `main`. Bump the Go toolchain in lockstep with the rest of the suite.
