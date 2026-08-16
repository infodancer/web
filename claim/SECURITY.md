# Security policy

## Reporting a vulnerability

This repository is currently private. If you have access to it and have
found a vulnerability, email matthewjayhunter@gmail.com with the subject
line `[claim security] <short description>`. Use PGP if you'd like --
keys on keys.openpgp.org under that address.

> claim lives in the [infodancer/web](https://github.com/infodancer/web)
> monorepo. If that repository later becomes public, this section will be
> updated to lead with GitHub's [private vulnerability reporting][gh-pvr]
> with email as a fallback.

[gh-pvr]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

I'll acknowledge within a few days and aim to ship a fix within two
weeks for confirmed issues. This is a personal project maintained on
nights and weekends; please don't expect commercial-grade response
times, but I will take credible reports seriously.

## What this component protects

A claim token is a bearer credential. Whoever holds one can take ownership
of the entity it names, and on most hosts ownership then permits editing
the listing and publishing contact details. Treat findings against the
token lifecycle as high severity.

The properties the component is meant to guarantee:

- **Tokens are never recoverable from the database.** Only a SHA-256 hash
  is stored, so a database read, a backup, or a SQL injection elsewhere in
  a host application does not yield working claim links.
- **Redemption is single use**, enforced in SQL rather than by a
  read-then-write in Go, so concurrent redemptions cannot both succeed.
- **Issuing a new invitation revokes the previous one** for that entity,
  and a partial unique index enforces at most one live invitation per
  entity.
- **Expiry is enforced at redemption**, not only at display.
- **Tokens carry 256 bits** from `crypto/rand` and are located by hash
  through a unique index, so there is no application-level secret
  comparison and no practical guessing attack.

## In scope

- Any way to redeem an invitation twice, redeem an expired or revoked one,
  or redeem one without possessing its token.
- Any path that causes the raw token to be persisted, logged, or returned
  by a query.
- Token generation weaknesses.
- SQL injection in the generated query layer.

## Out of scope

- Issues that require the attacker to already have database or filesystem
  access on the host.
- **The host's identity layer.** This component records whatever string
  the host passes as `consumedBy` and never authenticates anyone. Session
  security, sign-in, and the correctness of the host's admin checks are
  the host's responsibility.
- **Host obligations documented in the README**: `Referrer-Policy` on the
  claim page, keeping claim URLs out of the index, rate limiting
  redemption, not rendering a token as a clickable link, and requiring a
  verified email before acting on `PendingForEmail`. A host that skips
  these has a bug in the host.
- **Email delivery.** This component sends nothing; how an invitation
  reaches its recipient, and whether that channel is secure, is the
  host's decision.
- Vulnerabilities in upstream dependencies (`pgx`, `goose`, `sqlc`) --
  please report those to their respective upstreams. I track advisories
  and will bump dependencies.
- Misconfiguration in the operator's deployment (publicly exposed
  Postgres, missing TLS).
- Whether it is appropriate to create a listing about a particular person
  or business in the first place. The component has no opinions about
  that; it is the operator's call.
