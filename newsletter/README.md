# newsletter

A small Go toolkit for newsletter subscription on low-volume sites:
a subscribe handler, a PostgreSQL subscriber store, thin
clients for [Listmonk](https://listmonk.app/) and
[Buttondown](https://buttondown.email/), and a few composable anti-bot
middlewares.

> Maintained as a personal utility for my own sites. Issues and PRs are
> welcome but I may not respond quickly. See [SECURITY.md](SECURITY.md)
> for vulnerability reporting.

```
import (
    "github.com/infodancer/web/newsletter/antibot"
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

mux := http.NewServeMux()
mux.Handle("POST /subscribe",
    antibot.Honeypot("website")(
        antibot.Timing("ts", 3*time.Second)(h),
    ),
)
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
- **`antibot/`** — Composable middlewares: `Honeypot` (hidden field),
  `Timing` (token-stamped submit), `IPRateLimiter` (per-IP sliding
  window), and `ValidateMX` (domain has MX records). Each is independent
  — pick the ones you want.
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

None of these are strong by themselves. Stacked, they catch most casual
bots without bothering real users with a CAPTCHA:

```go
mux.Handle("POST /subscribe",
    antibot.Honeypot("website")(
        antibot.Timing("ts", 3*time.Second)(
            rateLimiter.Middleware()(h),
        ),
    ),
)
```

- **Honeypot** — hidden field that bots fill and humans don't.
  Rejections return `200` with the success fragment so bots don't learn.
- **Timing** — submission must arrive at least N seconds after render.
  Token is a base64-encoded Unix timestamp injected at template time.
- **IPRateLimiter** — sliding-window per-IP counter with background
  cleanup. Call `Stop()` on shutdown.
- **ValidateMX** — call from your handler before accepting an email;
  rejects domains with no MX records.

## Status

v0.x. The API is stable enough for my own use across several sites; I
reserve the right to break it if I find a better shape. Pin a version
if that matters to you.

## License

Apache-2.0 — see the repository [LICENSE](../LICENSE).
