# infodancer/blog

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

A reusable Go module providing a database-backed blog / lightweight CMS for consumer sites in the infodancer / matthewjhunter web portfolio. Full-UI module: ships templates, CSS, and the authoring UX. Builds on three sibling modules — `infodancer/markdown` (render + sanitize), `infodancer/mdedit` (the editor component), and `infodancer/ui` (design tokens).

> Private module, maintained for sites in the infodancer / matthewjhunter portfolio. Consumed by tagged version and vendored, not distributed publicly. See [SECURITY.md](SECURITY.md) for vulnerability reporting.

## Status

**Pre-implementation.** The repo currently contains [DESIGN.md](DESIGN.md) and the standard scaffolding. No Go code yet, no migrations, no templates. Implementation begins once the design is reviewed and the first integration (osg session-notes) is ready to drive it.

## What it is

A database-backed blog/CMS designed to replace GitHub-plus-Hugo as the authoring workflow for narrative content on portfolio sites. The motivating use case is the **osg session-notes workflow**: campaign players need to author session reports from their character's POV, and `git push`-based workflows are unworkable for non-developer contributors. blog gives them a real CMS.

Full-UI module that mounts into a host Go service under a base path (e.g., `/blog` at osg). Consumer sites supply auth via a `UserResolver`, a database connection, and an optional `infodancer/ui` skin overlay. Markdown rendering/sanitizing is delegated to `infodancer/markdown`, and the authoring editor to `infodancer/mdedit` — blog reimplements neither.

## Quickstart

Pre-implementation; quickstart sketched in DESIGN.md §3 and will be finalized with the v0.1 implementation.

## Design

[DESIGN.md](DESIGN.md) is the source of truth: scope, data model, authoring UX, HTTP surface, templates and theming (`infodancer/ui` consumption), Hugo-bundle migration path, milestones.

## License

Apache-2.0. See [LICENSE](LICENSE).
