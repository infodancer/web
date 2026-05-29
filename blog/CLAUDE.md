# CLAUDE.md

Guidance for Claude Code sessions in `github.com/infodancer/web/blog`.

## What this repo is

A reusable Go module providing a database-backed blog / lightweight CMS for consumer sites in the infodancer / matthewjhunter web portfolio. Full-UI module — ships templates, CSS, and the authoring UX. See [DESIGN.md](DESIGN.md) for the v0.1 design.

**Pre-implementation.** Only [DESIGN.md](DESIGN.md) and scaffolding exist. Don't generate code until the design is reviewed.

## Primary consumer driving the design

**osg session-notes.** Campaign players (non-dev contributors) need a real CMS to author session reports from their character's POV. GitHub-plus-Hugo authoring is the wall this module exists to remove. The authoring UX is the load-bearing feature; everything else is in service of it.

## Sister modules

- [`infodancer/ui`](https://github.com/infodancer/ui) — design tokens this module consumes. Treat `--app-*` token names as a public API; chain through `--blog-*` in module CSS.
- [`infodancer/oidclient`](https://github.com/infodancer/oidclient) — consumer sites use this for OIDC; blog accepts a `UserResolver` from the consumer (same shape as faq).
- [`github.com/matthewjhunter/faq`](https://github.com/matthewjhunter/faq) — design parallel. blog's data model, mount pattern, and template shape mirror faq deliberately. Markdown rendering pipeline (goldmark + bluemonday) is identical; eventually extracted to a shared package once both have shipped.

## What NOT to change without explicit approval

- **License** is Apache-2.0. Don't change without explicit approval.
- **Module mount contract** (`New(Config) → Module → Routes() http.Handler → Migrate(ctx)`) — mirrors faq, must remain compatible with the consumer-mount pattern documented in the portfolio architecture doc.
- **`blog_*` schema-prefixed table names** — feature modules use prefixed tables so a single Postgres can host multiple modules without collision (see portfolio architecture).

## Conventions

- Apache-2.0 license, Matthew Hunter copyright. Per-file headers not required.
- Go version: track the latest patched release per the infodancer org standard.
- Markdown rendering: `goldmark` + `bluemonday`, identical to faq.
- Tests: integration tests against real Postgres via `testcontainers`, same pattern as faq.
- No frontend framework. htmx + vanilla JS where dynamic. Templates consume `infodancer/ui` tokens.

## Related docs

- [DESIGN.md](DESIGN.md) — v0.1 design proposal.
- [`infodancer/infodancer/docs/web-portfolio-architecture.md`](https://github.com/infodancer/infodancer/blob/master/docs/web-portfolio-architecture.md) — portfolio-level context.
- [`infodancer/ui/DESIGN.md`](https://github.com/infodancer/ui/blob/main/DESIGN.md) — token vocabulary blog consumes.
