# CLAUDE.md

Guidance for Claude Code sessions in `github.com/infodancer/web/blog`.

## What this module is

A reusable Go module providing a database-backed, multi-author blog — the
authoritative editorial voice of a consumer site. Full-UI module: ships
templates, CSS, and the authoring UX. See [DESIGN.md](DESIGN.md) for the v0.1
design; once code lands, design + code together are authoritative.

Key shape: named blogs (distinct editorial streams in one instance),
invitation-only authorship via `infodancer/authz` role grants, RSS/Atom/JSON
Feed at combined/per-author/per-blog scope with autodiscovery, JSON-LD, and
host-wired hooks into the suite's `profile`, `media`, and `search` components.

## Primary consumer

**osg's editorial voice** — announcements, dev notes, OSR commentary written
by the site maintainers. Not the session-notes feature (that shipped
separately inside osg's own server).

## Sister modules

- [`infodancer/ui`](https://github.com/infodancer/ui) — chrome (`ui/document`),
  design tokens (`--app-*`), `ui/markdown` (the sanitization boundary), and
  `ui/mdedit` (the editor component).
- [`infodancer/authz`](https://github.com/infodancer/authz) — role store;
  `blogauthz` mirrors faq's `faqauthz` adapter. Roles: `blog`/`author`,
  `blog`/`admin`.
- Monorepo siblings `profile`, `media`, `search` — consumed through
  host-injected hooks (`AuthorDirectory`, upload via `media.Ingest`,
  `Indexer`), never imported for search/profile. `faq` and `profile` are the
  pattern parallels for structure, CSRF, templates, and tests.

## What NOT to change without explicit approval

- **License** is Apache-2.0.
- **Module mount contract** (`New(Config) → Module → Routes() → Migrate(ctx)`).
- **`blog_*` table prefix** and the dedicated `blog_goose_db_version` table.
- **Sanitization boundary**: only `ui/markdown` output may become
  `template.HTML`.

## Conventions

- Follow the monorepo [CONVENTIONS.md](../CONVENTIONS.md): Task, golangci-lint,
  govulncheck, goose, sqlc + pgx/v5, Postgres only, host owns the pool.
- Tests: handler unit tests on an in-memory fake store; Postgres integration
  tests live in the monorepo `integration` module (WEB_TEST_PG).
- No frontend framework. htmx + vanilla JS; templates consume `infodancer/ui`
  tokens through a `--blog-*` chain.
