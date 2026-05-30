# CLAUDE.md

Guidance for Claude Code when working in `infodancer/web`.

## What this is

A multi-module monorepo of reusable web components for infodancer sites:
`search`, `notify`, `blog`, `contact`, `newsletter`, plus an `integration`
module for cross-component tests. The repository root is **not** a Go module —
it holds `go.work`, shared tooling, and the component subdirectories.

Read [CONVENTIONS.md](CONVENTIONS.md) before adding or changing a component:
toolchain, the mount contract, database posture, and security expectations are
defined there and apply across all modules.

## Working in a module

`cd` into the component directory. Each module builds against its own `go.mod`
(`GOWORK=off` matches CI). Run module-specific tasks (sqlc, migrations, local
servers) from inside the module.

```bash
task build   # build every module (from the root)
task test    # test every module
task all     # build, lint, vulncheck, test
```

## Workflow

- Open a GitHub issue before starting a distinct change; branch
  `feature/<issue-number>` or `bug/<issue-number>`; reference the issue URL in
  commits. PRs merge to `main`.
- Small, logical, separate commits. Don't bundle unrelated changes.
- Bump the Go toolchain in lockstep across modules; don't let versions drift.

## Open items

- All components are Postgres-backed (SQLite was purged so the suite runs on one
  database). The `contact` and `newsletter` store packages have no PostgreSQL
  tests yet — their SQLite tests were removed with the backend; cover the SQL in
  the `integration` module against the CI Postgres service.
- `contact` lost its runnable example when the SQLite backend module was
  deleted; a Postgres-backed example is worth adding.
- `faq` is intended to join this monorepo but is mid-refactor and lives in its
  own repository for now.
