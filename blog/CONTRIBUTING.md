# Contributing

This is a personal-portfolio module, not a general-purpose OSS CMS. PRs welcome but design scope is intentionally narrow — please read [DESIGN.md](DESIGN.md) first.

## Status

Pre-implementation. No build commands yet; they'll land with v0.1 implementation.

## Conventions (when code lands)

- Apache-2.0 license; new files don't need per-file headers (repo-level LICENSE covers them).
- `gofmt`, `go vet`, `golangci-lint`, `govulncheck` all clean.
- Tests against real Postgres via testcontainers; build tag `integration` for the expensive tests.
- Markdown rendering pipeline identical to [`github.com/matthewjhunter/faq`](https://github.com/matthewjhunter/faq) (goldmark + bluemonday). See faq's `internal/render` for the reference implementation; this module copies until the two converge enough to justify extracting to a shared package.

## Reporting issues

GitHub Issues for non-security; see [SECURITY.md](SECURITY.md) for security-relevant reports.
