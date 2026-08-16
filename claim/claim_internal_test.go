package claim

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/claim/db"
)

// These tests sit inside the package on purpose. The exported Consume performs
// an application-level state check before it issues the UPDATE, which produces a
// specific error for the caller -- and, in a test, hides whether the SQL itself
// is safe. A mutation run proved that: deleting "AND consumed_at IS NULL" from
// ConsumeInvitation left the whole external suite green, because the Go check
// caught every case first.
//
// Single use is a security property, so the guarantee has to live in the
// database where concurrency cannot get around it, and it has to be tested
// there directly rather than through the layer that masks it.

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	defer admin.Close()

	// Lowercased because Postgres folds unquoted identifiers, so a mixed-case
	// CREATE DATABASE cannot be connected to by the name as written.
	name := strings.ToLower("claim_internal_" + t.Name())
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+name); err != nil {
		t.Fatalf("drop database: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatalf("create database: %v", err)
	}

	dsn := replaceDB(testDSN, name)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func replaceDB(dsn, name string) string {
	return strings.Replace(dsn, "/claim_test?", "/"+name+"?", 1)
}

// TestConsumeQueryIsSingleUseAtTheDatabase drives the generated query directly,
// with no Go-side state check in the way. Two consumes of the same token must
// produce one row and one pgx.ErrNoRows.
func TestConsumeQueryIsSingleUseAtTheDatabase(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	m, err := New(Config{DB: pool, AutoMigrate: true})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	token, _, err := m.Invite(ctx, InviteParams{
		EntityType: "vendor", EntityRef: "sql-forge", Email: "sql@held.test",
	})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	hash := hashToken(token)

	if _, err := m.q.ConsumeInvitation(ctx, db.ConsumeInvitationParams{
		TokenHash: hash, ConsumedBy: "first",
	}); err != nil {
		t.Fatalf("first consume must succeed: %v", err)
	}

	_, err = m.q.ConsumeInvitation(ctx, db.ConsumeInvitationParams{
		TokenHash: hash, ConsumedBy: "second",
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second consume must match no row, got err=%v", err)
	}

	// The winner must still be the first claimant: a second UPDATE that matched
	// would have overwritten consumed_by.
	var consumedBy string
	if err := pool.QueryRow(ctx,
		`SELECT consumed_by FROM claim_invitations WHERE token_hash = $1`, hash).
		Scan(&consumedBy); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if consumedBy != "first" {
		t.Fatalf("consumed_by = %q, want %q -- a later claim overwrote the first", consumedBy, "first")
	}
}

// TestConsumeQueryRejectsExpiredAndRevoked: the same UPDATE also has to refuse
// an expired or revoked invitation without help from the Go layer.
func TestConsumeQueryRejectsExpiredAndRevoked(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	m, err := New(Config{DB: pool, AutoMigrate: true})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	expiredTok, _, err := m.Invite(ctx, InviteParams{
		EntityType: "vendor", EntityRef: "expired-forge", Email: "e@held.test",
	})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE claim_invitations
SET created_at = NOW() - interval '10 days', expires_at = NOW() - interval '1 hour'
WHERE entity_ref = 'expired-forge'`); err != nil {
		t.Fatalf("age row: %v", err)
	}
	if _, err := m.q.ConsumeInvitation(ctx, db.ConsumeInvitationParams{
		TokenHash: hashToken(expiredTok), ConsumedBy: "late",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expired invitation must match no row, got err=%v", err)
	}

	revokedTok, _, err := m.Invite(ctx, InviteParams{
		EntityType: "vendor", EntityRef: "revoked-forge", Email: "r@held.test",
	})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := m.Revoke(ctx, "vendor", "revoked-forge"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := m.q.ConsumeInvitation(ctx, db.ConsumeInvitationParams{
		TokenHash: hashToken(revokedTok), ConsumedBy: "nope",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("revoked invitation must match no row, got err=%v", err)
	}
}

// TestTokenEntropy: tokens must not repeat. A collision would let one
// invitation redeem another, and a low-entropy or misseeded generator is the
// kind of failure that stays invisible until it matters.
func TestTokenEntropy(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		tok, hash, err := newToken()
		if err != nil {
			t.Fatalf("newToken: %v", err)
		}
		if len(hash) != 32 {
			t.Fatalf("hash length = %d, want 32", len(hash))
		}
		// 32 raw bytes in base64url with no padding.
		if len(tok) != 43 {
			t.Fatalf("token length = %d, want 43 (32 bytes base64url)", len(tok))
		}
		if _, dup := seen[tok]; dup {
			t.Fatalf("duplicate token generated: %q", tok)
		}
		seen[tok] = struct{}{}
	}
}

// TestLiveReportsRedeemability pins the state machine the host relies on to
// decide whether to show an outstanding invitation.
func TestLiveReportsRedeemability(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)

	for name, tc := range map[string]struct {
		inv  Invitation
		want bool
	}{
		"pending":  {Invitation{ExpiresAt: future}, true},
		"expired":  {Invitation{ExpiresAt: past}, false},
		"consumed": {Invitation{ExpiresAt: future, ConsumedAt: &past}, false},
		"revoked":  {Invitation{ExpiresAt: future, RevokedAt: &past}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.inv.Live(now); got != tc.want {
				t.Fatalf("Live() = %v, want %v", got, tc.want)
			}
		})
	}
}
