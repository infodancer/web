package claim_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/claim"
)

// newModule builds a migrated module against a fresh database, so each test
// starts from an empty table and the partial unique indexes cannot carry state
// between tests.
func newModule(t *testing.T, opts ...func(*claim.Config)) (*claim.Module, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, claim.ContainerDSN())
	require.NoError(t, err)
	defer admin.Close()

	dbName := "claim_" + strings.ToLower(strings.NewReplacer("/", "", "-", "", " ", "_").Replace(t.Name()))
	if len(dbName) > 60 {
		dbName = dbName[:60]
	}
	_, err = admin.Exec(ctx, `DROP DATABASE IF EXISTS `+dbName)
	require.NoError(t, err)
	_, err = admin.Exec(ctx, `CREATE DATABASE `+dbName)
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, strings.Replace(claim.ContainerDSN(), "/claim_test?", "/"+dbName+"?", 1))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	cfg := claim.Config{DB: pool, AutoMigrate: true}
	for _, o := range opts {
		o(&cfg)
	}
	m, err := claim.New(cfg)
	require.NoError(t, err)
	return m, pool
}

func invite(t *testing.T, m *claim.Module, ref, email string) (string, *claim.Invitation) {
	t.Helper()
	tok, inv, err := m.Invite(context.Background(), claim.InviteParams{
		EntityType: "vendor",
		EntityRef:  ref,
		Email:      email,
		CreatedBy:  "admin-handle",
	})
	require.NoError(t, err)
	return tok, inv
}

// TestInviteAndConsume is the happy path: an invitation is issued, resolved by
// its token, and redeemed once.
func TestInviteAndConsume(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()

	tok, inv := invite(t, m, "morine-emporium", "hello@morine.test")
	require.NotEmpty(t, tok)
	require.Equal(t, "vendor", inv.EntityType)
	require.Equal(t, "morine-emporium", inv.EntityRef)
	require.Equal(t, "hello@morine.test", inv.InvitedEmail)
	require.Equal(t, "admin-handle", inv.CreatedBy)
	require.Nil(t, inv.ConsumedAt)
	require.True(t, inv.Live(time.Now()))

	got, err := m.Lookup(ctx, tok)
	require.NoError(t, err)
	require.Equal(t, inv.ID, got.ID)

	claimed, err := m.Consume(ctx, tok, "user-42")
	require.NoError(t, err)
	require.NotNil(t, claimed.ConsumedAt)
	require.Equal(t, "user-42", claimed.ConsumedBy)
	require.False(t, claimed.Live(time.Now()))
}

// TestTokenIsNeverStored is the guarantee the whole show-once design rests on:
// reading every column of the table must not yield anything that works as a
// token. If this fails, a database read hands out listing takeovers.
func TestTokenIsNeverStored(t *testing.T) {
	m, pool := newModule(t)
	ctx := context.Background()

	tok, _ := invite(t, m, "held-forge", "smith@held.test")

	var storedHash []byte
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT token_hash FROM claim_invitations`).Scan(&storedHash))

	wantHash := sha256.Sum256([]byte(tok))
	require.Equal(t, wantHash[:], storedHash, "stored value must be the SHA-256 of the token")
	require.NotEqual(t, []byte(tok), storedHash, "the token itself must never be stored")

	// Nothing anywhere in the row may contain the token as text.
	var row string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT claim_invitations::text FROM claim_invitations`).Scan(&row))
	require.NotContains(t, row, tok, "no column may contain the raw token")
}

// TestConsumeIsSingleUse: a redeemed link is dead. Otherwise a forwarded email
// lets a second person take the listing after the first claimed it.
func TestConsumeIsSingleUse(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()

	tok, _ := invite(t, m, "twice-forge", "twice@held.test")
	_, err := m.Consume(ctx, tok, "first-user")
	require.NoError(t, err)

	_, err = m.Consume(ctx, tok, "second-user")
	require.ErrorIs(t, err, claim.ErrConsumed)

	_, err = m.Lookup(ctx, tok)
	require.ErrorIs(t, err, claim.ErrConsumed)
}

