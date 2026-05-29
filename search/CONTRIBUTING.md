# Contributing

This module mirrors the conventions of the rest of the suite (`faq` is the
closest sibling).

- **Test-driven.** Add or update tests with every change. Pure-Go logic
  (config defaults, the rerank pass-through, cursor encode/decode) gets unit
  tests; anything touching SQL gets an integration test against a real
  Postgres via `testcontainers`.
- **Toolchain.** `pgxpool` + `goose` + `sqlc`. Regenerate the store with
  `sqlc generate` from `store/` after editing `queries.sql` or a migration.
- **CI must stay green:** `gofmt`, `go vet`, `go test -race`,
  `golangci-lint`, `govulncheck`.
- **Local workspace.** A dev-local `go.work` resolves `go-embedding` and
  `infodancer/ui` from sibling working trees; it is git-ignored.

See DESIGN.md for the architecture and the milestone plan.
