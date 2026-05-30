# Security Policy

## Reporting a vulnerability

Use GitHub's [private vulnerability reporting](https://github.com/infodancer/faq/security/advisories/new)
for any security-relevant issue. Do **not** open a public issue or PR.

We aim to acknowledge reports within 7 days and ship a fix or mitigation within
30 days for high-severity issues.

## Scope

In scope:

- The library code in this repository.
- The embedded templates and SQL migrations.

Out of scope:

- Vulnerabilities in dependencies (report to upstream; we'll bump versions as
  appropriate after upstream fixes ship).
- Misconfiguration by a consuming application (e.g. a host site that fails to
  set `Secure` on cookies, or that exposes admin endpoints unauthenticated).
- Content authored or imported by an operator. Imported content remains under
  its own source license and is the operator's responsibility to surface with
  correct attribution.

## Defense-in-depth practices in this codebase

- All SQL via sqlc; no string concatenation.
- HTML output through `html/template` + `bluemonday` sanitization.
- Per-row source attribution preserved from import through render.
- CSRF on every state-changing endpoint (configurable).
- `govulncheck` run on every CI build.
