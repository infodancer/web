# apitoken

Personal access tokens, so a script or command-line tool can call a site's API as a particular user.

A browser reaches a site through an OIDC session cookie behind same-origin CSRF checks, which a tool cannot use. The usual fallback is a shared secret in the environment: no per-user attribution, no revocation short of a redeploy, no record of use. This component lets a user mint named, scoped tokens on an account page, and lets a tool send one as `Authorization: Bearer <token>`.

## What it owns, and what it does not

It owns tokens: minting, hash storage, scopes, revocation, use tracking, the request dispatch that authenticates them, and the account page where users manage them.

It does not own users, sessions, or authorization. The host says who is signed in on the account page, turns a verified token into its own request context, and keeps applying its own permission checks behind that. A scope only narrows what a token may do; it never grants anything the user could not do in a browser.

## Identity is strings

Tokens belong to the host's user key, an opaque string. The schema has no foreign keys outside its own table, so it installs into any host database -- and so deleting a host user does not delete their tokens on its own. Call `DeleteForUser` when an account goes. Until it is called, such a token still fails: the host's `Authenticated` hook finds no user and returns `ErrNoUser`.

## Usage

```go
m, err := apitoken.New(apitoken.Config{
    DB:          pool,
    AutoMigrate: true,
    Prefix:      "xyz_pat_",
    Scopes: []apitoken.Scope{
        {Name: "notes:write", Description: "read and write your notes"},
    },
    // Build the same context the host's session middleware would.
    Authenticated: func(ctx context.Context, t *apitoken.Token) (context.Context, error) {
        u, err := users.ByKey(ctx, t.UserKey)
        if errors.Is(err, users.ErrNotFound) {
            return nil, apitoken.ErrNoUser
        }
        if err != nil {
            return nil, err
        }
        return withUser(ctx, u), nil
    },
    // For the account page.
    BasePath:    "/account/tokens",
    CurrentUser: requireSignedIn, // writes its own redirect when nobody is
    Chrome:      siteChrome,
})

// The routes a token may reach -- nothing else is reachable with one.
routes := m.Routes()
routes.HandleFunc("PUT /api/notes/{id}", "notes:write", putNote)
routes.Use(loadRoles) // host middleware, run after authentication

// The account page goes on the browser routes.
page, err := m.Handler()
mux.Handle("/account/tokens", page)
mux.Handle("/account/tokens/", page)

// Dispatch sits outside the CSRF guard and the session middleware.
handler := m.Dispatch(csrf(sessions(mux)), routes)
```

A handler can sit on both the browser mux and `routes`; `RequireScope` passes requests that carry no token, and `FromContext` tells the handler which kind it has.

## Tokens

A token is the host's prefix plus 32 bytes from `crypto/rand`, base64url without padding. The prefix lets `Dispatch` recognise the component's tokens without a database round trip, and makes a leaked token recognisable to people and to secret scanners -- pick one unique to the site.

The database stores only the token's SHA-256. The token carries 256 bits of entropy, so a slow KDF would add nothing, and the lookup is by hash through a unique index, so there is no application-level comparison of secrets to get wrong. The plain text is shown once, in the response to the mint form (`Cache-Control: no-store`), never in a redirect URL or a flash message. The account page afterwards shows only the first eight characters, to tell tokens apart.

Tokens do not expire; one lives until its owner revokes it. Revoked rows are kept and listed, for audit, and never authenticate. Revoking is scoped to the owner, so someone else's token id reads exactly like an id that does not exist. `last_used_at` is written at most once a minute per token.

## Request handling

`Dispatch(browser, routes)` looks at the `Authorization` header. Anything that is not a `Bearer` credential starting with the configured prefix goes to `browser`, untouched.

A token request:

- is a **401** (`WWW-Authenticate: Bearer error="invalid_token"`) when the token is malformed, unknown or revoked, or the host reports its user gone. It never falls through to `browser`, so a bad token cannot end up authenticated by a session cookie sent alongside it.
- is a **503** when the database or the host hook fails.
- otherwise has its `Cookie` header removed and goes to `routes`, with the token in the context.

`routes` holds only what the host registered, each route with a required scope. Anything else is a **403** `token_not_allowed`; a token without the route's scope is a **403** `insufficient_scope`. Registering a route with an unconfigured scope, or after `Dispatch`, panics: both are wiring mistakes, better caught at startup.

A token request skips the host's CSRF guard. CSRF works by abusing credentials the browser attaches on its own, and a bearer header is never attached that way. That is safe only because of the allow-list: a token cannot reach the host's ordinary form handlers at all.

## Host responsibilities

- **Mount `Dispatch` outermost**, outside the CSRF guard and session middleware, so a token request never touches either.
- **Register only API routes on `routes`.** Never the account page: a token able to mint tokens would make revocation meaningless.
- **Call `DeleteForUser`** when an account is deleted.
- **Gate the account page** as tightly as the host wants. `CurrentUser` decides who may manage tokens -- for example only users with a verified email.
- **Keep CSRF on for the account page** unless the host already checks every state-changing request (`Config.CSRF.Disable`).