// TestConcurrentConsumeHasOneWinner: single use has to hold under a race, not
// just under sequential calls. The guarantee lives in the UPDATE's WHERE
// clause, so two simultaneous claims cannot both match.
func TestConcurrentConsumeHasOneWinner(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()

	tok, _ := invite(t, m, "race-forge", "race@held.test")

	const racers = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		wins     int
		lastErrs []error
	)
	wg.Add(racers)
	for i := range racers {
		go func() {
			defer wg.Done()
			_, err := m.Consume(ctx, tok, fmt.Sprintf("user-%d", i))
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else {
				lastErrs = append(lastErrs, err)
			}
		}()
	}
	wg.Wait()

	require.Equal(t, 1, wins, "exactly one concurrent claim may succeed, got %d (errors: %v)", wins, lastErrs)
	require.Len(t, lastErrs, racers-1)
}

// TestExpiredInvitationIsRejected: past its TTL the link stops working, and the
// caller can tell expiry apart from a bad token.
func TestExpiredInvitationIsRejected(t *testing.T) {
	m, pool := newModule(t, func(c *claim.Config) { c.TTL = time.Hour })
	ctx := context.Background()

	tok, inv := invite(t, m, "stale-forge", "stale@held.test")
	require.WithinDuration(t, inv.CreatedAt.Add(time.Hour), inv.ExpiresAt, time.Minute,
		"configured TTL must be applied to expires_at")

	// Age the row rather than sleeping, so the test stays fast and does not
	// depend on wall-clock timing. created_at moves too, because the schema
	// requires expires_at to be after it.
	_, err := pool.Exec(ctx, `
UPDATE claim_invitations
SET created_at = NOW() - interval '10 days',
    expires_at = NOW() - interval '1 hour'`)
	require.NoError(t, err)

	_, err = m.Lookup(ctx, tok)
	require.ErrorIs(t, err, claim.ErrExpired)
	_, err = m.Consume(ctx, tok, "late-user")
	require.ErrorIs(t, err, claim.ErrExpired)
}

// TestReissueRevokesPrevious: generating a new link invalidates the old one, so
// a superseded invitation in an old mailbox cannot still be redeemed.
func TestReissueRevokesPrevious(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()

	first, _ := invite(t, m, "reissue-forge", "one@held.test")
	second, _ := invite(t, m, "reissue-forge", "one@held.test")
	require.NotEqual(t, first, second)

	_, err := m.Lookup(ctx, first)
	require.ErrorIs(t, err, claim.ErrRevoked)
	_, err = m.Consume(ctx, first, "user-x")
	require.ErrorIs(t, err, claim.ErrRevoked)

	_, err = m.Consume(ctx, second, "user-x")
	require.NoError(t, err, "the newest invitation must still work")
}

