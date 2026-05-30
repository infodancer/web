# Contributing

## Local development

Requires Go (`stable` or `oldstable` — see `.github/workflows/ci.yml` for the
exact matrix), [Task](https://taskfile.dev), and Docker (for integration tests
against Postgres via testcontainers).

```
task test       # unit tests with race detector
task check      # fmt + vet + lint + test + vulncheck
task build      # build the faq-serve CLI into ./bin/
task generate   # regenerate store/ from store/queries.sql via sqlc
```

`task check` is the gate for "ready to push" — CI runs the same checks.

## Conventions

- Postgres only (no sqlite dialect path). All SQL lives in `migrations/` and
  `store/queries.sql`; the store layer is sqlc-generated.
- All table and type names are prefixed `faq_` so the module's schema coexists
  with the host site's schema in a shared database.
- Library code has zero runtime deps outside what `go.mod` declares. Binaries
  live under `cmd/<name>/`.
- All exported symbols carry doc comments.
- Tests use the `package foo_test` black-box form where it makes sense.
  Integration tests hit a real Postgres via testcontainers — no mocks for the
  store layer.

## Module extension points

The module exposes three extension surfaces. Override them via `faq.Config`:

- **`UserResolver`** — the host site owns the auth flow. See `user.go`.
- **Templates** — set `Config.Templates` to an `fs.FS` that layers over the
  embedded defaults. View-model structs are documented inline in
  `internal/handler`.
- **CSRF** — disable the bundled middleware with `Config.CSRF.Disable = true`
  if the host site already has one.
