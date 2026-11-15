# Security policy

## Reporting a vulnerability

This repository is currently private. If you have access to it and have
found a vulnerability, email matthewjayhunter@gmail.com with the subject
line `[httpsec security] <short description>`. Use PGP if you'd like --
keys on keys.openpgp.org under that address.

> httpsec lives in the [infodancer/web](https://github.com/infodancer/web)
> monorepo. If that repository later becomes public, this section will be
> updated to lead with GitHub's [private vulnerability reporting][gh-pvr]
> with email as a fallback.

[gh-pvr]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

I'll acknowledge within a few days and aim to ship a fix within two
weeks for confirmed issues. This is a personal project maintained on
nights and weekends; please don't expect commercial-grade response
times, but I will take credible reports seriously.

## In scope

- **CSRF guard bypass:** any request that changes state and passes `CSRF`
  while its `Origin` or `Referer` names a different site, including host
  parsing tricks (userinfo, case, ports, scheme-less values).
- **CSP policy injection:** any way a value passed to `Policy.Allow` or
  `Policy.Set` can add a directive, header, or line break.
- **Weaker-than-documented defaults:** a default header, CSP directive, or
  server limit that is looser than the README states.
- **Body limit bypass:** a request body larger than the `BodyLimit` reaching
  a handler, with or without a declared `Content-Length`.
- **Report endpoint abuse:** any way a `ReportHandler` request can forge or
  split a log line, make the handler log or buffer more than its caps (300
  bytes a field, 16 KiB a body, the token bucket), or panic on a malformed
  body.
- **Shutdown correctness:** `Serve` returning before in-flight requests
  finish inside the grace period, or hanging past it.

## Out of scope

- Whether a site that relaxes the CSP (`Allow`, `Set`, `'unsafe-inline'`)
  stays safe. The defaults are strict; relaxing them is the site's decision.
- Session management, authentication, and `SameSite` cookie settings. The
  CSRF guard backs those up and does not replace them.
- Rate limiting and anti-abuse on public forms (see `antibot`).
- Request IDs, panic recovery, and error reporting (see `obs`).
- TLS termination and the trust boundary in front of the host (proxy
  headers, `X-Forwarded-*`).
