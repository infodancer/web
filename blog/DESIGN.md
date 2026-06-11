# infodancer/web/blog — design (v0.1)

*Status: implementation in progress. Initial design 2026-05-19; motivation revised 2026-06-09 (decoupled from the obsolete session-notes premise); revised 2026-06-11 for the authoritative-voice requirements: named blogs, the full feed matrix (RSS/Atom/JSON Feed at three scopes), JSON-LD, and reuse of the suite's existing auth, roles, profile, media, and search components. Once code lands the design + code together are authoritative.*

## 1. Purpose

`infodancer/web/blog` is a reusable Go module providing a database-backed blog for consumer sites in the infodancer / matthewjhunter web portfolio. It is a **full-UI module** — ships templates, CSS, and an authoring UX — mounted into a host Go service under a base path with `http.StripPrefix`, like the other components in this monorepo.

### Why this module exists

The module is **a site's authoritative editorial voice.** Consumer sites in the portfolio accumulate tools and content but have no first-person layer — nowhere for the maintainers to speak directly: announcements, dev notes, commentary, essays, recurring columns. A database-backed blog with a real authoring UX is that layer, and every site in the portfolio eventually wants one, so it is built once as a module.

The near-term driver is **osg**: it has a bestiary, generators, and a campaign directory but no voice, and it is starting outreach into the OSR blogosphere — which is person-to-person and needs a place where the maintainer writes directly. osg is the first consumer; mjh, triggerfinger, hunterfamily, and amyhunter are plausible later ones.

> **Not session-notes.** An earlier draft was motivated by the osg session-notes workflow. That feature shipped separately inside osg's own server and is not this module. `blog` is a general editorial blog.

### What the module is

- A Go module mountable under any base path (`/blog`, `/notes`, `/journal`).
- A Postgres-backed data model for **named blogs** (e.g. "Admin Notes", "The Weekly Owlbear"), posts, tags, and a thin author registry.
- **Multi-author, invitation-only.** Authoring rights are an `infodancer/authz` role grant (`blog`/`author`), not open registration. Identity, roles, and author profile data are all reused from the suite — the module owns none of them.
- An authoring UX built on `infodancer/ui/mdedit`: markdown editing with a toolbar, local-file load, and inline image upload backed by `infodancer/web/media`.
- A read surface: combined index, per-blog index, single post, author pages, tag pages.
- **The full feed matrix**: RSS 2.0, Atom 1.0, and JSON Feed 1.1, each at three scopes — combined, per-author, per-named-blog — with `<link rel="alternate">` autodiscovery on every HTML page.
- **SEO-rich output**: JSON-LD (`Blog`, `BlogPosting`, `ProfilePage`/`Person`), canonical URLs, OpenGraph/meta via `infodancer/ui`'s meta partial, and a sitemap.
- Post indexing into `infodancer/web/search` via a host-wired hook.

### What the module is not

- Not a general-purpose CMS competing with Ghost or WordPress. The scope is what this portfolio needs.
- Not a static-site generator: authoring and publishing happen at runtime through a web UI.
- Not a WYSIWYG editor. Markdown is the authoring format; mdedit adds quality-of-life tooling around it.
- Not an identity, role, profile, media, or search system. It consumes the suite's existing components for all five.
- Not multi-tenant in the isolation sense — one blog *instance* per site. Multiple **named blogs** within the instance are first-class (that is how "admin notes" and "a weekly humor column" coexist), but they share one author pool, one tag namespace, and one database.

## 2. Non-goals (v0.1)

Explicitly excluded from v0.1 to keep scope honest:

- **Comments.** The first consumers don't want open comments; the schema and moderation surface are real complexity. v0.2 candidate (flat, authenticated-only, as previously designed).
- **Revision history.** Post edits overwrite; an edit-history table and diff view are v0.2.
- **Draft autosave.** Drafts save explicitly.
- **Scheduled publishing.** Posts publish immediately.
- **Server-rendered live preview pane.** The post form embeds `mdedit/field` (the host-form variant), which has no preview pane; EasyMDE's editing affordances plus honest server-side rendering on save cover v0.1. A `/preview` endpoint riding `mdedit/edit`'s lifecycle is v0.2.
- **Reactions / likes / votes.**
- **Series / multi-post sequences.**
- **Archive-by-month pages.** Tag pages, author pages, per-blog indexes, and feeds cover discovery; a `/archive/{year}` surface is a small v0.2 addition.
- **Tag autocomplete.** v0.1 ships a plain comma-separated tag input.
- **Multi-tenant deployment** (one binary serving multiple isolated blog instances).
- **Ongoing sync with external sources** (RSS import, cross-posting).

