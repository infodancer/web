# Security policy

## Reporting a vulnerability

This repository is currently private. If you have access to it and have
found a vulnerability, email matthewjayhunter@gmail.com with the subject
line `[timeline security] <short description>`. Use PGP if you'd like —
keys on keys.openpgp.org under that address.

> If this repository later becomes public, this section will be updated
> to lead with GitHub's [private vulnerability reporting][gh-pvr]
> (`https://github.com/infodancer/timeline/security/advisories/new`)
> with email as a fallback.

[gh-pvr]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

I'll acknowledge within a few days and aim to ship a fix within two
weeks for confirmed issues. This is a personal project maintained on
nights and weekends; please don't expect commercial-grade response
times, but I will take credible reports seriously.

## In scope

- The HTTP server in `internal/web/`: authentication and authorization
  on write endpoints, IDOR on read endpoints for non-public timelines,
  SSRF or path traversal in any handler that resolves slugs to files,
  template injection in user-supplied event/article content.
- The CUE sync pipeline in `internal/sync/`: bypass of CUE schema
  validation before reaching the database, path traversal during
  directory walk, denial-of-service via crafted CUE inputs.
- The API key system in `db/queries/` and the auth middleware: timing
  attacks on key comparison, scope confusion (timeline-scoped key
  accepted as global), persistence of plaintext keys, weakness in the
  SHA-256-hashed storage scheme.
- The image storage in `internal/images/`: file-type confusion, write
  outside the configured `IMAGE_DIR`, content-type or filename
  injection in served responses.
- Markdown rendering for event articles: XSS via the rendered HTML,
  unsafe link targets, sanitizer bypass.

## Out of scope

- Issues that require an attacker to already have database access, file
  system access on the host, or possession of a valid global API key.
- Vulnerabilities in upstream dependencies (`pgx`, `goose`, `sqlc`,
  `htmx`, `cuelang`) — please report those to their respective
  upstreams. I track CVE advisories and will bump dependencies.
- Misconfiguration in the operator's deployment (publicly exposed
  Postgres, leaked API keys in logs or env, missing TLS, missing
  reverse-proxy authentication on admin endpoints).
- Denial-of-service through legitimate but expensive queries on
  large datasets (search across 15k events, etc.) — performance is
  tracked as functional bug reports, not security issues.
- Privacy implications of operating a public timeline that includes
  personal data about real people. The application has no opinions
  about what is appropriate to publish; that's the operator's call.
