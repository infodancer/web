# CLAUDE.md

Guidance for Claude Code when working in the **notify** module.

## What this is

A reusable notification system for infodancer websites. Sites record events
(e.g. account creation) to Postgres; admins see a bell + feed, users (later) a
personalized subset. See `DESIGN.md` for architecture and `README.md` for the
integration surface. Coding standards are in `CONVENTIONS.md`.

## Hard rules

- **Identity is opaque.** Actors/subjects are `(issuer, subject)` strings with a
  point-in-time `label`. Never add a foreign key from a notify table into a host
  table — it would break the self-contained schema.
- **The producer must never block or fail the host's request path.** The default
  emitter is async best-effort. Keep it that way unless the user explicitly opts
  into the sync/transactional path.
- **Migrations are append-only and namespaced.** New schema goes in a new
  `migrations/NNNNN_*.sql`; the goose history lives in `notify_goose_db_version`.

## Workflow

- Module path `github.com/infodancer/web/notify`, Go 1.26.6, part of the parent
  `go.work`.
- After editing `db/queries.sql` or `migrations/`, run `task generate` to
  regenerate the `db` package (sqlc), then `task check`.
- `task test` is unit-only (no Docker). Integration tests need Docker and the
  `integration` build tag: `task test:integration`.
- Per infodancer conventions: GitHub issue before branching
  (`feature/<issue>` or `bug/<issue>`), commits reference the issue, PRs merge
  to main.
