# contact

A small, reusable Go HTTP handler for contact-form submissions.
Validates → persists → emails. SQLite or PostgreSQL on the back, SMTP on
the front. Designed for small, low-volume sites where reaching for a
SaaS form-builder feels excessive.

> Maintained as a personal utility for my own sites. Issues and PRs are
> welcome but I may not respond quickly. See [SECURITY.md](SECURITY.md)
> for vulnerability reporting.

```
import (
    "github.com/infodancer/web/contact/handler"
    "github.com/infodancer/web/contact/submissions/sqlite"
    "github.com/infodancer/web/contact/smtp"
)
```

## Quick start (SQLite + SMTP)

```go
store, _ := sqlite.Open("data/contact.db")
defer store.Close()

mailer := smtp.New(smtp.Config{
    Host: "smtp.fastmail.com", Port: 587,
    Username: "contact@example.org", Password: "app-password",
})

h := handler.New(store, mailer, "you@example.org", "contact@example.org")

http.Handle("/contact", h)
```

The `to` argument accepts a single address or a comma-separated list
(`"a@x,b@y"`); all addresses appear in the `To:` header. Whitespace and
empty entries are trimmed, so values pulled directly from env vars work.

A complete runnable example lives in [`example/`](example/main.go).

## What the handler does

1. Accepts `POST /contact` with form fields `name`, `email`, `subject`
   (optional), `message`, plus a hidden honeypot field (default
   `website`).
2. Validates required fields and email format (`mail.ParseAddress`).
3. Strips `\r`/`\n` from name and subject (header-injection safety).
4. Persists the submission to the configured store **synchronously**.
5. Returns a small HTML fragment to the client.
6. Sends the primary email **asynchronously** in a goroutine.
   Optionally sends a confirmation email back to the visitor.
7. Updates the submission row with `sent` / `failed` status.

The `Reply-To:` header on the outgoing email is set to the visitor's
address — you hit Reply in your email client and it goes to them.

## Storage

One table, identical schema in both backends, applied via embedded
[goose](https://github.com/pressly/goose) migrations:

| column            | notes                                             |
|-------------------|---------------------------------------------------|
| `id`              | primary key                                       |
| `name`, `email`, `subject`, `message` | the user's input          |
| `ip`, `user_agent`                    | request metadata          |
| `created_at`      | UTC timestamp                                     |
| `email_status`    | `pending` / `sent` / `failed`                     |
| `email_attempts`  | incremented on each delivery attempt              |
| `email_last_error`| last error string                                 |
| `sent_at`         | populated on first successful delivery            |

### SQLite

```go
import "github.com/infodancer/web/contact/submissions/sqlite"

store, err := sqlite.Open("data/contact.db")
```

`sqlite.Open` creates the parent directory, runs migrations, sets max
connections to 1 (SQLite is a single-writer DB). Importing the
`submissions/sqlite` subpackage registers the `modernc.org/sqlite` driver;
Postgres-only consumers import just `submissions` and avoid that dependency.

### PostgreSQL

```go
db, _ := sql.Open("pgx", os.Getenv("DATABASE_URL"))
if err := submissions.MigratePostgres(ctx, db); err != nil { ... }
store := submissions.New(db, submissions.Postgres)
```

The contact module never imports a Postgres driver directly — bring
your own (`github.com/jackc/pgx/v5/stdlib` or `github.com/lib/pq`).

## Email transport

Implementations satisfy a one-method interface:

```go
type Mailer interface {
    Send(ctx context.Context, msg handler.Message) error
}
```

The bundled `smtp` package is enough for most low-volume cases (Fastmail,
Gmail app-password, Postmark SMTP endpoint, etc.). For higher-volume or
vendor-API delivery (Postmark HTTP, SES, Mailgun), implement `Mailer`
directly using the vendor SDK.

## Spam protection

- **Honeypot** — a hidden form field that real users won't see and bots
  will fill. If non-empty, the request is silently accepted (200 with
  the success fragment) without persisting or emailing. Configure via
  `Contact.HoneypotField` (default `website`).
- **Rate limiting** is **not** built into this handler. Wrap with any
  middleware you like — `github.com/infodancer/web/newsletter/antibot`'s
  `RateLimit` works well, as does any per-IP token bucket.
- **CAPTCHA** — not bundled. If honeypot + rate limit isn't sufficient,
  Cloudflare Turnstile is a good privacy-respecting drop-in.

## Customization

Build a `*Contact`, then override fields before serving:

```go
h := handler.New(store, mailer, "to@x", "from@x")

h.HoneypotField = "honeypot"
h.MaxMessageLen = 50_000

h.MsgSuccess = `<p class="ok">Thanks!</p>`
h.MsgInvalid = `<p class="err">Please complete all fields.</p>`
h.MsgError   = `<p class="err">Try again in a minute.</p>`

h.SubjectTmpl = template.Must(template.New("s").Parse(`Site contact: {{.Subject}}`))
h.BodyTmpl    = template.Must(template.New("b").Parse(myBodyTemplate))

h.SendConfirmation     = true
h.ConfirmationFrom     = "no-reply@example.org"
h.ConfirmationSubject  = "We got your message"
h.ConfirmationBodyTmpl = template.Must(template.New("c").Parse(`Hi {{.Name}}, we'll be in touch.`))
```

Templates receive a `submissions.Submission` value.

## Graceful shutdown

`Contact` runs deliveries in goroutines. On shutdown, after
`http.Server.Shutdown` returns, call `h.Wait()` to drain in-flight sends
before exiting:

```go
srv.Shutdown(ctx)
h.Wait()
store.Close()
```

## Status

v0.1. Synchronous persist + async send. **No retry worker** —
permanently-failed sends remain in the table with `email_status='failed'`
and a non-empty `email_last_error`. v0.2 will add a worker that drains
the failed/pending backlog on a ticker. Until then, recovery means a
manual SQL query and re-send (or just glancing at the DB occasionally).

## Privacy notice

Suggested copy for a small US non-commercial site, to render below the
form on the consuming site:

> By submitting this form you're sending a message directly to me. I keep
> submissions only to make sure nothing gets lost in transit. I don't
> share, sell, or use your information for anything else.

Calibrate to your own situation; this is not legal advice.

## License

Apache-2.0 — see [LICENSE](LICENSE).
