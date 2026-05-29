# infodancer/blog — design (v0.1)

*Status: pre-implementation. Initial design proposal, 2026-05-19. This document is the source of truth until implementation begins; once code lands the design + code together are authoritative.*

## 1. Purpose

`infodancer/blog` is a reusable Go module providing a database-backed blog / lightweight CMS for consumer sites in the infodancer / matthewjhunter web portfolio (see `infodancer/infodancer/docs/web-portfolio-architecture.md`). It is a **full-UI module** — ships templates, CSS, and an authoring UX. Mounts into a host Go service under a base path with `http.StripPrefix`.

### Why this module exists

The motivating case is the **osg session-notes workflow**. osg's campaign reports are authored as Hugo page bundles, edited locally, committed to git, and rendered as part of the Hugo build. This works for the maintainer; it does not work for the campaign players, who are the actual content creators — they aren't going to learn git, Hugo, markdown frontmatter, and a webp toolchain to write a session report from their character's POV. User adoption is gated on a real CMS with a real authoring UX. The shape of that CMS is "a blog": posts with title, body, author, tags, dates, comments, and a publish lifecycle.

The module exists to solve this user-adoption problem first, and to be reusable across other consumer sites second. mjh, hunterfamily, and amyhunter are plausible future consumers; they're not driving the design today.

### What the module is

