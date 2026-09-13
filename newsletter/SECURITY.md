# Security policy

## Reporting a vulnerability

Report privately via [GitHub's private vulnerability reporting][gh-pvr]
for this repository. As a fallback, you can email
matthewjayhunter@gmail.com with the subject line `[newsletter security]
<short description>`. Use PGP if you'd like — keys on keys.openpgp.org
under that address.

[gh-pvr]: https://github.com/infodancer/web/newsletter/security/advisories/new

I'll acknowledge within a few days and aim to ship a fix within two
weeks for confirmed issues. This is a personal-use module maintained on
nights and weekends; please don't expect commercial-grade response
times, but I will take credible reports seriously.

## In scope

- The HTTP handler in `handler/`: input validation, the mail-domain check
  it makes when no antibot guard is set, unintended persistence behavior, denial-of-service via oversized bodies, race
  conditions across goroutines.
- The `subscribers/` store: SQL injection, dialect-mismatch corruption,
  data leakage in logs.
- The `buttondown/` and `listmonk/` clients: credential handling,
  request smuggling, TLS configuration weaknesses.

## Out of scope

- Issues that require an attacker to already have shell or database
  access on the host.
- Vulnerabilities in upstream dependencies — please report those to the
  upstream project. I will track and update once fixes are available.
- Misconfiguration by the consuming application (e.g., exposed API
  keys, missing reverse-proxy hardening, public database).
- The strength of any individual anti-bot mechanism in isolation. They
  are designed to be stacked; none is sufficient alone, and none is
  claimed to defeat a determined attacker.
