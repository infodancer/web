# Security policy

## Reporting a vulnerability

This repository is currently private. If you have access to it and have
found a vulnerability, email matthewjayhunter@gmail.com with the subject
line `[timeline security] <short description>`. Use PGP if you'd like —
keys on keys.openpgp.org under that address.

> timeline lives in the [infodancer/web](https://github.com/infodancer/web)
> monorepo. If that repository later becomes public, this section will be
> updated to lead with GitHub's [private vulnerability reporting][gh-pvr]
> with email as a fallback.

[gh-pvr]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

I'll acknowledge within a few days and aim to ship a fix within two
weeks for confirmed issues. This is a personal project maintained on
nights and weekends; please don't expect commercial-grade response
times, but I will take credible reports seriously.

## In scope

- The HTTP handlers in `internal/handler/`: authorization gaps on
  admin-only routes (export), IDOR allowing an anonymous or non-admin
  user to read a non-public timeline, template injection in
  user-supplied event/article content.
- The auth integration in `internal/handler/auth.go`: a request reaching
  an admin route without admin rights, mis-scoped `timeline_users` sync,
  or the `sub_hash` identity key colliding across issuers.
- The CUE sync pipeline in `internal/sync/`: path traversal during the
  directory walk, denial-of-service via crafted CUE inputs.
- Portrait handling via the `media` module: file-type confusion or
  content-type/filename injection in served portrait responses.
- Markdown rendering of event articles and person bios through
  `infodancer/ui/markdown`: XSS via the rendered HTML, unsafe link
  targets, sanitizer bypass.

## Out of scope

- Issues that require an attacker to already have database access or file
  system access on the host.
- Anything in the host application's identity layer: timeline trusts the
  `UserResolver` the host supplies and the `Admin` flag it returns.
  Authentication itself, session security, and the correctness of the
  host's admin determination are the host's responsibility.
- Vulnerabilities in upstream dependencies (`pgx`, `goose`, `sqlc`,
  `htmx`, `cuelang`, `infodancer/ui`) — please report those to their
  respective upstreams. I track CVE advisories and will bump dependencies.
- Misconfiguration in the operator's deployment (publicly exposed
  Postgres, missing TLS, missing reverse-proxy authentication).
- Denial-of-service through legitimate but expensive queries on
  large datasets (search across 15k events, etc.) — performance is
  tracked as functional bug reports, not security issues.
- Privacy implications of operating a public timeline that includes
  personal data about real people. The application has no opinions
  about what is appropriate to publish; that's the operator's call.
