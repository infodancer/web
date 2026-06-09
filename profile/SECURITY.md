# Security policy

## Reporting a vulnerability

This repository is currently private. If you have access to it and have
found a vulnerability, email matthewjayhunter@gmail.com with the subject
line `[profile security] <short description>`. Use PGP if you'd like --
keys on keys.openpgp.org under that address.

> profile lives in the [infodancer/web](https://github.com/infodancer/web)
> monorepo. If that repository later becomes public, this section will be
> updated to lead with GitHub's [private vulnerability reporting][gh-pvr]
> with email as a fallback.

[gh-pvr]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

I'll acknowledge within a few days and aim to ship a fix within two
weeks for confirmed issues. This is a personal project maintained on
nights and weekends; please don't expect commercial-grade response
times, but I will take credible reports seriously.

## In scope

- The HTTP handlers in `internal/handler/`: a user writing another user's
  profile, the CSRF guard failing open, handle-claim races defeating
  uniqueness, or reserved handles becoming claimable.
- Content injection: user-authored bio markdown or link labels/URLs
  surviving into HTML unsanitized (the bio pipeline is
  `infodancer/ui/markdown`'s comment preset; links must be absolute
  http/https and render with `rel="me nofollow ugc"`).
- The identicon endpoint: seed content leaking into the served SVG, or the
  SVG executing script when navigated to directly (it is served with
  `Content-Security-Policy: default-src 'none'` and nosniff).
- Avatar upload: bypassing the media pipeline's re-encode/metadata-strip,
  or storing a URL the user does not control the image for.

## Defense-in-depth practices in this codebase

- No login flow, no credentials: identity arrives through the host's
  resolver; write routes require a non-empty user key.
- All user-authored text renders through html/template escaping; bio
  markdown additionally passes the audited bluemonday pipeline.
- Same-origin CSRF guard on every state-changing route, failing closed
  when neither Origin nor Referer is present.
- Form bodies are size-capped; avatar bytes are size- and pixel-capped by
  the media pipeline.
