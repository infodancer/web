# newsletter

A small Go toolkit for newsletter subscription on low-volume sites:
a subscribe handler, a local subscriber store (SQLite/PostgreSQL), thin
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
    "github.com/infodancer/web/newsletter/subscribers/sqlite"
)
```

## Quick start (SQLite + Listmonk)

```go
store, _ := sqlite.Open("data/newsletter.db")
defer store.Close()

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
- **`subscribers/`** — SQLite or PostgreSQL store with embedded
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
- **`migrations/`** — Embedded SQL for SQLite and PostgreSQL. Applied
  automatically by `subscribers/sqlite`'s `Open` (SQLite); for PostgreSQL,
  run `migrations/postgres`'s embedded FS through goose yourself.

## Storage

```go
// SQLite — single file, one open connection (writer-serialized).
// Importing subscribers/sqlite registers the modernc.org/sqlite driver.
store, err := sqlite.Open("data/newsletter.db")

// PostgreSQL — bring your own driver and run the embedded migrations.
db, _ := sql.Open("pgx", os.Getenv("DATABASE_URL"))
p, _ := goose.NewProvider(goose.DialectPostgres, db, pgmigrations.FS)
if _, err := p.Up(ctx); err != nil { ... }
store := subscribers.New(db, subscribers.Postgres)
```

The core `subscribers` package imports no SQL driver. For SQLite, import
`subscribers/sqlite` (pulls `modernc.org/sqlite`); for PostgreSQL, supply
`github.com/jackc/pgx/v5/stdlib` or `github.com/lib/pq` and apply
`github.com/infodancer/web/newsletter/migrations/postgres` via goose.

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

Apache-2.0 — see [LICENSE](LICENSE).
