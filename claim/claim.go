// Package claim issues and redeems ownership-claim invitations for entities a
// host site holds on someone's behalf.
//
// The motivating case is a directory listing created by import or by an admin
// on behalf of a business that has no account yet. The listing cannot be
// assigned an owner, because there is no user row to point at. Instead the host
// records an invitation addressed to an email, hands over a one-time link, and
// the recipient becomes the owner when they follow it and authenticate.
//
// # What the module does and does not do
//
// It owns invitations: minting tokens, storing their hashes, expiry, single-use
// redemption, revocation, and the audit history. It does not own entities,
// users, ownership, email delivery, or HTTP routes. The host decides what an
// entity is, who may issue an invitation, what an accepted claim means, and how
// the link reaches its recipient. That split is what lets one component serve a
// vendor directory, a convention listing, and an author page without knowing
// anything about any of them.
//
// # Identity is strings
//
// Every identifier crossing this boundary is a string the host chooses:
// entity_type and entity_ref for the thing, created_by and consumed_by for the
// people. The schema has no foreign keys outside its own tables, so it installs
// into any host database and a deleted host user cannot erase the record of who
// issued or accepted an invitation.
//
// # Tokens
//
// [Module.Invite] returns the only copy of the token that will ever exist; the
// database holds a SHA-256 hash. A host that wants to display a claim URL must
// therefore display it at creation, and offer re-issue rather than redisplay.
// This is the same posture as a password reset token, and for the same reason:
// possession of the link is sufficient to take ownership.
package claim

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/infodancer/web/claim/db"
)

//go:embed db/migrations/*.sql
var migrationsFS embed.FS

// migrationsTableName is this component's dedicated goose version table,
// distinct from the default so its history never collides with the host's own
// migrations or with another component sharing the database.
const migrationsTableName = "claim_goose_db_version"

// tokenBytes is the entropy in a claim token. 256 bits is more than the 122 a
// UUIDv4 carries and removes any need to reason about how much is enough.
const tokenBytes = 32

// Errors returned by redemption. They are distinguished so a host can tell the
// recipient what actually happened; a single "invalid link" for all four sends
// someone with an expired invitation looking for a typo.
var (
	// ErrNotFound means no invitation matches the token.
	ErrNotFound = errors.New("claim: invitation not found")
	// ErrExpired means the invitation existed but its TTL has passed.
	ErrExpired = errors.New("claim: invitation expired")
	// ErrConsumed means the invitation was already redeemed.
	ErrConsumed = errors.New("claim: invitation already claimed")
	// ErrRevoked means a replacement invitation was issued for the entity.
	ErrRevoked = errors.New("claim: invitation revoked")
)

// Invitation is one claim invitation. It never carries the token, which exists
// only as the return value of [Module.Invite].
type Invitation struct {
	ID           int64
	EntityType   string
	EntityRef    string
	InvitedEmail string
	CreatedBy    string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
	ConsumedBy   string
	RevokedAt    *time.Time
}

// Live reports whether the invitation is still redeemable as of now.
func (i *Invitation) Live(now time.Time) bool {
	return i.ConsumedAt == nil && i.RevokedAt == nil && i.ExpiresAt.After(now)
}

// Module is the runtime entry point. Construct with [New].
type Module struct {
	cfg Config
	q   *db.Queries
}

// New constructs a Module from cfg, optionally running the embedded migrations.
func New(cfg Config) (*Module, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()

	m := &Module{cfg: cfg, q: db.New(cfg.DB)}
	if cfg.AutoMigrate {
		if err := m.Migrate(context.Background()); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// Migrate applies the embedded migrations. Safe to call repeatedly; goose
// tracks applied versions in claim_goose_db_version.
func (m *Module) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationsFS, "db/migrations")
	if err != nil {
		return fmt.Errorf("claim: migrate: %w", err)
	}
	sqldb := stdlib.OpenDBFromPool(m.cfg.DB)
	defer func() { _ = sqldb.Close() }()
	prov, err := goose.NewProvider(goose.DialectPostgres, sqldb, sub,
		goose.WithTableName(migrationsTableName),
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		return fmt.Errorf("claim: migrate: %w", err)
	}
	if _, err := prov.Up(ctx); err != nil {
		return fmt.Errorf("claim: migrate: %w", err)
	}
	return nil
}

