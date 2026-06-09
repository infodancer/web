# profile

A reusable, mountable component for the public persona every community site
needs: a unique handle, a display name, a short markdown bio, an avatar, and
bio links. Built for [infodancer/web#52](https://github.com/infodancer/web/issues/52);
first consumers are OSG and SF.

## What it's for

Community sites keep re-implementing the same "who is this user, publicly"
surface: a claimable handle, a display name, a paragraph about the user, a
picture for forum posts and rosters, and a few links. This module owns that
surface once -- the edit form, the public profile page, avatar upload, a
deterministic identicon fallback, and an embeddable card other components
(forum posts, bylines) render inline.

What it deliberately does **not** own: users, sessions, authentication, or
password flows. Identity arrives per request through a host-supplied
resolver, and profiles are keyed by an opaque, host-supplied user key (e.g.
the host's `users.id` as text). The host's users table stays the host's FK
hub; webauth (or whatever IdP the host runs) keeps owning credentials.

## Mount contract

```go
m, err := profile.New(profile.Config{
    DB:       pool, // *pgxpool.Pool, owned by the host
    Resolver: profile.ResolverFunc(func(ctx context.Context, r *http.Request) (*profile.User, error) {
        u := session.UserFrom(r) // however the host resolves its session
        if u == nil {
            return nil, nil // anonymous: read-only access to public profiles
        }
        return &profile.User{Key: strconv.FormatInt(u.ID, 10), DisplayName: u.Name, Admin: u.IsAdmin()}, nil
    }),
    BasePath:  "/profile",
    SignInURL: "/login",

    // Optional: avatar uploads through web/media. Without these, profiles
    // fall back to the generated identicon.
    Blobs:   blobs,
    Objects: mediaStore,
    Avatar:  media.Config{Category: "avatar", PublicBaseURL: "/media/avatar"},
})
if err != nil { ... }
if err := m.Migrate(ctx); err != nil { ... } // profile_goose_db_version

mux.Handle("/profile/", http.StripPrefix("/profile", m.Routes()))
```

Routes (relative to the mount point):

| Route | Purpose |
|-------|---------|
| `GET /edit` | The signed-in user's own profile form |
| `POST /edit` | Save display name, bio, links; claim a handle |
| `POST /avatar` | Upload (or remove) the profile picture |
| `GET /avatar/{handle}` | Deterministic SVG identicon fallback |
| `GET /{handle}` | Public profile page |
| `GET /static/` | Module CSS + shared ui assets |

The embeddable card for forum posts, rosters, and bylines:

```go
html, err := m.Card(ctx, userKey) // template.HTML, or profile.ErrNotFound
```

## Behavior and policy

- **Handles** are lowercase-normalized (3-32 chars, alphanumerics and
  interior hyphens) and unique case-insensitively. A claimed handle is
  immutable except for admin users; route words and authority names are
  reserved (extend via `Config.ReservedHandles`).
- **Bio** is stored as markdown source and sanitized at render time through
  `infodancer/ui/markdown`'s untrusted-comment preset (safe-scheme links
  only). Raw HTML never survives.
- **Links** must be absolute http/https URLs (at most 8) and render with
  `rel="me nofollow ugc"`.
- **Avatars** go through `web/media`'s ingest pipeline (re-encode, metadata
  strip, content-address); the profile stores the canonical URL media
  returns. Profiles without an upload get a deterministic identicon derived
  from the handle -- no email hashes, unlike gravatar.
- **Display name** is seeded from the resolver's claim on first visit and
  owned by the user after the first save.

## Skinning (host chrome)

Pages render through `infodancer/ui`'s `ui/document`. Supply
`Config.Chrome` to wrap pages in the host's nav/footer/theme per request;
without it the module renders a standalone default using
`SiteName`/`HomeURL`. The module's structural CSS rides in `ExtraCSS` and
hooks the ui `--app-*` variables.
