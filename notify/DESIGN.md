# notify — design

## Purpose

Give infodancer websites a way to record events they generate (often from user
actions) and surface them — first to **admins** (visibility is the primary
goal), later to **users** (retention is secondary). The system must be reusable
across sites with minimal wiring, render with HTMX on top of `infodancer/ui`,
and offer a low-requirements producer that any site can call.

First consumer: **osg** (`old-school-gamers/osg`).

## Key decisions

1. **Embedded library, per-site database.** Like every other infodancer module,
   notify runs in-process. Its tables live in the host's own Postgres, migrated
   under a dedicated `notify_goose_db_version` table so they coexist with the
   host schema. There is no standalone ingest service and no shared cross-site
   database. Central/cross-site visibility, when wanted, is an **optional
   outbound webhook** (phase 3): each site forwards events to a collector.

2. **Async best-effort delivery.** `Emit` enqueues to a bounded channel drained
   by a background worker; it never blocks the request path. On a full buffer it
   logs, counts a drop, and returns nil. A crash with buffered events loses
   them. This is deliberate: a dropped admin notification must never fail an
   account creation. A `NewSyncEmitter` escape hatch exists for the rare event a
   host wants written in its own transaction. There is no outbox in v1.

3. **Typed core columns + JSONB payload.** The columns the admin feed filters
   and sorts on (`type`, `source`, `severity`, `audience`, `actor_*`, `title`,
   `created_at`) are typed; everything site-specific goes in a free-form `JSONB`
   `payload`. Payload is not indexed in v1 (add a GIN index only if a real query
   pattern justifies it); keep `Title` informative so the feed is useful without
   payload queries.

4. **Opaque identity, no host foreign keys.** Actors and subjects are recorded
   as `(issuer, subject)` strings with a point-in-time `label` cache — never a
   foreign key into a host table. This keeps the schema self-contained.

## Schema (phase 1)

- `notify_events` — append-only log. `id BIGINT GENERATED ALWAYS AS IDENTITY`,
  the typed columns above, `payload JSONB`, `created_at` (DB clock). Indexed by
  `(audience, created_at DESC, id DESC)` for the feed, plus `type` and `source`.
  CHECK constraints pin `severity` and `audience` to their domains.
- `notify_admin_read_cursor` — one row per admin identity (`issuer`, `subject`,
  `last_seen_at`). Not one row per event.

### Unread without a row per event per admin

Admins see ~everything, so a recipient row per (event × admin) is wasteful. The
bell uses a **monotonic cursor**:

- unread = `count(*) WHERE audience='admin' AND created_at > last_seen_at`
- mark-seen advances `last_seen_at` to the newest *rendered* event's timestamp
  via `greatest(...)`, so a stale request cannot move the cursor backwards and
  events arriving between render and click are not silently swallowed.

Tradeoff: a cursor gives an accurate count and a "what's new since" line but no
per-event read state. That is correct for an admin firehose. Users (phase 4)
get a different model — explicit `notify_user_notifications` rows with per-event
`read_at`/`dismissed_at`, since they receive only a subset.

## Pagination

Keyset over `(created_at DESC, id DESC)` via `Filter.BeforeID` (rows strictly
older than that id), not OFFSET — the feed is append-heavy and OFFSET drifts.
`Filter.Limit` is clamped to `[1, 100]` (default 25).

## Package layout

```
notify (root)   Event, Identity, Severity, Audience, Emitter, Store, Migrate
  db/           sqlc-generated query layer (package db) + queries.sql
  migrations/   goose SQL, embedded
  cmd/notifyctl operator CLI (migrate / emit-test / feed-dump)
```

The producer is the root package so a host imports `github.com/infodancer/web/notify`
and gets the small `Emitter`/`Event` surface; the admin UI, webhook, user, and
email layers arrive as opt-in subpackages so a producer-only consumer pulls
minimal dependencies. The generated `db` package never imports the root, so
there is no import cycle; the root maps domain types to/from generated types.

## Host integration contract

- Host supplies a `*pgxpool.Pool`; notify owns its own tables and migrations.
- Host calls `Migrate` once at startup and constructs an emitter, calling
  `Emit` at each event point and the returned `drain` on shutdown.
- Phase 2 admin UI: the host gates access with its own auth and injects an
  identity resolver; notify owns no authentication.

## Risks (accepted)

- Best-effort loss is not audit-grade; the sync escape hatch is the mitigation.
- A shared cross-site DB would couple deploys; avoided by the webhook approach.
- `actor_label` is a point-in-time cache and is not authoritative.
- `source` is self-asserted by the producer; validate non-empty.
