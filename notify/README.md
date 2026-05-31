# notify

A reusable notification system for websites in the infodancer ecosystem.

A host site records **events** — usually from user actions, such as a new
account being created — as small, flexible objects in PostgreSQL. **Admins**
(the primary audience) see them in a bell + feed; **users** (a later phase) will
see a personalized subset and opt into email per type.

Built on Go + [infodancer/ui](https://github.com/infodancer/ui) (HTMX), pgx v5,
and goose. The producer surface is intentionally tiny so dropping notify into a
site is a few lines of wiring plus one line at each event point.

## Status

| Phase | Scope | State |
|-------|-------|-------|
| 1 | Producer (`Emitter`/`Event`) + schema + store | **done** |
| 2 | Admin UI (bell + feed), mountable | **done** |
| 3 | Outbound webhook forwarder (central aggregation) | deferred |
| 4 | User UI (per-user notifications, read/dismiss) | deferred |
| 5 | Email delivery, opt-in per type | deferred |

## Producing events

```go
import "github.com/infodancer/web/notify"

// once, at startup, on the host's pgx pool:
if err := notify.Migrate(ctx, pool); err != nil { /* ... */ }
store := notify.NewStore(pool)
emitter, drain := notify.NewAsyncEmitter(store, slog.Default(), notify.AsyncOpts{Buffer: 256})
defer drain(context.Background()) // flush on shutdown

// at each event point — never blocks, never fails the user flow:
_ = emitter.Emit(ctx, notify.Event{
    Type:   "user.created",
    Source: "osg",
    Title:  "New account: " + name,
    Actor:  &notify.Identity{Issuer: iss, Subject: sub, Label: email},
    Payload: map[string]any{"handle": handle},
})
```

The default async emitter is **best-effort**: it never blocks the request path
and a delivery failure is logged, not returned. Use `NewSyncEmitter` only when
you want the write in your own request path or transaction.

## Reading (admin)

```go
feed, _ := store.AdminFeed(ctx, notify.Filter{Type: "user.created", Limit: 25})
unread, _ := store.AdminUnreadCount(ctx, admin)        // count since the admin's cursor
_ = store.AdminMarkSeen(ctx, admin, feed[0].CreatedAt) // advance the cursor (monotonic)
```

## Mounting the admin UI

The `admin` subpackage is a mountable bell + feed. It owns no authentication —
the host gates access (mount behind its own admin RBAC) and supplies a resolver
that returns the current admin identity, or `false` to deny.

```go
import "github.com/infodancer/web/notify/admin"

h, err := admin.New(admin.Config{
    Store:    store,                       // *notify.Store
    SiteName: "Old School Gamers",
    BasePath: "/admin/notifications",
    Resolver: admin.ResolverFunc(func(r *http.Request) (notify.Identity, bool) {
        id, ok := currentAdmin(r)           // host's own auth + role check
        if !ok { return notify.Identity{}, false }
        return notify.Identity{Issuer: id.Issuer, Subject: id.Subject, Label: id.Email}, true
    }),
    // Chrome (optional) injects the host's own nav/footer/meta/analytics per
    // request as a ui.DocumentData; the feed page renders through ui/document
    // so it matches the rest of the site. nil falls back to a brand chrome
    // derived from SiteName/HomeURL, so a standalone mount still renders.
    Chrome: func(r *http.Request) ui.DocumentData { return site.Chrome(r) },
})
if err != nil { /* ... */ }
mux.Handle("/admin/notifications/", http.StripPrefix("/admin/notifications", h.Routes()))
```

Routes (relative to `BasePath`): `GET /` (feed page, or a fragment for htmx
requests), `GET /bell` (self-contained unread-count anchor), `GET /bell/count`
(bare unread number), `POST /seen` (advance the read cursor), and
`GET /static/...` (bundled CSS + vendored htmx).

**The bell is the host's to place.** The feed page renders through `ui/document`,
where the host owns the nav — so notify no longer draws the bell itself. There
are two ways to embed it, matching how your nav is built:

*Self-contained anchor* — embed the `GET /bell` fragment (a full `<a>` with icon
and count) wherever your menu bar lives, refreshing on the `notify:read` event
that `POST /seen` fires (so "Mark all read" updates it live):

```html
<span id="notify-bell"
      hx-get="/admin/notifications/bell"
      hx-trigger="load, every 30s, notify:read from:body"></span>
```

*infodancer/ui nav badge* — if your nav is built from `ui.MenuItem`s, give the
bell item a `ui.Badge` whose `PollURL` points at `GET /bell/count` (the bare
number, which drops into the badge span's innerHTML). ui's `ui/nav-glyph` already
polls it on `load` and every 30s:

```go
bell.Badge = &ui.Badge{Label: "Notifications", PollURL: "/admin/notifications/bell/count"}
```

For a quick local look, `notifyctl serve` mounts the UI with auth stubbed
(**dev only** — every visitor is admin).

## CLI

```
notifyctl migrate     # apply the schema
notifyctl emit-test   # insert a sample event
notifyctl feed-dump   # print the recent admin feed as JSON
notifyctl serve       # dev-only: mount the admin UI (auth stubbed)
```

DSN via `NOTIFY_DATABASE_URL` (falling back to `DATABASE_URL`); `serve` address
via `NOTIFY_ADDR` (default `:8099`).

## Development

```
task test              # unit tests (race + goleak, no Docker)
task test:integration  # integration tests (testcontainers — needs Docker)
task generate          # regenerate db/ from db/queries.sql via sqlc
task check             # fmt, vet, lint, test, vulncheck
```

See [DESIGN.md](DESIGN.md) for architecture and rationale.
