# newsletter

A small Go toolkit for newsletter subscription on low-volume sites:
a subscribe handler, a PostgreSQL subscriber store, thin
clients for [Listmonk](https://listmonk.app/) and
[Buttondown](https://buttondown.email/). Abuse checks come from
[`antibot`](../antibot).

> Maintained as a personal utility for my own sites. Issues and PRs are
> welcome but I may not respond quickly. See [SECURITY.md](SECURITY.md)
> for vulnerability reporting.

```
import (
    "github.com/infodancer/web/newsletter/buttondown"
    "github.com/infodancer/web/newsletter/handler"
    "github.com/infodancer/web/newsletter/listmonk"
    "github.com/infodancer/web/newsletter/subscribers"
)
```

## Quick start (Postgres + Listmonk)

Hand the store the host's pgx connection pool:

```go
pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
if err := subscribers.Migrate(ctx, pool); err != nil { ... }
store := subscribers.New(pool)

lm := listmonk.NewClient("http://listmonk:9000")

h := handler.New(store, lm, "your-list-uuid")
h.Guard = guard // *antibot.Guard from github.com/infodancer/web/antibot

mux := http.NewServeMux()
mux.Handle("POST /subscribe", h)
mux.Handle("GET /antibot/token/newsletter", guard.TokenHandler("newsletter"))
```

## Packages

- **`handler/`** — `http.Handler` for `POST /subscribe`. Persists the
  email locally, forwards to Listmonk, returns a small HTML fragment.
- **`subscribers/`** — PostgreSQL store ([sqlc](https://sqlc.dev/)-generated
  queries in `subscribers/db`) with embedded
  [goose](https://github.com/pressly/goose) migrations. One table:
  `email`, `created_at`, `synced_at`.
- **`listmonk/`** — Posts to Listmonk's public subscription API. No-ops
  silently if the base URL is empty (handy for local dev).
- **`buttondown/`** — Posts to Buttondown's `/v1/subscribers` endpoint.
  Same no-op behavior on empty API key.
- **`antibot/`** — Deprecated, and to be removed: superseded by the
  [`antibot`](../antibot) module. The handler uses its `ValidateMX` only
  when no Guard is set.
- **`migrations/`** — Embedded PostgreSQL SQL, applied by
  `subscribers.Migrate`.

## Storage

```go
pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
if err := subscribers.Migrate(ctx, pool); err != nil { ... }
store := subscribers.New(pool)
```

The host opens one `*pgxpool.Pool` and passes it in. `Migrate` applies the
embedded migrations (tracked in a dedicated `newsletter_goose_db_version`
table so several components can share one database); the caller owns the
pool, so the store has no `Close`.

## Anti-bot

Set `Subscribe.Guard` to an [`antibot`](../antibot) guard. Before a signup is
stored it checks the honeypot, a signed timing token, per-IP and per-address
rate limits, and the address's mail domain. The form needs a hidden
`antibot_token` field filled from the token endpoint and an empty hidden
`website` field; antibot's README has the page script.

A refused signup gets `MsgRejected`; an address whose domain takes no mail gets
`MsgInvalidDomain`; a honeypot trip gets `MsgSuccess` and nothing is stored; a
guard that cannot reach its database gets `MsgError`, and the signup is refused
rather than let through unchecked. `Subscribe.Form` (default `newsletter`)
names the form to the guard.

Without a Guard the handler checks only that the domain has MX records, which
puts no limit on volume.

## Status

v0.x. The API is stable enough for my own use across several sites; I
reserve the right to break it if I find a better shape. Pin a version
if that matters to you.

## License

Apache-2.0 — see the repository [LICENSE](../LICENSE).
