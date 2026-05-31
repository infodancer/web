# faq

[![Go Reference](https://pkg.go.dev/badge/github.com/infodancer/faq.svg)](https://pkg.go.dev/github.com/infodancer/faq)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

Reusable Go module for database-driven FAQ / Q&A sections. Serves imported and
locally authored questions and answers under a custom htmx UI, with per-row
source attribution for imported content.

**Status:** pre-release.

## Features

- Imported and locally authored Q&A in one Postgres database, with source
  attribution (domain, license, canonical URL) preserved per row.
- Up/down voting on answers with self-vote prevention.
- Admin lock / delete (soft) for moderation.
- Author edit window (10 min default) plus unlimited admin edits.
- Subsites: named, query-time slices of the corpus.
- Postgres full-text search.
- Sitemap-index endpoints for SEO.
- Server-rendered htmx HTML; templates overridable via an `fs.FS`.
- OIDC consumed via a `UserResolver` interface — the host site owns the auth flow.

The schema is bulk-load friendly (`pgx.CopyFrom` into the `faq_*` tables), so
source-specific import tooling can live outside this module and write directly
into the migrated schema.

## Install

```
go get github.com/infodancer/faq
```

## Usage sketch

```go
mod, err := faq.New(faq.Config{
    DB:       pool,
    Resolver: myUserResolver,
    BasePath: "/faq",
})
if err != nil { log.Fatal(err) }

if err := mod.Migrate(ctx); err != nil { log.Fatal(err) }

mux.Handle("/faq/", http.StripPrefix("/faq", mod.Routes()))
```

## Skinning (host chrome)

Pages render through infodancer/ui's `ui/document`. A host site owns the chrome
by setting `Config.Chrome`, called per request to supply a `ui.DocumentData`
(nav, footer, meta, theme, analytics, asset base, head tags) — so the FAQ wears
the host's live, auth-aware navigation. Leave it nil for a standalone
deployment and the module renders a complete default page from
`SiteName`/`HomeURL`. Either way the module overlays its own section nav
(Tags / Search / Ask) as the left sidebar; it owns the FAQ content, not the
surrounding chrome.

The module owns no login flow, so set `Config.SignInURL` to the host's login
URL: an anonymous visitor on an open question then gets a linked "sign in to
post an answer" prompt (plain text when unset).

```go
mod, err := faq.New(faq.Config{
    DB:        pool,
    Resolver:  myUserResolver,
    SignInURL: "/login",
    Chrome:    func(r *http.Request) ui.DocumentData { return host.PageChrome(r) },
})
```

## License

Code: [Apache-2.0](LICENSE).

Imported content remains under its own source license. The module preserves the
attribution fields and license string per imported row; consumers are
responsible for surfacing them.
