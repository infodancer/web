# Security policy

## Reporting a vulnerability

Report privately via [GitHub's private vulnerability reporting][gh-pvr]
for this repository. As a fallback, you can email
matthewjayhunter@gmail.com with the subject line `[contact security]
<short description>`. Use PGP if you'd like — keys on keys.openpgp.org
under that address.

[gh-pvr]: https://github.com/infodancer/web/contact/security/advisories/new

I'll acknowledge within a few days and aim to ship a fix within two
weeks for confirmed issues. This is a personal-use module maintained on
nights and weekends; please don't expect commercial-grade response
times, but I will take credible reports seriously.

## In scope

- The HTTP handler in `handler/`: input validation, header injection,
  unintended persistence behavior, denial-of-service via oversized
  bodies, race conditions across goroutines.
- The `submissions/` store: SQL injection, dialect-mismatch corruption,
  data leakage in logs.
- The `smtp/` package: header injection in outgoing mail, TLS
  configuration weaknesses, credential handling.

## Out of scope

- Issues that require an attacker to already have shell or database
  access on the host.
- Vulnerabilities in upstream dependencies — please report those to the
  upstream project. I will track and update once fixes are available.
- Misconfiguration by the consuming application (e.g., unprotected SMTP
  credentials in environment, missing SPF/DKIM, exposing the database).
