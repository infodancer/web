# Security policy

## Reporting a vulnerability

This repository is currently private. If you have access to it and have
found a vulnerability, email matthewjayhunter@gmail.com with the subject
line `[paging security] <short description>`. Use PGP if you'd like --
keys on keys.openpgp.org under that address.

> paging lives in the [infodancer/web](https://github.com/infodancer/web)
> monorepo. If that repository later becomes public, this section will be
> updated to lead with GitHub's [private vulnerability reporting][gh-pvr]
> with email as a fallback.

[gh-pvr]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

I'll acknowledge within a few days and aim to ship a fix within two
weeks for confirmed issues. This is a personal project maintained on
nights and weekends; please don't expect commercial-grade response
times, but I will take credible reports seriously.

## In scope

- **Integer handling in `ParseParams` and `Paginate`:** a crafted
  `?offset=` or `?size=` that escapes the clamps (negative, overflow, or
  a value that reaches the underlying query as an unbounded limit) and
  turns a page request into a full-table scan or out-of-range read.
- **The over-fetch contract:** any input that makes `Paginate` return
  the sentinel row to the caller (a cross-page data leak) or miscompute
  `HasNext`/`HasPrev`.

## Out of scope

- The caller's `fetch` closure and the SQL it runs. This package never
  builds a query or touches a database; it passes a clamped `limit` and
  `offset` to code the host owns. SQL injection, row-level authorization,
  and query cost live in that closure, not here.
- Denial of service from a host that ignores the clamped `PageSize` and
  fetches an unbounded result anyway.
