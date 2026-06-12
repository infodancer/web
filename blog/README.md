# infodancer/web/blog

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

A reusable Go module providing a site's authoritative editorial voice: a
database-backed, multi-author blog with named editorial streams, a markdown
authoring UX, the full feed matrix, JSON-LD, and a sitemap. Full-UI
component of the [infodancer/web](https://github.com/infodancer/web)
monorepo: ships templates, CSS, and the authoring surface, and mounts into
a host Go service under a base path.

## What it does

- **Named blogs.** One instance hosts several editorial streams ("Admin
  Notes", a weekly column), each with its own index page and feeds, sharing
  one author pool and tag namespace. Posts may also stand outside every
  named blog.
- **Multi-author, invitation-only.** Authoring rights are an
  [infodancer/authz](https://github.com/infodancer/authz) role grant
  (module `blog`, roles `author` and `admin`) via the bundled `blogauthz`
  resolver adapter — no registration surface exists. The host's profile
  component supplies author display identity through a hook.
- **Feeds.** RSS 2.0, Atom 1.0, and JSON Feed 1.1, each at combined,
  per-author, and per-named-blog scope, with `<link rel="alternate">`
  autodiscovery on every HTML page.
- **SEO.** JSON-LD (`Blog`, `BlogPosting`, `ProfilePage`), canonical URLs,
  OpenGraph metadata via `infodancer/ui`'s meta partial, and
  `/sitemap.xml`.
- **Authoring.** A full-page post form embedding
  [ui/mdedit](https://github.com/infodancer/ui)'s editor field: markdown
  with a toolbar, local-file load, and drag-drop/paste image upload backed
  by the sibling `media` component. Draft → publish → unpublish → archive
  lifecycle with soft delete.
- **Search.** Published posts index into the sibling `search` component
  through a host-wired `Indexer` hook; drafts never do.

## Quickstart

```go
import (
    "github.com/infodancer/web/blog"
    "github.com/infodancer/web/blog/blogauthz"
)

resolver := blogauthz.NewResolver(identify, authzStore, func(id *authz.Identity) string {
    return lookupUserKey(id) // the host's stable user id, as profile uses
})

mod, err := blog.New(blog.Config{
    DB:       pool,
    Resolver: resolver,
    BasePath: "/blog",
    SiteName: "Old School Gamers",
    BaseURL:  "https://oldschoolgamers.org", // absolute origin for feeds/JSON-LD/sitemap
})
if err != nil { ... }
if err := mod.Migrate(ctx); err != nil { ... }
mux.Handle("/blog/", http.StripPrefix("/blog", mod.Routes()))
```

Grant authorship (the "invitation"):

```
authzctl --driver postgres --dsn $DATABASE_URL grant ISSUER SUBJECT author --module blog
```

### Suite hooks (all optional)

```go
cfg.Authors = profileDirectory     // blog.AuthorDirectory over web/profile
cfg.AuthorCard = profileMod.Card   // byline card on post pages
cfg.Indexer = searchAdapter        // ~10 lines over web/search; see config.go
cfg.Blobs, cfg.Objects = blobs, objects // web/media stores; enables image upload
cfg.Media = media.Config{PublicBaseURL: "/media/blog"} // host's serving mount
```

The host serves uploaded bytes from its own media mount; blog only ingests.
Call `mod.ReindexAll(ctx)` at boot to reconcile the search index.

## HTTP surface

Read (anonymous): `/`, `/p/{slug}`, `/b/{slug}`, `/authors`,
`/authors/{slug}`, `/tags`, `/tags/{slug}`, `/sitemap.xml`, and the feed
matrix `/feed.{xml,atom,json}`, `/authors/{slug}/feed.*`,
`/b/{slug}/feed.*`.

Write (author/admin role, CSRF-guarded): `/new`, `/mine`, `/p/{id}/edit`,
`/p/{id}/{publish,unpublish,archive,delete}`, `/upload`.

Admin: `/admin/blogs` (named-blog management).

## Design

[DESIGN.md](DESIGN.md) is the design record: scope, data model, identity
and roles, the feed matrix, SEO, hooks, and milestones. Conventions follow
the monorepo's [CONVENTIONS.md](../CONVENTIONS.md) (Postgres + sqlc + pgx,
goose migrations in `blog_goose_db_version`, `blog_*` table prefix, host
owns the pool).

## License

Apache-2.0. See [LICENSE](LICENSE).
