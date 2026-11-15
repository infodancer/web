# httpsec

Shared HTTP hardening for the `infodancer/web` suite: security response
headers with a strict default Content-Security-Policy, a same-origin CSRF
guard, a request body limit, and `http.Server` defaults with graceful
shutdown. Headers, the body limit and the server are **host-side**, like
[obs](../obs/): the site that mounts the components wraps its mux with them.
`CSRF` is also imported by components, because a component that serves its own
state-changing form posts guards them itself.

Request IDs and panic recovery are in `obs` and are not repeated here.

## Use

```go
h := httpsec.Chain(mux,
	obs.Middleware,                          // request id (see obs)
	httpsec.Headers(httpsec.HeaderConfig{}), // security headers, strict CSP
	httpsec.CSRF(httpsec.CSRFConfig{}),      // same-origin check on POST/PUT/PATCH/DELETE
	httpsec.BodyLimit(1<<20),                // 1 MiB
)

srv := httpsec.NewServer(":8080", h, httpsec.ServerConfig{ErrorLog: slogErrorLog})

ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
ln, err := net.Listen("tcp", srv.Addr)
if err != nil { /* ... */ }
err = httpsec.Serve(ctx, srv, ln, 15*time.Second)
```

`Chain` puts the first middleware outermost.

## The default CSP

Everything from `'self'` only, images also from `data:`, no inline script or
style, no plugins, no framing, no `<base>` or form retargeting:

```
default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:;
font-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'self';
object-src 'none'; frame-ancestors 'none'
```

A site that needs another origin adds it by name, so every relaxation is
visible in its own code:

```go
policy := httpsec.DefaultPolicy().
	Allow("script-src", "https://stats.example.com").
	Allow("connect-src", "https://stats.example.com")
mw := httpsec.Headers(httpsec.HeaderConfig{Policy: policy})
```

`Allow` appends (and drops a lone `'none'`), `Set` replaces. Both panic on a
source containing `;`, `,` or whitespace, so a configuration value cannot
inject a directive. Reaching for `'unsafe-inline'` is possible and is a code
smell worth a comment. Per-response nonces are not supported yet.

### Rolling a policy onto a live site

Tightening a CSP on a site that already ships inline scripts, inline styles or
third-party assets will break pages, and nothing outside a browser tells you
which. Run it as report-only first:

```go
mux.Handle("POST /csp-report", httpsec.ReportHandler(logger)) // outside the CSRF guard

httpsec.Headers(httpsec.HeaderConfig{
	CSPMode:   httpsec.CSPReportOnly, // blocks nothing; reports what it would block
	ReportURI: "/csp-report",
})
```

Browsers then POST each would-be violation to `/csp-report`, and `ReportHandler`
logs it as one WARN line (`csp violation`, with the document, directive, blocked
URI and source file). Fix what shows up, or `Allow` it deliberately, then drop
`CSPMode` to enforce. `CSPOff` sends no CSP at all and keeps the other headers.

`ReportHandler` is public, so it treats every field as hostile: values are cut
to 300 bytes with control characters replaced (a report cannot forge or split a
log line), the body is capped at 16 KiB, and a token bucket (a burst of 50,
20 a second sustained) drops the excess while still answering 204. It accepts
both `application/csp-report` and `application/reports+json`. Mount it outside
`CSRF`: browsers send reports without a reliable `Origin`.

A handler may overwrite any header for its own response, for example to send
`default-src 'none'; sandbox` on user-supplied files. HSTS is two years with
`includeSubDomains`; `preload` is opt-in.

### Changing a default

The default policy is shared with smoke. This package decides what a site sends and `github.com/infodancer/smoke` decides what a probe expects. Neither controls the other and they must stay in sync (the site template runs smoke against a server built on these defaults, so drift fails a test). Adding a restriction or loosening one is decided from first principles, not by whichever side says so. A site conforms to the defaults unless a specific exception is made for that site or route, with the reason written next to it.

The defaults are strict on purpose, and changing one is a deliberate, visible act at one of two scopes.

Site-wide: set the field on `HeaderConfig` (`PermissionsPolicy`, `FrameOptions`, `ReferrerPolicy`).

One route: wrap it with `Override`, inside `Headers`.

```go
cam := httpsec.Override(map[string]string{"Permissions-Policy": "camera=(self), microphone=(self)"})
mux.Handle("GET /call", cam.Wrap(callHandler))
```

An override replaces the whole value for that header; nothing is merged, so the exception is written out in full. It panics at startup on a header that is not overridable (HSTS and the CSP are not), a misspelled name, an empty value or a control character.

`HeaderConfig.Expected()` returns the exact headers the middleware sends, name to value, and `Override.Expect()` returns the same form for a route. They are the values a checker needs to verify the live headers.

## CSRF

For every method except GET, HEAD and OPTIONS, the `Origin` header (or the
`Referer`, when `Origin` is absent) must name the request's own host. Missing
headers, `Origin: null`, an unparseable value, or a URL with userinfo get a
403. `TrustedOrigins` admits extra hosts, and `Disable` turns the guard off for
a host that enforces CSRF another way. It backs up a `SameSite=Lax` session
cookie; it does not replace one.

## Server defaults

`NewServer` sets `ReadHeaderTimeout` 10s, `ReadTimeout` 15s, `WriteTimeout`
30s, `IdleTimeout` 120s and `MaxHeaderBytes` 256 KiB. Go sets none of these by
default. `Serve` stops accepting when the context ends, gives in-flight
requests the grace period, then force-closes and returns an error wrapping
`context.DeadlineExceeded`.

See [SECURITY.md](SECURITY.md) for scope and reporting.