## 3. Architecture

### Module shape

The standard monorepo mount contract (`New(Config) → *Module → Routes() / Migrate(ctx)`), mirroring `profile`:

```go
import "github.com/infodancer/web/blog"

mod, err := blog.New(blog.Config{
    DB:       pool,               // *pgxpool.Pool from the host
    Resolver: resolver,           // bloguser.Resolver (e.g. blogauthz.NewResolver)
    BasePath: "/blog",
    SiteName: "Old School Gamers",
    BaseURL:  "https://oldschoolgamers.org", // absolute origin for feeds/JSON-LD/sitemap
})
mux.Handle("/blog/", http.StripPrefix("/blog", mod.Routes()))
```

### Suite components consumed

| Component | How blog uses it |
|---|---|
| `infodancer/ui` | Page chrome via `ui/document` (Layer 2), design tokens, htmx. Same render-package pattern as profile/faq/timeline. |
| `infodancer/ui/markdown` | The single sanitization boundary. Post bodies render through the `Rich()` preset at save time; cached in `body_html`. |
| `infodancer/ui/mdedit` | The editor component: `mdedit/field` embedded in the post form, `AssetsFS`/`HeadTags` mounted by the module, `Field.UploadURL` wired to the upload endpoint, `AllowFileLoad` on. |
| `infodancer/authz` | Roles, via the `blogauthz` subpackage (mirrors `faqauthz`): module `blog`, roles `author` and `admin`. Invitation-only authorship **is** the role grant — `authzctl grant <issuer> <subject> author --module blog`. |
| `infodancer/web/media` | Inline image upload. The module runs `media.Ingest` itself (auth + CSRF enforced by blog); the host serves the bytes from its media mount. |
| `infodancer/web/profile` | Author display identity (handle, display name, avatar, bio) via the host-injected `AuthorDirectory` hook and optional `AuthorCard` byline hook. No hard import. |
| `infodancer/web/search` | Post indexing via the host-injected `Indexer` hook. No hard import — the adapter over `*search.Module` is ~10 lines of host glue. |

Direct Go dependencies are `ui` (+ subpackages), `authz`, and `media`; `profile` and `search` stay behind interfaces per the "host owns cross-module wiring" convention.

### Database

Postgres only, `blog_*`-prefixed tables, goose migrations in a dedicated `blog_goose_db_version` table, sqlc + pgx/v5 store. The host supplies the `*pgxpool.Pool`.

## 4. Module layout

```
blog/
├── blog.go                    // Module: New, Routes, Migrate
├── config.go                  // Config, CSRFConfig, AuthorDirectory, Indexer
├── bloguser/user.go           // User + Resolver types
├── blogauthz/resolver.go      // authz → bloguser.Resolver adapter
├── sqlc.yaml
├── db/
│   ├── migrations/00001_init.sql
│   ├── queries/*.sql
│   └── *.go                   // sqlc-generated (committed)
├── store/store.go             // domain wrapper over db, sentinel errors
└── internal/
    ├── slug/                  // slug normalization (faq's algorithm)
    ├── feeds/                 // RSS 2.0 / Atom 1.0 / JSON Feed 1.1 builders
    ├── render/                // ui/document + mdedit partials template loader
    └── handler/               // HTTP layer, templates/, static/
```

## 5. Identity, roles, and authors

The module owns **no identity**. Three layers, all supplied by the host:

### bloguser.User / Resolver

```go
package bloguser

type User struct {
    Key         string // host's stable opaque user id (profile's user_key convention)
    DisplayName string
    Author      bool   // may create and edit own posts
    Admin       bool   // may edit anything, manage named blogs
}

type Resolver interface {
    Resolve(ctx context.Context, r *http.Request) (*User, error)
}
```

`(nil, nil)` = anonymous (read-only). `(nil, err)` = present-but-invalid credential → 401.

### blogauthz

Mirrors `faqauthz`: the host supplies an `Identifier` (request → `*authz.Identity`) and an `authz.Store`; the adapter resolves roles scoped to module `blog` and sets `Author`/`Admin` from the `author`/`admin` roles. A `KeyFunc func(issuer, subject string) string` maps the authz identity to the host's user key (defaulting to `issuer + "|" + subject`); hosts that key profiles by their own users.id pass their mapping.

**Invitation-only authorship** is therefore an authz grant, made by an admin via `authzctl` or the host's own role UI. Blog ships no registration or invitation flow of its own.

### Author registry

Posts store `author_key`. For URLs, bylines, and feeds the module keeps a thin **registry** — a cache of display identity for users who have authored posts, *not* an identity table:

