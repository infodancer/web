# Timeline Tool

Interactive web timeline tool for tracking complex events over extended timeframes. PostgreSQL-backed, supports massive event counts (tested to hundreds, designed for 15k+), with a REST API and CUE data format.

## Tech Stack

- **Go** backend with `jackc/pgx`, `pressly/goose`, `sqlc`
- **PostgreSQL** database
- **HTML templates + htmx + vanilla JS/CSS** frontend
- **CUE** for data schemas, validation, and wire format
- **Taskfile** for dev automation

## Key Commands

- `task dev` -- run the dev server
- `task sync DIR=/path/to/data` -- sync CUE directory tree into database
- `task test` -- run integration tests (requires Docker for testcontainers)
- `task sqlc` -- regenerate sqlc after query changes
- `go run ./cmd/timeline --create-key "label"` -- bootstrap an API key
- `go run ./cmd/timeline --sync /path/to/data` -- sync from CUE tree

## Architecture

- `cmd/timeline/` -- entry point
- `internal/web/` -- HTTP server, handlers, templates, static assets, CUE schemas
- `internal/sync/` -- CUE directory tree sync tool
- `db/` -- migrations, sqlc queries, generated code, and a WWI sample
  dataset in `db/seed/`

## Data Model

Events, people, categories, tags, sources, event-to-event links, event-to-person links. Each event has a date, title, summary, article (markdown), category assignment, tags, and cross-references to people/events/sources.

## API

REST API under `/timeline/api/`:
- `/timeline/api/timelines` -- timeline CRUD
- `/timeline/api/timelines/{slug}/events` -- event CRUD, search (`?q=`), date lookup (`?date=`)
- `/timeline/api/timelines/{slug}/people` -- people CRUD, search (`?q=`)
- Plus categories, sources, tags, links, import/export

Content negotiation: JSON (default) and CUE (`Content-Type: application/cue`, `Accept: application/cue`). CUE input is validated against schemas before database writes.

## Auth

API key auth. Bearer token on write endpoints. Keys are SHA-256 hashed, scoped per-timeline or global. Public timelines readable without auth. Bootstrap first key via `--create-key`.

## CUE Directory Sync

The sync tool reads a CUE directory tree and upserts into the database:

```
any-timeline/
  timeline.cue          # name, slug, description
  categories/*.cue      # one per category
  people/*.cue          # one per person
  events/*.cue          # one per event (date-prefixed filename)
```

Schema-owned fields are updated on conflict. DB-only content (comments, supplementary data added via web/API) is never touched. Two-pass event sync resolves cross-references by slug.

## Frontend

Central-axis column layout with category lanes. Events are compact one-line titles that expand inline on click (no page navigation). Year markers and time-gap spacing convey temporal relationships. Category column toggles, collapsible tag filter, full-text search, density minimap. Embed mode via `?embed=true`.

## Current Status

Phases 1-5 complete (foundation, people/links, search/filtering, import/export/API/CUE, auth). Phase 6 partial (cache headers done, integration tests done, sync tool done). Remaining: keyboard nav, 15k perf test, mobile polish.