// InviteParams describes an invitation to issue.
type InviteParams struct {
	// EntityType and EntityRef name the thing being claimed, in the host's own
	// vocabulary. EntityRef should be a stable public identifier such as a slug.
	EntityType string
	EntityRef  string

	// Email is the address the invitation is addressed to. The module records
	// it and does not send anything.
	Email string

	// CreatedBy identifies the issuing admin for the audit record.
	CreatedBy string
}

// Invite issues an invitation and returns the token, which is the only copy
// that will ever exist. Any outstanding invitation for the same entity is
// revoked first, so generating a new link stops the previous one working.
//
// The caller is responsible for deciding that the entity is unowned and that
// the actor may issue invitations for it; the module has no way to check either.
func (m *Module) Invite(ctx context.Context, p InviteParams) (string, *Invitation, error) {
	entityType := strings.TrimSpace(p.EntityType)
	entityRef := strings.TrimSpace(p.EntityRef)
	email := strings.TrimSpace(p.Email)
	switch {
	case entityType == "":
		return "", nil, errors.New("claim: EntityType is required")
	case entityRef == "":
		return "", nil, errors.New("claim: EntityRef is required")
	case email == "":
		return "", nil, errors.New("claim: Email is required")
	}

	token, hash, err := newToken()
	if err != nil {
		return "", nil, err
	}

	if _, err := m.q.RevokeLiveInvitationsForEntity(ctx, db.RevokeLiveInvitationsForEntityParams{
		EntityType: entityType,
		EntityRef:  entityRef,
	}); err != nil {
		return "", nil, fmt.Errorf("claim: revoke previous invitations: %w", err)
	}

	row, err := m.q.CreateInvitation(ctx, db.CreateInvitationParams{
		EntityType:   entityType,
		EntityRef:    entityRef,
		InvitedEmail: email,
		TokenHash:    hash,
		CreatedBy:    strings.TrimSpace(p.CreatedBy),
		ExpiresAt:    pgTime(time.Now().Add(m.cfg.TTL)),
	})
	if err != nil {
		return "", nil, fmt.Errorf("claim: create invitation: %w", err)
	}
	return token, invitationFrom(row), nil
}

// Lookup resolves a token without redeeming it, so a landing page can name what
// is being claimed before asking the visitor to authenticate.
//
// Showing the entity does leak, to whoever holds the token, that it is a real
// invitation. That is not a meaningful disclosure: the token is 256 bits of
// randomness, so the only way to hold one is to have been sent it.
func (m *Module) Lookup(ctx context.Context, token string) (*Invitation, error) {
	inv, err := m.find(ctx, token)
	if err != nil {
		return nil, err
	}
	return inv, redeemable(inv, time.Now())
}

