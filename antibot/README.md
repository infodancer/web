# antibot

The anti-abuse check for public intake forms in the `infodancer/web` suite:
newsletter signups, contact forms, resume requests -- anything an anonymous
visitor can submit that costs the site a database row, an email, or a person's
attention.

A `Guard` checks a request the handler has already parsed and returns a
`Verdict`. It never writes the response. Each form keeps its own copy, and the
verdict only says whether the request is allowed and, if not, why.

## Use

```go
if err := antibot.Migrate(ctx, pool); err != nil { ... }

g, err := antibot.New(antibot.Config{
    DB:        pool,
    Secret:    secret,            // >= 32 random bytes, from the environment
    Proxies:   httplog.NewProxies(trusted...),
    Logger:    logger,
    RequestID: reqid.FromContext,
})

// Rendering the form: embed the token in a hidden field named "antibot_token",
// and add a hidden, empty honeypot field named "website".
data.AntibotToken = g.Token("contact")

// Handling the submission:
if err := r.ParseForm(); err != nil { ... }
v, err := g.Check(r.Context(), r, "contact", antibot.Email(r.PostForm.Get("email")))
switch {
case err != nil:
    // The limiter's store failed. Treat the submission as failed.
case v.Silent:
    // Honeypot. Render your normal success response and do nothing.
case !v.Allowed:
    // Render this form's rejection. v.Reason says why.
default:
    // Save the submission.
}
```

Run `go g.SweepEvery(ctx, time.Hour)` to delete expired rate-limit windows, or
call `g.Sweep(ctx)` from a job the host already schedules. An unswept table
grows; it never miscounts.

### Static pages

A page built ahead of time -- a Hugo site, say -- cannot embed a token, because
its HTML was generated long before the visitor arrived. Mount a token endpoint
per form and have the page fetch it when it loads:

```go
mux.Handle("GET /antibot/token/contact", g.TokenHandler("contact"))
```

```html
<input type="hidden" name="antibot_token" id="contact-token">
<script>
fetch('/antibot/token/contact', {credentials: 'same-origin'})
  .then(r => r.ok ? r.text() : '')
  .then(t => { document.getElementById('contact-token').value = t; });
</script>
```

The response is `no-store`: a token records when it was issued, so a cached one
would reach every later visitor already aged. Serving one counts nothing against
any budget. A form that depends on the script to get its token rejects a
submission from a browser with scripts disabled, as `token_missing`.

A bot can fetch a token too, wait out `MinFill`, and submit. It could do the
same with a token embedded in rendered HTML. The token raises the cost of a
submission and binds it to one form and a two-hour window; the limits are what
bound the volume.

## Checks

Check runs them cheapest first, so an early rejection costs nothing later.

| Check | Rejects | Reason |
|---|---|---|
| Honeypot | a filled hidden field, silently | `honeypot` |
| Timing token | no token; a forged, backdated, future or other-form token; faster than `MinFill`; older than `MaxAge` | `token_missing`, `token_invalid`, `too_fast`, `token_expired` |
| IP limit | more than `IPLimit` per form, per IP (IPv6 per /64) | `rate_limited` |
| Email syntax | not exactly one `@` with both sides | `invalid_email` |
| Email limit | more than `EmailLimit` per form, per address, from any IP | `rate_limited` |
| MX | a domain that does not exist, has no MX, or has a null MX | `no_mx` |

A resolver failure lets the request through and logs at warn: it is not evidence
of a bot.

Defaults: `MinFill` 3s, `MaxAge` 2h, `IPLimit` 5 an hour, `EmailLimit` 3 a day.
Set a limit to `antibot.NoLimit` to disable it.

## Design notes

- **Counters live in Postgres**, in `antibot_counters`, so a restart or a deploy
  does not reset every budget. Each submission is one atomic upsert, so
  concurrent requests cannot race past the limit. Windows are fixed, which
  allows up to twice the limit in a burst across a window boundary.
- **The table stores no identifiers.** Each key is an HMAC of the IP or email
  under the secret. The limiter needs equality, not identity.
- **A store error fails closed.** Every consumer writes its submission to the
  same database, so a form whose limiter cannot reach it could not have saved
  anyway.
- **Rejections are logged at info** with the form, reason, client IP and request
  id -- never the token, which anyone reading the log could replay.

## Testing

```
WEB_TEST_PG='postgres://user:pass@host:5432/postgres?sslmode=disable' task test
```

The checks are tested against an in-memory store and need no database. The
tests of the SQL itself -- atomic counting under concurrency, sweep, migration --
take a private database per test from `pgtest`, and skip when `WEB_TEST_PG` is
unset.
