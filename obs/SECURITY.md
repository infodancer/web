# Security policy

## Reporting a vulnerability

This repository is currently private. If you have access to it and have
found a vulnerability, email matthewjayhunter@gmail.com with the subject
line `[obs security] <short description>`. Use PGP if you'd like --
keys on keys.openpgp.org under that address.

> obs lives in the [infodancer/web](https://github.com/infodancer/web)
> monorepo. If that repository later becomes public, this section will be
> updated to lead with GitHub's [private vulnerability reporting][gh-pvr]
> with email as a fallback.

[gh-pvr]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

I'll acknowledge within a few days and aim to ship a fix within two
weeks for confirmed issues. This is a personal project maintained on
nights and weekends; please don't expect commercial-grade response
times, but I will take credible reports seriously.

## In scope

- **Request-id generation:** any path through `Middleware`/`newRequestID`
  that yields a predictable, empty, or colliding correlation id, or that
  lets an attacker-controlled value reach a log/metric/notice field
  unescaped in a way that forges or splits records.
- **Inbound `X-Request-Id` trust:** the middleware reuses an inbound
  header value verbatim so a single id spans a proxy hop. A deployment
  that exposes the middleware directly to untrusted clients lets a caller
  choose its own correlation id (log spoofing / collision). Strip or
  override the header at the trust boundary if the upstream is not
  trusted.
- **Capture leaking internals:** any case where `Capture` forwards panic
  values, stack traces, or error detail to the HTTP *response* rather than
  only to the `ErrorReporter`. The client must get a generic 500; detail
  stays in the notice.

## Out of scope

- What an `ErrorReporter` or `Metric` implementation does with a `Notice`
  (persistence, notification, PII handling). Those live in the host's
  implementation, not here; this package only defines the interfaces and
  hands them a struct.
- Alert routing, throttling, and delivery. Paging an operator is a
  metric-driven, out-of-process concern (Prometheus + Alertmanager);
  this package emits the observation and nothing more.
- Denial of service from a host wiring a slow or blocking `Report`/
  `ObserveRequest` onto the request path. The contract requires those to
  be non-blocking; honoring it is the implementation's responsibility.
