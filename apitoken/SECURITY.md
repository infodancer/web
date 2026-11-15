# Security policy

## Reporting a vulnerability

This repository is currently private. If you have access to it and have
found a vulnerability, email matthewjayhunter@gmail.com with the subject
line `[apitoken security] <short description>`. Use PGP if you'd like --
keys on keys.openpgp.org under that address.

> apitoken lives in the [infodancer/web](https://github.com/infodancer/web)
> monorepo. If that repository later becomes public, this section will be
> updated to lead with GitHub's [private vulnerability reporting][gh-pvr]
> with email as a fallback.

[gh-pvr]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

I'll acknowledge within a few days and aim to ship a fix within two
weeks for confirmed issues. This is a personal project maintained on
nights and weekends; please don't expect commercial-grade response
times, but I will take credible reports seriously.

## What this component protects

A personal access token is a bearer credential. Whoever holds one can call
the host's token-reachable API routes as the user who minted it, within the
token's scopes. Treat findings against the token lifecycle or the request
dispatch as high severity.

The properties the component is meant to guarantee:

- **Tokens are never recoverable from the database.** Only a SHA-256 hash
  is stored, so a database read, a backup, or a SQL injection elsewhere in
  a host application does not yield working tokens.
- **Tokens carry 256 bits** from `crypto/rand` and are located by hash
  through a unique index, so there is no application-level secret
  comparison and no practical guessing attack.
- **The plain text is shown once**, in the response to the mint form
  (`Cache-Control: no-store`), and is never put in a redirect URL, a flash
  message, a log line, or an error.
- **A token request never falls through to the browser handler.** A
  malformed, unknown or revoked token is a 401 even when a valid session
  cookie rides along, and the `Cookie` header is stripped before a token
  request reaches a route.
- **A token reaches only what the host registered.** Routes are an
  allow-list, each with a required scope; everything else is a 403, and a
  token cannot reach the account page that mints tokens.
- **Revocation is owner-scoped and immediate.** Revoking someone else's
  token id is indistinguishable from revoking an id that does not exist, and
  a revoked token never authenticates again.

## In scope

- Any way to authenticate with a malformed, unknown or revoked token, or
  without possessing the token.
- Any way for a token to reach a route that was not registered for it, or
  one whose scope it lacks.
- Any way for a token request to be authenticated by a cookie, or to fall
  through to the host's browser handler.
- Any path that causes the plain token to be persisted, logged, cached, or
  returned by a query other than the one mint response.
- Revoking or listing another user's tokens.
- Token generation weaknesses.
- SQL injection in the generated query layer.
- Cross-site request forgery or injection against the account page.

## Out of scope

- Issues that require the attacker to already have database or filesystem
  access on the host.
- **The host's identity layer.** This component records whatever string
  the host passes as the user key and never authenticates a person. Session
  security, sign-in, and the correctness of the host's `Authenticated`,
  `CurrentUser` and permission checks are the host's responsibility.
- **Host obligations documented in the README**: mounting `Dispatch`
  outermost, registering only API routes (never the account page), calling
  `DeleteForUser` when an account is deleted, and gating who may manage
  tokens. A host that skips these has a bug in the host.
- **Token expiry.** Tokens do not expire; one lives until its owner revokes
  it. This is a known limitation, not a vulnerability.
- **Rate limiting** of requests that carry a token. A host that needs it
  applies it in front of `Dispatch`.
- Vulnerabilities in upstream dependencies (`pgx`, `goose`, `sqlc`) --
  please report those to their respective upstreams. I track advisories
  and will bump dependencies.
- Misconfiguration in the operator's deployment (publicly exposed
  Postgres, missing TLS).
