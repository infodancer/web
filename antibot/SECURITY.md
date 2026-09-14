# Security policy

## Reporting a vulnerability

This repository is currently private. If you have access to it and have
found a vulnerability, email matthewjayhunter@gmail.com with the subject
line `[antibot security] <short description>`. Use PGP if you'd like --
keys on keys.openpgp.org under that address.

> antibot lives in the [infodancer/web](https://github.com/infodancer/web)
> monorepo. If that repository later becomes public, this section will be
> updated to lead with GitHub's [private vulnerability reporting][gh-pvr]
> with email as a fallback.

[gh-pvr]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

I'll acknowledge within a few days and aim to ship a fix within two
weeks for confirmed issues. This is a personal project maintained on
nights and weekends; please don't expect commercial-grade response
times, but I will take credible reports seriously.

## In scope

- **Token forgery or replay:** any way to produce a token `Check` accepts
  without the secret, to reuse one past `MaxAge`, to use one issued for a
  different form, or to backdate one past `MinFill`.
- **Limiter bypass:** any way for one client to obtain more than the configured
  budget -- concurrent submissions racing the counter, an `X-Forwarded-For`
  believed from an untrusted peer, IPv6 addresses within one /64 counted
  separately, or case or whitespace splitting an email's budget.
- **Identifiers in the limiter table:** an IP address, email or account id
  stored, or recoverable without the secret, in `antibot_counters`.
- **Account budget bypass:** an empty or omitted account id on a signed-in
  form landing in a shared bucket, or skipping the account limit, rather than
  failing.
- **Failing open:** any store failure that results in an allowed verdict.
- **Leaks through logs:** a token, or a secret-derived value, written to the log.

## Out of scope

- **Volumetric denial of service.** antibot bounds what a submission costs the
  site; it is not a DDoS defence. That belongs at the edge.
- **Distributed abuse within the limits:** many IPs each submitting many
  different addresses, or a person filling the form by hand. The limits make
  this slow and expensive, not impossible.
- **Host misconfiguration:** a `Proxies` list naming an untrusted peer, a weak
  or committed `Secret`, a form rendered without the token or honeypot, or a
  handler that ignores the verdict.
- **What a consumer does with an allowed submission.** Sanitizing its content
  and delivering its email are the consumer's job.