// Consume redeems a token on behalf of consumedBy, which is the host's string
// identifier for the authenticated claimant. It succeeds at most once per
// invitation; a second attempt returns [ErrConsumed].
//
// Success means the claimant proved possession of the link. Acting on that --
// assigning ownership, changing visibility -- is the host's job.
func (m *Module) Consume(ctx context.Context, token, consumedBy string) (*Invitation, error) {
	consumedBy = strings.TrimSpace(consumedBy)
	if consumedBy == "" {
		return nil, errors.New("claim: consumedBy is required")
	}
	inv, err := m.find(ctx, token)
	if err != nil {
		return nil, err
	}
	// Checked before the update purely to report the specific reason; the
	// update's own WHERE clause is what actually makes redemption single-use,
	// so two simultaneous claims cannot both succeed.
	if err := redeemable(inv, time.Now()); err != nil {
		return nil, err
	}

	row, err := m.q.ConsumeInvitation(ctx, db.ConsumeInvitationParams{
		TokenHash:  hashToken(token),
		ConsumedBy: consumedBy,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Lost a race against another claim, an expiry, or a revocation between
		// the check above and here.
		return nil, ErrConsumed
	}
	if err != nil {
		return nil, fmt.Errorf("claim: consume invitation: %w", err)
	}
	return invitationFrom(row), nil
}

// Pending returns the outstanding invitation for an entity, or [ErrNotFound].
// Hosts use it to show whether someone has already been invited.
func (m *Module) Pending(ctx context.Context, entityType, entityRef string) (*Invitation, error) {
	row, err := m.q.GetLiveInvitationForEntity(ctx, db.GetLiveInvitationForEntityParams{
		EntityType: entityType,
		EntityRef:  entityRef,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("claim: pending invitation: %w", err)
	}
	return invitationFrom(row), nil
}

// Revoke invalidates any outstanding invitation for an entity and reports how
// many it invalidated. Used when a listing is claimed by another route, or when
// an invitation was sent in error.
func (m *Module) Revoke(ctx context.Context, entityType, entityRef string) (int64, error) {
	n, err := m.q.RevokeLiveInvitationsForEntity(ctx, db.RevokeLiveInvitationsForEntityParams{
		EntityType: entityType,
		EntityRef:  entityRef,
	})
	if err != nil {
		return 0, fmt.Errorf("claim: revoke invitations: %w", err)
	}
	return n, nil
}

// PendingForEmail lists live invitations addressed to an email, newest first.
//
// This is the hook for granting ownership when someone creates an account with
// an address that was already invited. Callers MUST confirm the identity
// provider verified the address before acting on the result: matching an
// unverified address would let anyone claim a listing by signing up with
// somebody else's email.
func (m *Module) PendingForEmail(ctx context.Context, email string) ([]*Invitation, error) {
	rows, err := m.q.ListLiveInvitationsForEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		return nil, fmt.Errorf("claim: pending invitations for email: %w", err)
	}
	out := make([]*Invitation, len(rows))
	for i, r := range rows {
		out[i] = invitationFrom(r)
	}
	return out, nil
}

// History returns every invitation ever issued for an entity, newest first.
func (m *Module) History(ctx context.Context, entityType, entityRef string) ([]*Invitation, error) {
	rows, err := m.q.ListInvitationsForEntity(ctx, db.ListInvitationsForEntityParams{
		EntityType: entityType,
		EntityRef:  entityRef,
	})
	if err != nil {
		return nil, fmt.Errorf("claim: invitation history: %w", err)
	}
	out := make([]*Invitation, len(rows))
	for i, r := range rows {
		out[i] = invitationFrom(r)
	}
	return out, nil
}

// find resolves a token to its row. Tokens are located by hash through a unique
// index, so there is no application-level comparison of secrets: a wrong token
// matches no row.
func (m *Module) find(ctx context.Context, token string) (*Invitation, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrNotFound
	}
	row, err := m.q.GetInvitationByTokenHash(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("claim: lookup invitation: %w", err)
	}
	return invitationFrom(row), nil
}

// redeemable maps an invitation's state to the specific reason it cannot be
// used, so callers can report something better than a flat failure.
func redeemable(inv *Invitation, now time.Time) error {
	switch {
	case inv.ConsumedAt != nil:
		return ErrConsumed
	case inv.RevokedAt != nil:
		return ErrRevoked
	case !inv.ExpiresAt.After(now):
		return ErrExpired
	}
	return nil
}

// newToken mints a token and its stored hash.
func newToken() (token string, hash []byte, err error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, fmt.Errorf("claim: generate token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, hashToken(token), nil
}

// hashToken is the one-way mapping from a presented token to what the database
// stores. SHA-256 is right here rather than a password hash: the input is 256
// bits of uniform randomness, so there is nothing to brute-force and no need to
// make verification deliberately slow.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