```sql
CREATE TABLE blog_authors (
    user_key     TEXT PRIMARY KEY,
    slug         TEXT NOT NULL UNIQUE,   -- /authors/{slug}; profile handle when available
    display_name TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

The row is created/refreshed whenever the user saves a post, sourced from the `AuthorDirectory` hook when configured (profile handle → slug, profile display name) and from the resolver's `DisplayName` otherwise (slugified, `-2`/`-3` suffix on collision). Avatar and bio are never cached — author pages enrich live through the hook.

```go
// AuthorInfo is what the host's profile system knows about a user.
type AuthorInfo struct {
    Slug        string // URL-safe handle; "" lets blog derive one
    DisplayName string
    AvatarURL   string
    BioHTML     template.HTML // pre-sanitized by the host (profile's bio pipeline)
}

type AuthorDirectory interface {
    Lookup(ctx context.Context, userKey string) (*AuthorInfo, error) // (nil, nil) = unknown
}
```

`Config.AuthorCard func(ctx, userKey) (template.HTML, error)` optionally renders bylines through `profile.Module.Card`; nil falls back to a plain-text byline.

## 6. Data model

### Named blogs

A site runs one blog *instance* containing several named **blogs** — distinct editorial streams ("Admin Notes", "The Weekly Owlbear") with their own index pages and feeds, sharing the author pool and tag namespace:

```sql
CREATE TABLE blog_blogs (
    id          BIGSERIAL PRIMARY KEY,
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Admins manage them (§8). A post may belong to one named blog or to none (`blog_id NULL` — it appears in combined surfaces only).

### Posts

```sql
CREATE TABLE blog_posts (
    id           BIGSERIAL PRIMARY KEY,
    slug         TEXT NOT NULL UNIQUE,
    title        TEXT NOT NULL,
    body_md      TEXT NOT NULL,
    body_html    TEXT NOT NULL,            -- sanitized cache; regenerated on save
    excerpt      TEXT NOT NULL DEFAULT '', -- manual override; auto-derived if empty
    author_key   TEXT NOT NULL REFERENCES blog_authors(user_key),
    blog_id      BIGINT REFERENCES blog_blogs(id) ON DELETE SET NULL,
    status       TEXT NOT NULL CHECK (status IN ('draft', 'published', 'archived')),
    external_id  TEXT,                     -- opaque host-defined linkage
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,              -- set on first publish, preserved after
    deleted_at   TIMESTAMPTZ               -- soft-delete
);
```

Indexes on `(status, published_at DESC)`, `author_key`, `blog_id`, and `external_id` (all partial on `deleted_at IS NULL` where it matters). Slug uniqueness is global across the instance. Slugs are auto-generated from the title on first save and preserved on rename; admins can override.

### Tags

As previously designed: `blog_tags (id, slug UNIQUE, name)` + `blog_post_tags (post_id, tag_id)` join, flat namespace, created on first use from the comma-separated input.

### Lifecycle

| Action | Effect |
|--------|--------|
| Save draft | `status='draft'`; invisible publicly; not in search |
| Publish | `status='published'`, `published_at=now()` on first publish (else preserved); indexed into search |
| Unpublish | back to draft, `published_at` preserved; removed from search |
| Archive | `status='archived'`; out of indexes/feeds/search; direct URL still 404s for the public, visible to author/admin |
| Delete | `deleted_at=now()`; soft; admin restore + hard-delete deferred to v0.2 admin surface |

### Authorization

- Create post: `user.Author || user.Admin`.
- Edit / publish / unpublish / archive / delete: post's `author_key == user.Key` or `user.Admin`.
- Manage named blogs: `user.Admin`.

## 7. Authoring UX

A single full-page form at `GET /new` and `GET /p/{id}/edit`: title, named-blog `<select>` (admins see all blogs; "none" allowed), tags (comma-separated), excerpt override, and the body as an embedded `mdedit/field` (toolbar `full`, `AllowFileLoad` on, `Rows` ~20). The module mounts `mdedit.AssetsFS()` and `ui.AssetsFS()` under its static path and emits `mdedit.HeadTags` from its base template, so the host needs no extra wiring.

### Inline images via /media

When `Config.Blobs`/`Config.Objects` are set, the editor's `Field.UploadURL` points at `POST {BasePath}/upload`. The handler authenticates (author or admin), enforces CSRF, and calls `media.Ingest` with `subject = "user:" + user.Key`, responding `{"url": "..."}` — exactly the contract mdedit's adapter expects, so drag-drop and paste insert a markdown image link at the cursor. Serving the bytes stays with the host's media mount; `Config.Media.PublicBaseURL` must point at it (e.g. `/media/blog`). Uploads are subject-tagged to the uploading user, not the post, so images can be uploaded before the first save.

## 8. HTTP surface

All paths relative to `BasePath`.

### Read (anonymous OK; published, non-deleted posts only)

| Method | Path | Purpose |
|---|---|---|
| GET | `/` | Combined index, paginated |
| GET | `/p/{slug}` | Single post (author/admin also see their drafts) |
| GET | `/b/{slug}` | Named-blog index, paginated |
| GET | `/authors` | Authors index |
| GET | `/authors/{slug}` | Author page (posts by author) |
| GET | `/tags` | Tags index |
| GET | `/tags/{slug}` | Tag page |
| GET | `/feed.xml` `/feed.atom` `/feed.json` | Combined feeds (RSS 2.0 / Atom / JSON Feed) |
| GET | `/authors/{slug}/feed.xml` `…/feed.atom` `…/feed.json` | Per-author feeds |
| GET | `/b/{slug}/feed.xml` `…/feed.atom` `…/feed.json` | Per-blog feeds |
| GET | `/sitemap.xml` | Sitemap (posts, blogs, authors, tags) |

### Write (author or admin; CSRF-guarded)

| Method | Path | Purpose |
|---|---|---|
| GET | `/new` | New-post form |
| POST | `/new` | Create (draft or publish per submit button) |
| GET | `/p/{id}/edit` | Edit form |
| POST | `/p/{id}/edit` | Update fields (re-render body, refresh author registry, re-index) |
| POST | `/p/{id}/publish` | → published |
| POST | `/p/{id}/unpublish` | → draft |
| POST | `/p/{id}/archive` | → archived |
| POST | `/p/{id}/delete` | Soft-delete |
| POST | `/upload` | Inline image upload → media (only when media stores configured) |

### Admin

| Method | Path | Purpose |
|---|---|---|
| GET | `/admin/blogs` | List + create form for named blogs |
| POST | `/admin/blogs` | Create named blog |
| POST | `/admin/blogs/{id}` | Update name/description/slug |
| POST | `/admin/blogs/{id}/delete` | Delete (refused while posts reference it) |

### CSRF

The same-origin (Origin/Referer) guard used by profile and faq, on every non-GET route; `Config.CSRF.Disable` for hosts enforcing it at a higher layer.

## 9. Feeds

The feed matrix is formats × scopes: **RSS 2.0, Atom 1.0 (RFC 4287), JSON Feed 1.1** at **combined, per-author, per-named-blog** scope. All feeds:

- carry the `FeedSize` (default 20) most recent published posts in scope, newest first;
- include full sanitized `body_html` content (`<content:encoded>` / `<content type="html">` / `content_html`) plus the excerpt as summary/description;
- carry per-item author names, tag categories, and `published_at`/`updated_at` timestamps;
- use absolute URLs built from `Config.BaseURL` (self link, item links, home link);
- are generated by hand-rolled `encoding/xml`/`encoding/json` builders in `internal/feeds` — no third-party feed dependency.

**Autodiscovery:** every HTML page emits `<link rel="alternate" type="application/rss+xml|application/atom+xml|application/feed+json" …>` in `<head>` (via ui/document's `head` block) for the feeds of its scope: the index and post pages advertise the combined feeds (post pages additionally their named blog's), author pages the author feeds, named-blog pages that blog's feeds. Titles distinguish scope ("Old School Gamers — Admin Notes (Atom)").

## 10. SEO

- **JSON-LD** in `<script type="application/ld+json">`, built server-side from typed view models:
  - post detail → `BlogPosting` (headline, description, datePublished, dateModified, author `Person` with URL to the author page, keywords from tags, `mainEntityOfPage`, `isPartOf` the named `Blog` when set);
  - combined and per-blog indexes → `Blog` (name, description, url);
  - author pages → `ProfilePage` wrapping `Person`.
- **Canonical URLs** (`<link rel="canonical">`) from `BaseURL` on every read page.
- **Meta/OpenGraph** through `ui/meta` via `DocumentData.Meta` (title, description from excerpt).
- **Sitemap** at `/sitemap.xml` covering published posts (lastmod = `updated_at`), named blogs, author pages, and tag pages.

## 11. Search integration

Search is delegated to `infodancer/web/search` — the module keeps **no internal FTS** (the earlier generated-`tsvector` design is dropped; one search corpus per site beats per-module search). Blog defines a narrow hook and calls it on lifecycle transitions; the host adapts it over `*search.Module`:

```go
type SearchDocument struct {
    ExternalID  string    // post id
    URL         string    // absolute canonical post URL
    Title       string
    Summary     string    // excerpt
    Tags        []string
    BodyMD      string
    PublishedAt time.Time
}

type Indexer interface {
    IndexPost(ctx context.Context, doc SearchDocument) error
    DeletePost(ctx context.Context, externalID string) error
}
```

Publish and edit-while-published index; unpublish, archive, and delete remove. Drafts are never indexed. Indexing failures are logged, never user-facing — search lags rather than blocking a save. A `Module.ReindexAll(ctx)` walks published posts for boot-time reconciliation (mirrors osg's search wiring pattern).

## 12. External linkage

Unchanged from the earlier design: `blog_posts.external_id` is an opaque `TEXT NULL` column the host may set and interpret; blog stores and indexes it but never reads meaning into it.

## 13. Config

```go
type Config struct {
    // Required
    DB       *pgxpool.Pool
    Resolver bloguser.Resolver

    // Site identity / chrome
    Logger          *slog.Logger // default slog.Default()
    BasePath        string       // default "/blog"
    SiteName        string       // default "Blog"
    SiteDescription string       // feeds + JSON-LD + meta description
    HomeURL         string       // back-to-site link
    BaseURL         string       // absolute origin (https://example.org); required for valid feeds/sitemap/JSON-LD, relative fallback without it
    SignInURL       string
    Chrome          func(*http.Request) ui.DocumentData

    // Suite hooks (all optional)
    Authors    AuthorDirectory // profile-backed author identity
    AuthorCard func(ctx context.Context, userKey string) (template.HTML, error)
    Indexer    Indexer         // search hook

    // Inline image upload (enabled only when Blobs and Objects are both set)
    Blobs   media.BlobStore
    Objects *media.Store
    Media   media.Config // Category default "blog"; PublicBaseURL → host's media mount

    // Behavior
    PageSize    int // default 20
    FeedSize    int // default 20
    CSRF        CSRFConfig
    AutoMigrate bool
}
```

## 14. Markdown rendering

Post bodies render through `infodancer/ui/markdown`'s `Rich()` preset at save time, cached in `body_html`; only that pipeline's output is ever cast to `template.HTML`. Excerpt auto-derivation (when the override is empty) takes the first ~200 characters of the body stripped of markdown syntax, ellipsized.

## 15. Testing

Same posture as profile: handler unit tests against an in-memory fake store (happy path + error paths, CSRF fixtures, role gates), pure unit tests for slug/feeds/JSON-LD (feeds validated by re-parsing with `encoding/xml`/`encoding/json`), and Postgres integration tests for the store + migrations in the monorepo's `integration` module against `TEST_DATABASE_URL`, including coexistence with the other components' tables and a smoke mount.

## 16. Security

- **CSRF**: bundled same-origin guard on every write.
- **XSS**: all user markdown through `ui/markdown` presets; JSON-LD payloads built from typed structs and emitted via `json.Marshal` (never string-assembled); feed XML through `encoding/xml`.
- **AuthN/Z**: identity from the host resolver; authorship/admin from authz roles; checks in handlers, templates only consume precomputed booleans.
- **SQL injection**: sqlc-generated typed queries only.
- **Uploads**: authenticated + CSRF + media's validation/re-encode pipeline; size and pixel caps from `media.Config`.
- **Open redirect**: no user-supplied redirect targets; post-save redirects are built from the post id.

## 17. Milestones (v0.1)

- **M1 — schema + store**: migration, sqlc queries, store wrapper, integration tests.
- **M2 — read path**: index, post, named-blog, author, tag pages; render pkg; chrome.
- **M3 — feeds + SEO**: the 3×3 feed matrix, autodiscovery, JSON-LD, sitemap.
- **M4 — write path**: forms, lifecycle transitions, mdedit field, upload, admin blogs.
- **M5 — hooks + registration**: Indexer/AuthorDirectory wiring, ReindexAll, integration-module tests, root Taskfile/CI (already registered), README/SECURITY.
- **M6 — osg integration** (separate repo): mount under `/blog`, blogauthz resolver over osg's session, profile directory adapter, search adapter, media serving mount. Tag `blog/v0.1.0` once osg runs it.

## 18. v0.2 candidates

Comments (flat, authenticated), revision history + diffs, draft autosave, scheduled publishing, archive-by-month pages, tag autocomplete, `/preview` live-preview endpoint, admin deleted-posts/restore/hard-delete surface, tag rename/merge admin, featured posts, per-feed paging (RFC 5005), WebSub ping on publish.
