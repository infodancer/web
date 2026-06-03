# timeline

An interactive web tool for tracking complex events over extended
timeframes. Postgres-backed, designed to scale to 15,000+ events per
timeline, with a REST API and CUE-based data format suited to
version-controlled research.

> Personal project, currently kept private. Published as a courtesy if
> you stumbled onto this — no support promises, the API and schema are
> still moving, and a few rough edges remain (see "Status" below). See
> [SECURITY.md](SECURITY.md) if you find something exploitable.

## What it's for

Most timeline tools assume a small handful of events on a flat axis.
This one is built for densely populated, multi-category timelines where
the interesting work is in the cross-references: events linked to other
events, people who appear in multiple events, sources cited across many
events. The data model and the UI both prioritize that web of
relationships.

## Tech stack

- Go (`jackc/pgx`, `pressly/goose`, `sqlc`)
- PostgreSQL
- HTML templates + [htmx](https://htmx.org/) + vanilla JS/CSS
- [CUE](https://cuelang.org/) for schemas, validation, and the wire
  format
- [Taskfile](https://taskfile.dev/) for dev automation

No JavaScript bundler, no frontend framework, no ORM. Server renders
HTML; htmx swaps fragments.

## Quick start

```bash
cp .env.example .env.local
docker compose up -d                       # starts Postgres
task migrate                               # apply schema
task seed                                  # load WWI sample data
go run ./cmd/timeline --create-key admin   # bootstrap API key (save it)
task dev                                   # http://localhost:8080
```

Browse to `http://localhost:8080/t/wwi` for the sample timeline.

## Layout

- `cmd/timeline/` -- entry point and CLI flags (`--create-key`, `--sync`)
- `internal/web/` -- HTTP server, handlers, templates, static assets,
  CUE schemas
- `internal/sync/` -- CUE directory tree importer
- `internal/images/` -- portrait/image storage
- `db/migrations/` -- goose migrations
- `db/queries/` -- sqlc-managed SQL (generated code under `db/`)
- `db/seed/sample_data.sql` -- WWI sample dataset

## Data model

Events, people, categories, tags, sources, and cross-reference links
(event↔event, event↔person). Each event has a date, title, summary, a
markdown article body, a category assignment, tags, and references to
people and sources. Events can be inline-only (compact card) or
full-page with longer article + analysis fields.

## API

REST under `/timeline/api/`:

- `timelines` -- timeline CRUD
- `timelines/{slug}/events` -- event CRUD, full-text search (`?q=`),
  date lookup (`?date=`)
- `timelines/{slug}/people` -- people CRUD, search
- Plus categories, sources, tags, links, and bulk import/export

Content negotiation: JSON by default, CUE via `Content-Type:
application/cue` / `Accept: application/cue`. CUE inputs are validated
against schemas before any database write.

## Auth

API key bearer tokens on write endpoints; reads on public timelines
require no auth. Keys are SHA-256 hashed at rest, scoped either to a
single timeline or globally. Bootstrap the first key with `--create-key
<label>`.

## CUE directory sync

The importer reads a directory tree of CUE files and upserts into the
database:

```
any-timeline/
  timeline.cue              # name, slug, description
  categories/*.cue          # one per category
  people/*.cue              # one per person
  events/YYYY-MM-DD-*.cue   # one per event (date-prefixed)
```

Schema-owned fields are updated on conflict. Database-only content
(comments, user-added supplementary fields) is preserved across syncs.
A two-pass approach resolves event→event and event→person references
by slug.

## Frontend

Central-axis column layout with per-category lanes. Events are
single-line cards that expand inline on click — no page navigation in
the common reading path. Year markers and time-gap spacing convey
relative time. Toggle category visibility, collapse the tag filter,
search across all fields, and use the density minimap on the side to
jump around. Embed mode (`?embed=true`) strips chrome for iframe use.

## Status

Pre-v1. Phases 1-5 complete (foundation, people/links, search/filter,
import/export/API/CUE, auth). Phase 6 in progress (cache headers,
integration tests, sync tool done; keyboard nav, 15k-event performance
validation, and mobile polish remaining). Schema is stable enough for
my own use; expect at least one migration shape change before v1.

## License

Apache-2.0 — see [LICENSE](LICENSE). Vulnerability reporting in
[SECURITY.md](SECURITY.md).
