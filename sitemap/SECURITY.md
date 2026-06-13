# Security policy

## Reporting a vulnerability

This repository is currently private. If you have access to it and have
found a vulnerability, email matthewjayhunter@gmail.com with the subject
line `[sitemap security] <short description>`. Use PGP if you'd like --
keys on keys.openpgp.org under that address.

> sitemap lives in the [infodancer/web](https://github.com/infodancer/web)
> monorepo. If that repository later becomes public, this section will be
> updated to lead with GitHub's [private vulnerability reporting][gh-pvr]
> with email as a fallback.

[gh-pvr]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

I'll acknowledge within a few days and aim to ship a fix within two
weeks for confirmed issues. This is a personal project maintained on
nights and weekends; please don't expect commercial-grade response
times, but I will take credible reports seriously.

## In scope

- **Part-number handling in `servePart`:** a crafted `/{name}/{n}`
  segment (negative, overflow, non-numeric, or out of range) that escapes
  the bounds check and reaches a source's page window as an unclamped
  offset/limit, or that addresses a part beyond the computed count.
- **URL construction in `joinURL` / `selfStem`:** an `Entry.Loc` or
  configured prefix that produces a malformed or origin-crossing `<loc>`
  in served XML (e.g. a leaf advertising URLs outside its `BaseURL`).
- **XML output integrity:** any entry value that breaks well-formedness or
  injects markup into the emitted `<urlset>` / `<sitemapindex>` (the
  package relies on `encoding/xml` escaping -- a bypass is in scope).

## Out of scope

- The content a `Source` chooses to emit. This package serializes the
  `Entry` values a source yields; whether a URL should be public, and
  whether it is reachable, is the source's and the host's decision. The
  forced-inclusion gate is a CI aid, not a runtime authorization control.
- Denial of service from a `Source` that streams an unbounded set with no
  `CacheTTL` configured. Cache policy is the host's choice per leaf.
- robots.txt, sitemap submission, and search-engine registration -- this
  package only serves the XML.