- A Go module mountable under any base path (`/blog`, `/notes`, `/journal`, whatever the consumer picks).
- A Postgres-backed data model for posts, authors, tags, comments, revisions, and OIDC users.
- An htmx-driven authoring UX built on `infodancer/mdedit`: a markdown editor with a toolbar and server-rendered live preview, a draft/publish lifecycle, tag autocomplete, author selection.
- A read surface: index, single post, author pages, tag pages, archive by month/year, Atom feed, search.
- Admin surfaces: post management across all statuses, author management, tag management.
- Templates and CSS consuming `infodancer/ui` design tokens; markdown rendering and sanitizing delegated to `infodancer/markdown`; the editor component delegated to `infodancer/mdedit`.
- A migration tool to import existing Hugo page bundles (osg's existing session-notes corpus).

### What the module is not

See §2 for the v0.1 exclusions; a few framing points belong here:

- Not a general-purpose CMS competing with Ghost, Wagtail, or WordPress. The scope is what this portfolio needs, no more.
- Not a static-site generator. Hugo handles content sites where authors are devs; blog handles content sites where they aren't.
- Not a WYSIWYG editor. Markdown is the authoring format. The editor adds quality-of-life tooling around markdown (toolbar, preview), not a rich-text replacement.
- Not multi-tenant. One blog instance per consumer site, per database. Multiple authors and many users are first-class; multiple separate blog instances in one deployment are not.

## 2. Non-goals (v0.1)

Explicitly excluded from v0.1 to keep scope honest:

- **Drag-and-drop image upload** inside the editor. v0.1 supports image references via raw markdown (`![alt](url)`); a media subsystem with upload, resize, and storage is v0.2.
- **Draft autosave.** Drafts save explicitly. JS-driven autosave is v0.2.
- **Scheduled publishing.** Posts publish immediately on `POST /p/{id}/publish`. Future-dating publication is v0.2.
- **Threaded comments.** v0.1 ships flat comments on posts. Reply-tree threading is v0.2.
- **Comment moderation queue.** Comments from authenticated users post immediately; spam handling deferred. Admin can delete after the fact.
- **Reactions / likes / votes.** Not a Stack Exchange surface; not in scope.
- **Series / multi-post sequences** with prev/next navigation between linked posts. v0.2 candidate.
- **Markdown document export** (mirror of faq's Track E). v0.2 candidate if it proves useful.
- **WYSIWYG editor.** Markdown is the format.
- **Multi-tenant deployment** (one binary serving multiple isolated blogs). Out of scope.
- **Ongoing sync with external sources** (RSS import, cross-posting, etc.). Out of scope.

Some of these (autosave, image upload, threaded comments) are obvious quality-of-life features. They're v0.2 because they each add real complexity and v0.1 needs to *ship* before the perfect is the enemy of the good.

## 3. Architecture

### Module shape

The same shape as `github.com/matthewjhunter/faq`:

```go
import "github.com/infodancer/web/blog"

cfg := blog.Config{
    DB:          pool,         // *pgxpool.Pool from consumer
    Resolver:    myResolver,   // blog.UserResolver
    Logger:      logger,       // *slog.Logger (optional)
    BasePath:    "/blog",      // mount point
    SiteName:    "Old School Gamers",
    HomeURL:     "/",
    AutoMigrate: true,
    Templates:   tplOverride,  // fs.FS, optional
    Static:      staticOverride, // fs.FS, optional
}

mod, err := blog.New(cfg)
mux.Handle("/blog/", http.StripPrefix("/blog", mod.Routes()))
```

A consumer wiring blog into its existing service supplies a database connection, a `UserResolver` mapping the consumer's session model to `bloguser.User`, and a base path. The module handles everything under that base path internally.

### Portfolio building blocks

blog is not a from-scratch CMS; it composes three already-published portfolio modules rather than reimplementing them:

- **`infodancer/markdown`** — the goldmark + bluemonday render/sanitize pipeline (§10). The single sanitization boundary; blog never parses or sanitizes markdown itself.
- **`infodancer/mdedit`** — the editor UI component (§7). Ships the display/edit/preview partials, the adapter seam, and vendored, SRI-pinned EasyMDE assets. blog renders bodies through markdown and hands the result to mdedit as `Field.HTML`.
- **`infodancer/ui`** — design tokens + base CSS (§9). blog's CSS and mdedit's both chain through `--app-*`.

The host page loads mdedit's and ui's assets in `<head>` (mdedit exposes `HeadTags(staticBase)`); blog mounts `mdedit.AssetsFS()` and `ui.AssetsFS()` under static paths alongside its own. All three are private modules consumed by tagged version and vendored, per the portfolio visibility policy.

### UserResolver

Identical contract to faq's `faquser.UserResolver`. Long-term, this resolver type will move to a shared `infodancer/userctx` package once a third module wants the same shape; until then each module declares its own with the same structural shape so consumers can implement once and adapt cheaply.

```go
package bloguser

type User struct {
    Subject     string  // OIDC sub
    Issuer      string  // OIDC iss
    DisplayName string
    Email       string
    Admin       bool
}

type UserResolver interface {
    Resolve(ctx context.Context, r *http.Request) (*User, error)
}
```

Returning `(nil, nil)` means anonymous. Returning `(nil, err)` means "present but invalid" → 401.

### Database

Postgres only. `blog_*`-prefixed tables coexist in the consumer's database with other modules' tables (faq's `faq_*`, the consumer's own). Migrations embedded via goose, runnable via `Module.Migrate(ctx)` or `cfg.AutoMigrate`.

The consumer supplies a `*pgxpool.Pool`. The module does not open its own pool; one pool per process, shared.

### Templates and static assets

`embed.FS`-backed defaults overlaid by `Config.Templates` (per-template overrides via name match) and `Config.Static` (per-file overrides). Same overlay shape as faq.

Templates consume `infodancer/ui` tokens — see §9.

### htmx

Same approach as faq. htmx 2.x loaded by the host; module endpoints emit fragments via `hx-target` / `hx-swap` for the parts that benefit. The authoring UX is the heaviest htmx user — live preview, tag autocomplete, inline edit.

## 4. Module layout

```
blog/
├── LICENSE
├── README.md
├── DESIGN.md
├── CLAUDE.md
├── CONTRIBUTING.md
├── SECURITY.md
├── Taskfile.yml
├── go.mod
├── blog.go                         // package entry (New, Routes, Migrate, Config)
├── bloguser/                       // User + UserResolver types
│   └── user.go
├── migrations/                     // goose SQL migrations
│   ├── 00001_users_authors_tags.sql
│   ├── 00002_posts.sql
│   ├── 00003_post_tags.sql
│   ├── 00004_comments.sql
│   └── 00005_post_revisions.sql
├── store/                          // sqlc-generated + queries.sql
│   ├── queries.sql
│   ├── store.go
│   └── *_integration_test.go
├── internal/
│   ├── handler/                    // HTTP handlers, middleware, view-models
│   ├── slug/                       // slug generation + validation
│   └── importer/                   // Hugo bundle migration
│                                   // (render/sanitize delegated to infodancer/markdown — §10)
├── templates/                      // embed:all
│   ├── base.gohtml
│   ├── pages/
│   │   ├── index.gohtml
│   │   ├── post_detail.gohtml
│   │   ├── post_edit.gohtml
│   │   ├── author.gohtml
│   │   ├── tag.gohtml
│   │   ├── archive.gohtml
│   │   ├── search.gohtml
│   │   └── admin/…
│   └── partials/
│       ├── post_card.gohtml
│       ├── comment.gohtml
│       ├── tag_chip.gohtml
│       └── nav.gohtml   // module-internal nav; consumer can override
│                        // (the editor toolbar/preview partials ship in infodancer/mdedit — §7)
└── static/                         // embed:all
    └── css/
        └── blog.css                // consumes --app-* tokens via --blog-* chain
```

## 5. Identity / users

Same shape as faq. `blog_users` stores one row per OIDC subject; `blog_authors` stores display identities used as post bylines.

A `blog_user` can own multiple `blog_authors` — the osg use case is that one player has multiple characters across campaigns. When posting, the user picks which author identity to publish as.

A `blog_author` can also exist without an associated `blog_user.id` — used during Hugo-bundle migration when the original frontmatter lists an author string that doesn't correspond to a registered user. Such authors can later be associated with a real user by an admin.

```sql
CREATE TABLE blog_users (
    id            BIGSERIAL PRIMARY KEY,
    oidc_subject  TEXT NOT NULL,
    oidc_issuer   TEXT NOT NULL,
    display_name  TEXT NOT NULL,
    email         TEXT,
    admin         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (oidc_subject, oidc_issuer)
);
```

```sql
CREATE TABLE blog_authors (
    id            BIGSERIAL PRIMARY KEY,
    slug          TEXT NOT NULL UNIQUE,         -- URL-safe; used in /authors/{slug}
    display_name  TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',     -- markdown bio
    avatar_url    TEXT,                          -- optional, consumer-hosted
    user_id       BIGINT REFERENCES blog_users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX blog_authors_user_idx ON blog_authors (user_id) WHERE user_id IS NOT NULL;
```

## 6. Data model

### Posts

```sql
CREATE TABLE blog_posts (
    id            BIGSERIAL PRIMARY KEY,
    slug          TEXT NOT NULL UNIQUE,         -- URL-safe; globally unique
    title         TEXT NOT NULL,
    body_md       TEXT NOT NULL,
    body_html     TEXT NOT NULL,                -- sanitized cache; regenerated on save
    excerpt       TEXT NOT NULL DEFAULT '',     -- manual override; auto-derived if empty
    author_id     BIGINT NOT NULL REFERENCES blog_authors(id) ON DELETE RESTRICT,
    status        TEXT NOT NULL CHECK (status IN ('draft', 'published', 'archived')),
    featured      BOOLEAN NOT NULL DEFAULT FALSE,
    external_id   TEXT,                          -- opaque host-defined linkage (see §11)
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at  TIMESTAMPTZ,                   -- NULL while draft/archived; set on first publish
    deleted_at    TIMESTAMPTZ                    -- soft-delete
);
CREATE INDEX blog_posts_status_published_idx ON blog_posts (status, published_at DESC NULLS LAST) WHERE deleted_at IS NULL;
CREATE INDEX blog_posts_author_idx ON blog_posts (author_id) WHERE deleted_at IS NULL;
CREATE INDEX blog_posts_external_idx ON blog_posts (external_id) WHERE external_id IS NOT NULL;
```

Slug uniqueness is **global**. (osg's session-notes are typically date-prefixed slugs like `shadowmaze-2026-02-10-bancroft` which are naturally unique.)

`external_id` is opaque to blog — consumers populate it for their own linkage purposes; blog itself never interprets it. See §11.

### Tags

```sql
CREATE TABLE blog_tags (
    id          BIGSERIAL PRIMARY KEY,
    slug        TEXT NOT NULL UNIQUE,            -- lowercase, hyphenated
    name        TEXT NOT NULL,                   -- display name
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE blog_post_tags (
    post_id BIGINT NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    tag_id  BIGINT NOT NULL REFERENCES blog_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (post_id, tag_id)
);
CREATE INDEX blog_post_tags_tag_idx ON blog_post_tags (tag_id);
```

Tag namespace is flat. No tag hierarchy in v0.1. Admins can rename and merge tags (§8 admin endpoints).

### Comments

```sql
CREATE TABLE blog_comments (
    id          BIGSERIAL PRIMARY KEY,
    post_id     BIGINT NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    user_id     BIGINT NOT NULL REFERENCES blog_users(id) ON DELETE RESTRICT,
    body_md     TEXT NOT NULL,
    body_html   TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ                       -- soft-delete (admin)
);
CREATE INDEX blog_comments_post_idx ON blog_comments (post_id, created_at) WHERE deleted_at IS NULL;
```

Flat. No threading in v0.1. Authenticated users only; anonymous commenting deliberately not supported.

### Revisions

```sql
CREATE TABLE blog_post_revisions (
    id          BIGSERIAL PRIMARY KEY,
    post_id     BIGINT NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    body_md     TEXT NOT NULL,
    edited_by   BIGINT NOT NULL REFERENCES blog_users(id) ON DELETE RESTRICT,
    edited_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    note        TEXT NOT NULL DEFAULT ''         -- optional edit summary
);
CREATE INDEX blog_post_revisions_post_idx ON blog_post_revisions (post_id, edited_at DESC);
```

A revision row is written on every save that changes `title` or `body_md`. The current post row is the live version; the revision table is the full history. Storage grows with edits but blog posts are not high-frequency mutation; acceptable.

## 7. Authoring UX

This is the make-or-break feature. The whole point of this module is that authoring works for non-developer contributors. Every design decision in this section is judged on that one criterion: "can a campaign player who doesn't use git successfully write a session report from start to publish?"

### The editor: infodancer/mdedit

The body editor is the `infodancer/mdedit` component, not a hand-rolled control: blog supplies an `mdedit.Field` to the edit template and lets the module own the display↔edit↔preview↔save loop. A single full-page form at `GET /new` and `GET /p/{id}/edit`. Layout (responsive, falls to single column on narrow viewports):

```
┌──────────────────────────────────────────────────────────────────┐
│ Title:    [ The Plague at Bobwhite Castle                     ]  │
│                                                                  │
│ Author:   ( me as Bancroft  ▾ )                                  │
│                                                                  │
│ ┌────────────────────────────┬───────────────────────────────┐   │
│ │ [B] [I] [H] [link] [list]  │  Preview                       │   │
│ ├────────────────────────────┤  ┌─────────────────────────┐   │   │
│ │                            │  │ # The Plague at …       │   │   │
│ │  *body markdown here*      │  │ rendered html…           │   │   │
│ │                            │  │                          │   │   │
│ │                            │  └─────────────────────────┘   │   │
│ └────────────────────────────┴───────────────────────────────┘   │
│                                                                  │
│ Tags:     [ shadowmaze, 2026-02-10, bancroft           ]         │
│                                                                  │
│ Excerpt:  [ (optional override; auto-derived if empty)  ]        │
│                                                                  │
│ [Save draft] [Publish]                  [Delete]                 │
└──────────────────────────────────────────────────────────────────┘
```

### Why mdedit rather than a bespoke textarea + toolbar

mdedit already encodes the decisions this section originally argued for, and the editor is exactly the kind of cross-site component the portfolio extracts once rather than rebuilds per consumer:

- The `<textarea>` stays the source of truth and works with JS off; an editor adapter (EasyMDE today, behind a swappable seam) enhances it in place. Markdown remains the storage and export format — no WYSIWYG round-trip bugs.
- **The server is the only renderer.** mdedit deliberately disables the client editor's own preview, so the preview an author sees comes from the exact `infodancer/markdown` pipeline that sanitizes and stores the body. No client/server divergence for an attacker or a confused author to exploit.
- The toolbar is a configured profile (`Field.Toolbar`), not custom JS. blog uses the `full` profile (bold, italic, heading, lists, blockquote, link, code) for long-form posts; comments use the `standard` profile (no headings) to match their tighter render preset (§10).
- Vendored, pinned, SRI-hashed assets — no npm, no CDN, no icon-font fetch. blog inherits that supply-chain posture for free.

blog wires the four `Field` URLs to its own endpoints:

| `mdedit.Field` URL | blog endpoint |
|---|---|
| `DisplayURL` | `GET /p/{slug}` (rendered post; Cancel target) |
| `EditURL` | `GET /p/{id}/edit` |
| `SaveURL` | `POST /p/{id}/edit` (persists, returns display partial) |
| `PreviewURL` | `POST /preview` (server-rendered preview fragment) |

### Live preview

mdedit's `Field.LivePreview` opts the body field into debounced server-side preview as the author types; the `POST /preview` handler renders the submitted markdown through `infodancer/markdown` (§10) and returns the HTML fragment. Because preview shares the render path with save, what the author sees is an honest picture of what gets stored, not a client approximation — the single biggest authoring quality-of-life win.

### Author selection

A `<select>` of the authoring user's `blog_authors` rows. Defaults to the most-recently-used author. Admins also see a "Post as someone else" option that opens a dropdown of all authors.

A campaign player with two characters (Bancroft for Monday games, Cael for Tuesday) sees both in the dropdown. Switching authors changes the post's `author_id`; nothing else.

### Tag input

A comma-separated text input. On save, the handler splits on commas, slugifies each token, and:

- Existing tags (by slug) get added to `blog_post_tags`.
- New tags are created in `blog_tags`, then added.

Autocomplete (htmx `hx-trigger="input changed delay:200ms"` posting to `/tags/suggest`) shows matching existing tags as the user types. Server-rendered dropdown; click inserts.

### Slug

Auto-generated from the title on first save (`slugify(title)`). On subsequent saves, the slug is preserved — renaming the title does not change the URL. Admins can override the slug via the edit form's "Advanced" disclosure.

### Save lifecycle

| Action | Effect |
|--------|--------|
| **Save draft** | `status = 'draft'`, `updated_at = now()`, `published_at` unchanged (NULL on first save). Stores a revision row. Post is invisible on public read endpoints. |
| **Publish** | `status = 'published'`, `published_at = now()` on first publish (else preserved). Visible on public reads. Stores a revision. |
| **Unpublish** (back to draft) | `status = 'draft'`, `published_at` preserved (so re-publishing keeps the original date). Stores a revision. |
| **Archive** | `status = 'archived'`. Invisible on public reads except by direct URL with admin context. Used for "we don't want to delete this but it shouldn't show up." |
| **Delete** | `deleted_at = now()`. Soft. Invisible everywhere except admin's deleted-posts view. Admin can restore. |

A "Preview before publishing" option is implicit — the live-preview pane already shows the rendered output. Explicit `?preview=1` URL for sharing a preview to non-editors is a v0.2 nicety.

### Authorization

- **Create post**: any authenticated user with at least one `blog_author` row owned by them.
- **Edit post**: post author's `user_id == request_user.id` OR `request_user.admin`.
- **Publish / unpublish / archive**: same as edit.
- **Delete**: same as edit. Hard delete (admin-only) reachable from the admin deleted-posts view.

No "grace period" gating like faq has. Blog post edits are expected long after authoring; that's normal CMS behavior.

## 8. HTTP surface

All paths relative to the configured `BasePath`.

### Read (anonymous OK)

| Method | Path | Purpose |
|---|---|---|
| GET | `/` | Index — most recent published posts, paginated |
| GET | `/p/{slug}` | Single post (404 if draft and not author/admin) |
| GET | `/p/{slug}/revisions` | Revision history (visible to author + admin) |
| GET | `/authors` | Authors index |
| GET | `/authors/{slug}` | Author page (posts by author) |
| GET | `/tags` | Tags index |
| GET | `/tags/{slug}` | Tag page (posts with tag) |
| GET | `/archive/{year}` | Posts in year |
| GET | `/archive/{year}/{month}` | Posts in year/month (1-12, zero-padded) |
| GET | `/search` | Search results (q parameter; FTS) |
| GET | `/feed.xml` | Atom feed of N most recent published posts |
| GET | `/sitemap.xml` | Sitemap index |
| GET | `/sitemap/{N}.xml` | Sitemap part |

### Write (authenticated)

| Method | Path | Purpose |
|---|---|---|
| GET | `/new` | New-post form |
| POST | `/new` | Create draft |
| GET | `/p/{id}/edit` | Edit form |
| POST | `/p/{id}/edit` | Update post fields |
| POST | `/p/{id}/publish` | Transition to published |
| POST | `/p/{id}/unpublish` | Transition to draft |
| POST | `/p/{id}/archive` | Transition to archived |
| POST | `/p/{id}/delete` | Soft-delete |
| POST | `/p/{id}/restore` | Restore from soft-delete (admin) |
| POST | `/preview` | Render markdown to HTML (htmx live preview) |
| POST | `/p/{id}/comments` | Add comment |
| POST | `/comments/{id}/edit` | Edit own comment |
| POST | `/comments/{id}/delete` | Delete (author or admin) |
| GET | `/tags/suggest` | Tag autocomplete (htmx fragment) |

### Admin (admin role required)

| Method | Path | Purpose |
|---|---|---|
| GET | `/admin` | Dashboard |
| GET | `/admin/posts` | All posts, any status |
| GET | `/admin/posts/deleted` | Soft-deleted posts |
| GET | `/admin/authors` | Manage authors |
| POST | `/admin/authors` | Create author |
| POST | `/admin/authors/{id}` | Update author |
| POST | `/admin/authors/{id}/delete` | Delete author (only if no posts) |
| POST | `/admin/authors/{id}/associate` | Associate author with a user |
| GET | `/admin/tags` | Manage tags |
| POST | `/admin/tags/{id}/rename` | Rename tag |
| POST | `/admin/tags/{id}/merge` | Merge into another tag |
| POST | `/admin/p/{id}/feature` | Pin to top of index |
| POST | `/admin/p/{id}/unfeature` | Remove pin |
| POST | `/admin/p/{id}/hard-delete` | Permanently delete (after soft-delete) |
| POST | `/admin/import-hugo` | Trigger Hugo-bundle import (see §12) |

### CSRF

All write endpoints require a CSRF token, validated by the module's `csrfGuard` middleware (same shape as faq's). Token surfaced to forms via `{{ .CSRFToken }}` view-model field.

## 9. Templates and theming

`templates/` is `embed:all`. `blog.Config.Templates fs.FS` overlays per-template overrides. Same layered approach as faq.

### Consumption of `infodancer/ui`

Blog's CSS lives in `static/css/blog.css`. It declares `--blog-*` variables chained through `--app-*`:

```css
.blog {
  --blog-color-fg:           var(--app-color-fg, #1a1a1a);
  --blog-color-bg:           var(--app-color-bg, #fafafa);
  --blog-color-bg-raised:    var(--app-color-bg-raised, #ffffff);
  --blog-color-fg-muted:     var(--app-color-fg-muted, #5a5a5a);
  --blog-color-border:       var(--app-color-border, rgba(0, 0, 0, 0.12));
  --blog-color-accent:       var(--app-color-accent, #0b5394);
  --blog-color-accent-hover: var(--app-color-accent-hover, #1a73b9);
  --blog-color-prose-fg:     var(--app-color-prose-fg, var(--blog-color-fg));
  --blog-color-danger:       var(--app-color-danger, #b3261e);
  --blog-color-success:      var(--app-color-success, #1d7a3d);
  --blog-font-body:          var(--app-font-body, system-ui, sans-serif);
  --blog-font-display:       var(--app-font-display, var(--blog-font-body));
  --blog-font-mono:          var(--app-font-mono, ui-monospace, monospace);
  --blog-space:              var(--app-space, 16px);
  --blog-space-sm:           var(--app-space-sm, 8px);
  --blog-space-lg:           var(--app-space-lg, 32px);
  --blog-radius:             var(--app-radius, 6px);
  --blog-radius-pill:        var(--app-radius-pill, 9999px);
  --blog-max-width-prose:    var(--app-max-width-prose, 65ch);
}
```

All `.blog-*` selectors read from `--blog-*` tokens. With `infodancer/ui` loaded on the page, blog inherits the host palette automatically. Without it, the fallback constants kick in and blog renders standalone.

This is the three-level cascade:

1. `--blog-color-fg` (module-internal CSS reads this)
2. `--app-color-fg` (inherited from `infodancer/ui` if loaded)
3. Hardcoded fallback (rendered standalone)

Consumer sites that want to deviate from `infodancer/ui`'s defaults for blog specifically can override `--blog-*` directly. Rare; expected to be empty in normal use.

### Templates

Page templates under `templates/pages/`:

- `index.gohtml` — homepage / most recent
- `post_detail.gohtml` — single post
- `post_edit.gohtml` — author-facing edit form (also used for create)
- `author.gohtml` — author page
- `authors_index.gohtml` — list of authors
- `tag.gohtml` — tag page
- `tags_index.gohtml` — tag cloud / list
- `archive.gohtml` — year / month archive
- `search.gohtml` — search results
- `admin/*.gohtml` — admin pages

Partials under `templates/partials/`:

- `post_card.gohtml` — post summary in an index/listing context
- `comment.gohtml` — single comment block
- `editor_toolbar.gohtml` — the markdown editor's toolbar (HTML; JS is inline)
- `tag_chip.gohtml` — tag pill in metadata rows
- `pagination.gohtml` — prev/next controls
- `nav.gohtml` — module-internal nav (consumer can override)

Hosts override individual templates via `Config.Templates`. Same shape as faq.

### Template view-models

Pattern matches faq's §9.2. Each page template maps to a struct in `internal/handler/viewmodel.go`. Documented in README once implementation lands.

## 10. Markdown rendering

Rendering and sanitizing are delegated to `github.com/infodancer/markdown` — the shared, independently-versioned goldmark + bluemonday pipeline that is the portfolio's single sanitization boundary. blog carries no `internal/render` or `internal/sanitize`; it constructs `*markdown.Renderer` values at `New()` and reuses them.

- **Post bodies** use the `Rich()` preset: GFM (tables, strikethrough, autolinks, task lists) plus the inline HTML faq relies on for SE-style content (kbd, sub, sup, mark).
- **Comments** use a tighter preset (`Comment(LinkRelativeOnly)`-equivalent): inline emphasis/code/lists/blockquote, no headings/images/tables, same-origin links only — matching the `standard` mdedit toolbar comments get (§7).

bluemonday is the boundary regardless of the goldmark options chosen — script/style/iframe/object/embed/form and every `on*` handler are stripped, and dangerous URL schemes (`javascript:`, `vbscript:`, `data:text/html`, `file:`) are rejected — and its XSS corpus runs against every preset.

The `infodancer/markdown` extraction this section originally deferred has happened: markdown is published and tagged, faq migrates onto it, and blog consumes it from day one rather than duplicating the pipeline.

Bodies are rendered to HTML on save and cached in `blog_posts.body_html`. The cache is regenerated on every save; reading is a single column fetch with no per-request rendering. `POST /preview` (§7) renders through the same renderer, so preview and stored output never diverge.

## 11. External linkage (osg-shaped, but generic)

The osg session-notes use case wants each blog post to optionally link to a specific row in osg's `directory.sessions` table (campaign session metadata, attendance, etc.). Blog does **not** know what a session is; that's osg's domain.

The mechanism: `blog_posts.external_id` is an opaque `TEXT NULL` column. osg sets it to the session UUID when creating a post via the editor. Blog stores and indexes it (`blog_posts_external_idx`) but otherwise ignores it. Read-side template view-models include `ExternalID` as a string the consumer can interpret in overridden templates if they want to (e.g., osg overrides `post_detail.gohtml` to add a "Session details →" link to its directory page).

If a consumer doesn't use external linkage, the column stays NULL and nothing happens. The feature has zero cost when unused.

This is a deliberate generic-mechanism choice over an osg-specific schema column. Other consumers (mjh linking posts to projects, amyhunter linking posts to art pieces) can use the same column for their own purposes.

## 12. Hugo bundle migration

osg has a corpus of existing Hugo session-notes as page bundles:

```
content/shadowmaze/2026-02-10-bancroft/
├── index.md          # frontmatter + body
└── *.webp            # images referenced from the body
```

The migration tool is a CLI (`cmd/blog-import-hugo`) and an admin endpoint (`POST /admin/import-hugo`). It takes a path to a Hugo content directory and:

1. Walks the directory recursively, finding `index.md` files inside leaf directories.
2. Parses TOML / YAML frontmatter (Hugo supports both; osg uses TOML).
3. Creates a `blog_posts` row per bundle with:
   - `title` from frontmatter `title`
   - `slug` from frontmatter `slug` if present, else derived from the directory name
   - `author_id` looked up by `display_name = frontmatter.author`; created if not found (with `user_id = NULL` until admin associates)
   - `body_md` from the markdown body
   - `body_html` generated via the standard pipeline
   - `published_at` from frontmatter `date`
   - `created_at` same as `published_at`
   - `status = 'published'`
   - `external_id` set if the consumer provides a mapping (e.g., osg maps each bundle to its session UUID)
   - Tags from frontmatter `tags` — created if missing
4. **Images are NOT migrated in v0.1.** The body retains its relative image references; admin can either fix them by hand post-import or wait for v0.2 media handling.
5. Reports a summary: posts created, posts skipped (duplicate slug), authors created, tags created.

The tool is **idempotent**: running it twice over the same corpus does not create duplicates. Re-running updates posts where the source markdown has changed. Slug is the dedup key.

osg's flow on launch: import the existing corpus once into the live blog instance, then the campaign players take over authoring inside the editor. The Hugo content directory can be deleted from osg's repo once the import is verified.

## 13. Search

Postgres full-text search on `title` and `body_md` via a generated `tsvector` column. Same shape as faq's search:

```sql
ALTER TABLE blog_posts ADD COLUMN search_tsv tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(body_md, '')), 'B')
    ) STORED;
CREATE INDEX blog_posts_search_idx ON blog_posts USING GIN (search_tsv);
```

`GET /search?q=…` runs `WHERE search_tsv @@ websearch_to_tsquery('english', $1)` with `ts_rank` ordering. Returns published, non-deleted posts only (admin search includes drafts).

Comments are not searched in v0.1; defer if a consumer asks.

## 14. Config

```go
type Config struct {
    // Required
    DB       *pgxpool.Pool
    Resolver bloguser.UserResolver

    // Strongly recommended
    Logger    *slog.Logger    // defaults to slog.Default()
    BasePath  string          // mount path; defaults to ""
    SiteName  string          // displayed in chrome
    HomeURL   string          // back-to-site link

    // Optional behavior
    AutoMigrate    bool       // run goose migrations on New()
    PageSize       int        // index pagination; default 20
    FeedSize       int        // entries in /feed.xml; default 20
    AnonymousReads bool       // allow anonymous read of published posts; default true
    Templates      fs.FS      // per-template overlay
    Static         fs.FS      // per-file static overlay

    // Authoring policy
    AllowComments  bool       // default true
    MinAdminRole   string     // OIDC claim that grants admin; consumer parses via Resolver
}
```

Env-var driven defaults documented in README once implementation lands.

## 15. Testing

Same approach as faq: integration tests against a real Postgres via `testcontainers`, build tag `integration` for the expensive tests, pure-Go unit tests for renderer / slug / sanitize policies without the tag.

Coverage targets (per the OSS polish checklist):

- Every sqlc query has at least one integration test exercising it.
- Every handler has happy-path + at least one error-path test.
- Hugo importer has fixture tests over a small synthetic content tree.
- Markdown renderer has round-trip tests for SE-style inline HTML (kbd/sub/sup/mark).
- CSRF middleware tested via fixture requests.

## 16. Security

- **CSRF**: middleware on every write endpoint. Same shape as faq.
- **XSS**: all rendered markdown passes through `infodancer/markdown` (goldmark + bluemonday), the audited portfolio sanitization boundary (§10). Authors can write inline HTML in posts (kbd/sub/sup/mark) because that preset allows them; script/style/iframe/event-handlers and dangerous URL schemes are stripped. The client editor's own preview is disabled (§7), so the server is the only renderer — preview and stored output share one sanitization path.
- **Authentication**: handled entirely by the consumer's `UserResolver`. Blog does not run OIDC itself.
- **Authorization**: enforced in handlers, not in templates. Templates show/hide based on view-model booleans (`CanEdit`, `CanDelete`) computed by the handler.
- **SQL injection**: sqlc-generated typed queries; no string concatenation into SQL.
- **Open redirect**: any user-supplied redirect target validated against an allowlist of relative paths under `BasePath`.

## 17. Validation of infodancer/ui token vocabulary

This DESIGN.md exists in part to validate that `infodancer/ui` v0.1's 26-token vocabulary covers a second known full-UI consumer (faq is the first; blog is the second). The discipline is to enumerate every visual surface blog has and confirm it maps to existing tokens.

### Surfaces and the tokens they need

| Surface | Tokens used | Status |
|---------|-------------|--------|
| Post body (long-form prose) | `--app-color-prose-fg`, `--app-color-bg`, `--app-font-body`, `--app-line-height-body`, `--app-max-width-prose` | ✓ covered |
| Post titles (h1, page titles) | `--app-font-display`, `--app-line-height-display`, `--app-color-fg` | ✓ covered |
| Post metadata (date, author, tags row) | `--app-color-fg-muted`, `--app-font-body`, `--app-space-sm` | ✓ covered |
| Tag chips / pills | `--app-color-bg-raised`, `--app-color-border`, `--app-radius-pill`, `--app-color-fg-muted` | ✓ covered |
| Author byline | `--app-color-fg-muted` | ✓ covered |
| Comment cards | `--app-color-bg-raised`, `--app-color-border`, `--app-radius`, `--app-space` | ✓ covered |
| Code blocks (inline + fenced) | `--app-font-mono`, `--app-color-bg-raised`, `--app-color-border` | ✓ (already styled by `infodancer/ui` `base.css`) |
| Index post cards | `--app-color-border`, `--app-color-bg-raised`, `--app-radius`, `--app-space` | ✓ covered |
| Pagination controls | `--app-color-accent`, `--app-color-border`, `--app-color-fg-muted` | ✓ covered |
| Draft badge | `--app-color-fg-muted`, `--app-radius-pill` | ✓ covered |
| Published badge | `--app-color-success`, `--app-radius-pill` | ✓ covered |
| Editor toolbar | `--app-color-bg-raised`, `--app-color-border`, `--app-radius-sm` | ✓ covered |
| Editor textarea | `--app-font-mono`, base.css form input rules | ✓ covered |
| Live preview pane | `--app-color-bg-raised`, `--app-color-border`, `--app-color-prose-fg` | ✓ covered |
| Save / Publish CTA buttons | `--app-color-accent`, base.css button rules | ✓ covered |
| Delete / destructive buttons | `--app-color-danger` | ✓ covered |
| Form validation errors | `--app-color-danger`, `--app-color-bg-raised` | ✓ covered |
| Save confirmations | `--app-color-success` | ✓ covered |
| Admin tables | `--app-color-border`, `--app-color-bg-raised`, `--app-space` | ✓ covered |
| Tag autocomplete dropdown | `--app-color-bg-raised`, `--app-color-border`, `--app-radius-sm` | ✓ covered |
| Search results | `--app-color-prose-fg`, `--app-color-fg-muted` | ✓ covered |
| Feed icon / link | `--app-color-fg-muted`, `--app-color-accent-hover` | ✓ covered |

**Verdict: vocabulary holds up.** Every surface blog has maps cleanly to an existing token.

The editor toolbar, textarea, and live-preview surfaces are now styled by `infodancer/mdedit`'s own CSS, which consumes the same `--app-*` tokens — so blog inherits them rather than styling them directly. The coverage check still holds; the token *consumer* for those rows shifts from blog to mdedit.

### Token gaps surfaced (none blocking)

Two would-be-nice additions that did *not* make the v0.1 cut, that blog would use if they existed:

1. **`--app-color-bg-active`** — a third background variant distinct from `bg` (page) and `bg-raised` (cards) for "currently selected" UI like an active tag filter pill or the current page in pagination. Workaround: use `--app-color-accent` with reduced opacity via `color-mix()`, or rely on a thicker `--app-color-accent` border to mark active state. Acceptable for v0.1; revisit if real visual confusion emerges.
2. **Shadow scale** (e.g., `--app-shadow-card`) — for elevation on index-page post cards. `infodancer/ui` v0.1 deferred shadow tokens deliberately ("highly design-specific"). Workaround: use a slightly heavier border instead. Acceptable.

Neither gap blocks v0.1. Both are reasonable v0.x.y additions to `infodancer/ui` once a third full-UI consumer concurs.

### Token names that proved out

The role-based naming convention (`--app-color-prose-fg` rather than `--app-text-color`, `--app-radius-pill` rather than `--app-border-radius-full`) holds up against a second consumer with different visual surfaces from faq. Names communicate *purpose* not *value*, which is exactly what enables consumer-site override without semantic drift.

The decision to split `--app-color-fg` from `--app-color-prose-fg` proves correct: blog has both a UI-foreground need (button text, nav, admin tables) and a prose-reading need (post bodies), and a few sites will want them tuned separately.

The `--app-space` doubling scale (xs/sm/(default)/lg/xl) is sufficient for blog's layout needs; no missing tier.

### Token names that would tighten

None to retire. The v0.1 vocabulary is tight enough that nothing in the 26 is unused by blog.

## 18. Open questions

- **Comment moderation defaults.** v0.1 lets any authenticated user comment immediately. If spam is a real problem (it shouldn't be for a small portfolio site behind OIDC) we add a moderation queue in v0.2. The OIDC requirement should be enough friction to deter casual spam.
- **Migrating Hugo authors without users.** When an imported post's frontmatter lists an author name that doesn't correspond to any registered OIDC user, we create a `blog_authors` row with `user_id = NULL`. An admin later associates the author with a real user. The UX flow for "associate this author with an existing user" needs to be designed during M5 (admin).
- **Per-author slug uniqueness.** v0.1 makes slugs globally unique. If two campaigns each want a `session-1` post, they can't both have that slug. Mitigation: osg slugs are date-prefixed anyway. Reconsider if a real conflict emerges.
- **Excerpt derivation.** When `excerpt` is empty, do we auto-derive from the first paragraph, the first 200 chars, the first sentence, or use the post's first heading + first paragraph? v0.1 defaults to "first 200 chars of body, stripped of markdown syntax, ellipsized." Easy to change.
- **Atom feed pagination.** Single feed file with N most recent. If a consumer outgrows that (unlikely for portfolio sites), v0.2 adds paged feeds.
- **Author pages: archive view vs. card view.** Author detail page shows their posts. List view or chronological archive? v0.1: chronological list with post-card partial. Reconsider if the author list grows beyond ~100 posts.

## 19. Milestones (v0.1)

- **M0 — skeleton (1 day)**: repo scaffolding (already done; this DESIGN.md is the design artifact). go.mod, empty `blog.go`, `bloguser/user.go`, embed FS declarations, empty handler stubs returning 501. Taskfile targets. CI workflow.
- **M1 — schema + store (1-2 days)**: all migrations, sqlc queries, store-layer integration tests (testcontainers).
- **M2 — read path (2-3 days)**: index, post detail, author pages, tag pages, archive, feed.xml, sitemap. Public templates. Wire `infodancer/markdown` renderers (Rich for bodies, tighter preset for comments). Search.
- **M3 — write path (2-3 days)**: new/edit/save/publish/unpublish/archive/delete with auth, CSRF, view-models, redirects. Comment add/edit/delete.
- **M4 — authoring UX (2-3 days)**: mount `infodancer/mdedit` — `Field` wiring to the edit/save/preview endpoints, `/preview` rendering through `markdown`, `HeadTags` + asset mounting — plus tag autocomplete, author selector, and draft-vs-publish form behavior. The single biggest UX-quality milestone.
- **M5 — admin (1-2 days)**: posts/authors/tags/feature/merge admin endpoints + templates.
- **M6 — revisions (1 day)**: edit history schema is already in M1; M6 adds the diff-rendering view and the `/p/{slug}/revisions` page.
- **M7 — theme + infodancer/ui consumption (1 day)**: refactor `static/css/blog.css` to consume `--app-*` tokens via `--blog-*` chaining. Add `head_extra` block to `base.gohtml`. Make `partials/nav.gohtml` overridable via `Config.Templates`. Parallels faq M6a-12.
- **M8 — Hugo bundle migration (2-3 days)**: importer package, CLI command, admin endpoint, idempotency, fixture tests against a synthetic Hugo content tree. Image migration explicitly deferred.
- **M9 — osg integration (2-3 days)**: mount blog under `/blog` in osg's Go server with a `UserResolver` adapter over osg's existing session model, run the Hugo importer over osg's session-notes corpus, theme integration. Smoke tests against the live osg binary. Migration of the existing content is gradual after launch; players start using the new editor immediately for new posts.
- **M10 — polish + v0.1.0 tag**: OSS polish checklist [[matthew-oss-polish-checklist]].

Total realistic v0.1 effort: ~2-3 weeks of focused work. M0 + M1 + M2 + M3 + M4 are the critical path (~10 days); M5–M8 are sequence-flexible; M9 is the integration. Tag once osg is running blog successfully.

## 20. v0.2 candidates

- **Image / media upload** in the editor (drag-drop, server-side resize, storage backend).
- **Draft autosave** (JS state, periodic background POST).
- **Scheduled publishing** (`published_at` in the future, cron / job runner promotes drafts).
- **Threaded comments** (reply-tree).
- **Comment moderation queue** if spam emerges as a real problem.
- **Series** (multi-post sequences with prev/next nav between linked posts).
- **Markdown document export** — equivalent of faq's Track E for blogs. Compile a year's posts into a downloadable `.md` file for offline reading.
- **Per-author RSS / Atom feeds** (`/authors/{slug}/feed.xml`).
- **Public revision diff view** (admin sees diffs in v0.1 via the revisions page; making this public is v0.2).
- **Extract shared packages** with faq: `infodancer/userctx` (UserResolver type), `infodancer/csrf` (middleware). Trigger: when a third module wants the same shape. (`infodancer/markdown` and `infodancer/mdedit` are already extracted, published, and consumed — see §3, §7, §10.)
- **Anonymous read-only API** for consumer sites that want to render a "Latest blog posts" widget elsewhere without going through HTML.
