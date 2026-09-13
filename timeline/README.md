# timeline

An interactive event-timeline component for [infodancer/web](https://github.com/infodancer/web)
sites: a host mounts it in-process, and it serves an htmx UI for densely
populated, multi-category timelines over extended timeframes. Postgres-backed,
designed to scale to 15,000+ events per timeline, with a CUE-based data format
suited to version-controlled research.

> Personal project, currently kept private. Published as a courtesy if you
> stumbled onto this — no support promises, the API and schema are still moving.
> See [SECURITY.md](SECURITY.md) if you find something exploitable.

## What it's for

Most timeline tools assume a small handful of events on a flat axis. This one is
built for densely populated, multi-category timelines where the interesting work
is in the cross-references: events linked to other events, people who appear in
multiple events, sources cited across many events. The data model and the UI
both prioritize that web of relationships.

## Mount contract

timeline is a library, not a server. A host application owns the database pool
and identity, and mounts the module under a path it chooses:

```go
m, err := timeline.New(timeline.Config{
    DB:       pool,          // *pgxpool.Pool, owned by the host
    Resolver: resolver,      // timelineuser.UserResolver — host identity
    BasePath: "/timeline",   // mount prefix
    Blobs:    blobStore,     // media.BlobStore for portraits (optional)
})
if err != nil { /* ... */ }
if err := m.Migrate(ctx); err != nil { /* ... */ }

mux.Handle("/timeline/", http.StripPrefix("/timeline", m.Routes()))
```

- **`New(Config)`** builds the module; only `DB` and `Resolver` are required.
- **`Routes()`** returns the `http.Handler` to mount under `BasePath`.
- **`Migrate(ctx)`** applies the embedded migrations (own goose version table).
- **`Sync(ctx, dir)`** ingests a CUE directory tree (below).
- **`Import(ctx, slug, data)`** materializes the JSON interchange format,
  atomically.

The module owns no login flow: anonymous visitors read public timelines;
non-public timelines and the JSON export are gated to admins, where "admin" is a
flag on the host-resolved user. Page chrome comes from
[`infodancer/ui`](https://github.com/infodancer/ui); the host can inject its own
via `Config.Chrome`. Portrait images are stored through the
[`media`](https://github.com/infodancer/web/tree/main/media) module.

## Tech stack

- Go (`jackc/pgx`, `pressly/goose`, `sqlc`)
- PostgreSQL (tables prefixed `timeline_`)
- HTML templates + [htmx](https://htmx.org/) + vanilla JS/CSS, themed via
  `infodancer/ui`
- [`infodancer/ui/markdown`](https://github.com/infodancer/ui) for article/bio
  rendering (goldmark + bluemonday)
- [CUE](https://cuelang.org/) for the directory-sync data format
- [Taskfile](https://taskfile.dev/) for dev automation

No JavaScript bundler, no frontend framework, no ORM. The server renders HTML;
htmx swaps fragments.

## Layout

- `timeline.go`, `config.go`, `user.go` -- the mount surface (package `timeline`)
- `internal/handler/` -- HTTP handlers, templates (`pages/`, `partials/`), static
- `internal/render/` -- template loader + markdown pipeline
- `internal/sync/` -- CUE directory-tree importer (`Module.Sync`)
- `internal/transfer/` -- JSON interchange format + import (`Module.Import`)
- `internal/images/` -- portrait processing (bytes handed to `media`)
- `store/` -- thin wrapper over the generated `db` package
- `db/migrations/`, `db/queries/` -- goose migrations and sqlc SQL

## Data model

Events, people, categories, tags, sources, and cross-reference links
(event↔event, event↔person). Each event has a date, title, summary, a markdown
article body, a category assignment, tags, and references to people and sources.

## CUE directory sync

`Module.Sync(ctx, dir)` reads a directory tree of CUE files and upserts it:

```
any-timeline/
  timeline.cue              # name, slug, description
  categories/*.cue          # one per category
  people/*.cue              # one per person (sibling .jpg → portrait)
  events/YYYY-MM-DD-*.cue   # one per event
```

Schema-owned fields are updated on conflict. A two-pass approach resolves
event→event and event→person references by slug. See `docs/cue-format.cue` for
the expected shape.

## Frontend

Central-axis column layout with per-category lanes. Events are single-line cards
that expand inline on click — no page navigation in the common reading path.
Year markers and time-gap spacing convey relative time. Toggle category
visibility, collapse the tag filter, search across all fields, and use the
density minimap to jump around. Embed mode (`?embed=true`) strips chrome for
iframe use.

## Development

```bash
task check                                    # fmt, vet, lint, test, vulncheck
WEB_TEST_PG=postgres://… task test            # DB tests (skip without the DSN)
task generate                                 # regenerate the db package via sqlc
```

## License

Apache-2.0 — see [LICENSE](LICENSE). Vulnerability reporting in
[SECURITY.md](SECURITY.md).
