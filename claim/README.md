# claim

Ownership-claim invitations for entities a site holds on someone's behalf.

A directory listing is often created before its subject has an account -- imported from a public source, or entered by an admin who met the business at a convention. The listing cannot be given an owner, because there is no user to point at. This component records an invitation addressed to an email, mints a one-time link, and marks the invitation redeemed when someone follows that link and authenticates. The host does the rest.

## What it owns, and what it does not

It owns invitations: token minting, hash storage, expiry, single-use redemption, revocation, and the audit history.

It does not own entities, users, ownership, email delivery, or HTTP routes. The host decides what an entity is, who may issue an invitation, what an accepted claim means, and how the link reaches its recipient. That division is what lets one component serve a vendor directory, a convention listing, and an author page without knowing anything about any of them.

## Identity is strings

Every identifier crossing the boundary is a string the host chooses -- `entity_type` and `entity_ref` for the thing, `created_by` and `consumed_by` for the people. The schema has no foreign keys outside its own tables, so it installs into any host database, and deleting a host user cannot erase the record of who issued or accepted an invitation.

`entity_ref` should be a stable public identifier such as a slug. A row id would make the audit trail unreadable without a join into a table this component cannot see.

## Usage

```go
m, err := claim.New(claim.Config{DB: pool, AutoMigrate: true})

// Issue. The returned token is the only copy that will ever exist.
token, inv, err := m.Invite(ctx, claim.InviteParams{
    EntityType: "vendor",
    EntityRef:  "morine-emporium",
    Email:      "hello@example.test",
    CreatedBy:  adminHandle,
})
url := "https://example.org/claim/" + token

// Resolve without redeeming, so a landing page can name what is being claimed.
inv, err := m.Lookup(ctx, token)

// Redeem, once.
inv, err := m.Consume(ctx, token, userIdentifier)
```

`Lookup` and `Consume` distinguish `ErrNotFound`, `ErrExpired`, `ErrConsumed`, and `ErrRevoked`, so a host can tell someone what actually happened. A single "invalid link" for all four sends a person with an expired invitation hunting for a typo.

## Tokens

`Invite` returns the token; the database stores only its SHA-256. A host that displays a claim URL must therefore display it at creation and offer re-issue rather than redisplay. This is the same posture as a password reset token, for the same reason: possession of the link is enough to take ownership. Storing the token so it could be shown again would mean any database read, backup, or stray `SELECT` yields working takeover links for every pending listing.

Tokens are 32 bytes from `crypto/rand`, base64url encoded. They are located by hash through a unique index, so there is no application-level comparison of secrets to get wrong -- a wrong token matches no row.

Single use is enforced in the `UPDATE`'s `WHERE` clause rather than by a read-then-write in Go, so two simultaneous claims cannot both succeed. The package's internal tests drive that query directly, because the exported `Consume` checks state first and would otherwise hide whether the SQL is safe.

Issuing a new invitation revokes any outstanding one for the same entity, and a partial unique index makes that an invariant the database enforces rather than a rule the application remembers.

Default TTL is 90 days (`claim.DefaultTTL`), overridable per host.

## Host responsibilities

- **Serve the claim page with `Referrer-Policy: no-referrer`**, exchange the token for a session on landing, and redirect to a tokenless URL so it stops living in the address bar and in logs.
- **Keep claim URLs out of the index**: `noindex`, robots, and no sitemap entry.
- **Rate limit** redemption attempts per token and per IP.
- **Render a generated URL as text, not as a link.** A clickable single-use token on an admin page is a footgun: following it while signed in as an admin consumes the token and assigns the listing to the admin.
- **Verify the email before acting on `PendingForEmail`.** Granting ownership on a bare string match would let anyone claim a listing by signing up with somebody else's address. Check the identity provider's `email_verified`.

## Database

Tables are prefixed `claim_`, and migrations are tracked in a dedicated `claim_goose_db_version` so several components can share one database. `New` runs them when `AutoMigrate` is set; otherwise call `Migrate`.