// TestOnlyOneLiveInvitationPerEntity: the partial unique index is the real
// guarantee, not the application's memory to revoke first.
func TestOnlyOneLiveInvitationPerEntity(t *testing.T) {
	m, pool := newModule(t)
	ctx := context.Background()

	invite(t, m, "single-forge", "a@held.test")
	invite(t, m, "single-forge", "b@held.test")

	var live int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT count(*) FROM claim_invitations
WHERE entity_type = 'vendor' AND entity_ref = 'single-forge'
  AND consumed_at IS NULL AND revoked_at IS NULL`).Scan(&live))
	require.Equal(t, 1, live)

	// Inserting a second live row directly must be refused by the index.
	_, err := pool.Exec(ctx, `
INSERT INTO claim_invitations (entity_type, entity_ref, invited_email, token_hash, expires_at)
VALUES ('vendor', 'single-forge', 'c@held.test', '\x00', NOW() + interval '1 day')`)
	require.Error(t, err, "a second live invitation must violate the partial unique index")
}

// TestUnknownTokenIsNotFound: a wrong or empty token is distinguishable from an
// expired or consumed one.
func TestUnknownTokenIsNotFound(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()

	for _, tok := range []string{"", "   ", "not-a-real-token"} {
		_, err := m.Lookup(ctx, tok)
		require.ErrorIs(t, err, claim.ErrNotFound, "token %q", tok)
	}
}

// TestPendingForEmailIsCaseInsensitive: the matcher that will grant ownership on
// account creation has to find an invitation regardless of how the address was
// capitalized when it was recorded.
func TestPendingForEmailIsCaseInsensitive(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()

	invite(t, m, "case-forge", "Mixed.Case@Held.test")

	found, err := m.PendingForEmail(ctx, "mixed.case@held.TEST")
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, "case-forge", found[0].EntityRef)

	require.Empty(t, mustPending(t, m, "someone.else@held.test"))
}

// TestConsumedInvitationLeavesTheEmailMatcher: once claimed, an address is no
// longer waiting, so a later signup with the same address inherits nothing.
func TestConsumedInvitationLeavesTheEmailMatcher(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()

	tok, _ := invite(t, m, "done-forge", "done@held.test")
	require.Len(t, mustPending(t, m, "done@held.test"), 1)

	_, err := m.Consume(ctx, tok, "user-9")
	require.NoError(t, err)

	require.Empty(t, mustPending(t, m, "done@held.test"))
}

// TestPendingAndHistory: Pending reports the outstanding invitation; History
// retains superseded ones, because the audit trail is the point of the table.
func TestPendingAndHistory(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()

	_, err := m.Pending(ctx, "vendor", "hist-forge")
	require.ErrorIs(t, err, claim.ErrNotFound)

	invite(t, m, "hist-forge", "one@held.test")
	invite(t, m, "hist-forge", "two@held.test")

	pending, err := m.Pending(ctx, "vendor", "hist-forge")
	require.NoError(t, err)
	require.Equal(t, "two@held.test", pending.InvitedEmail)

	hist, err := m.History(ctx, "vendor", "hist-forge")
	require.NoError(t, err)
	require.Len(t, hist, 2, "a revoked invitation is retained for audit")
	require.NotNil(t, hist[1].RevokedAt)
}

// TestRevokeStopsAnOutstandingLink: used when a listing gets claimed by another
// route and the mailed link must stop working.
func TestRevokeStopsAnOutstandingLink(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()

	tok, _ := invite(t, m, "revoke-forge", "rev@held.test")
	n, err := m.Revoke(ctx, "vendor", "revoke-forge")
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	_, err = m.Consume(ctx, tok, "user-z")
	require.ErrorIs(t, err, claim.ErrRevoked)

	n, err = m.Revoke(ctx, "vendor", "revoke-forge")
	require.NoError(t, err)
	require.Zero(t, n, "revoking twice is a no-op")
}

// TestInviteValidatesRequiredFields: the module refuses to record an invitation
// that identifies neither a target nor a recipient.
func TestInviteValidatesRequiredFields(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()

	for name, p := range map[string]claim.InviteParams{
		"no type":  {EntityRef: "x", Email: "a@b.test"},
		"no ref":   {EntityType: "vendor", Email: "a@b.test"},
		"no email": {EntityType: "vendor", EntityRef: "x"},
		"blank":    {EntityType: "  ", EntityRef: "  ", Email: "  "},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := m.Invite(ctx, p)
			require.Error(t, err)
		})
	}
}

// TestConsumeRequiresAnIdentity: a claim with no claimant would produce an audit
// row that records nothing, which the schema also refuses.
func TestConsumeRequiresAnIdentity(t *testing.T) {
	m, _ := newModule(t)
	tok, _ := invite(t, m, "anon-forge", "anon@held.test")

	_, err := m.Consume(context.Background(), tok, "  ")
	require.Error(t, err)
}

// TestMigrateIsIdempotent: hosts call Migrate on every boot.
func TestMigrateIsIdempotent(t *testing.T) {
	m, _ := newModule(t)
	require.NoError(t, m.Migrate(context.Background()))
	require.NoError(t, m.Migrate(context.Background()))
}

func mustPending(t *testing.T, m *claim.Module, email string) []*claim.Invitation {
	t.Helper()
	got, err := m.PendingForEmail(context.Background(), email)
	require.NoError(t, err)
	return got
}
