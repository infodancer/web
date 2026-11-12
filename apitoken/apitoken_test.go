package apitoken_test

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/apitoken"
	"github.com/infodancer/web/pgtest"
)

const prefix = "tst_pat_"

var testScopes = []apitoken.Scope{
	{Name: "notes:write", Description: "read and write notes"},
	{Name: "admin:read", Description: "read admin data"},
}

// newModule builds a migrated module against a fresh database from pgtest,
// dropped when the test ends. Skips when WEB_TEST_PG is unset.
func newModule(t *testing.T, opts ...func(*apitoken.Config)) (*apitoken.Module, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.Pool(t)
	cfg := apitoken.Config{DB: pool, AutoMigrate: true, Prefix: prefix, Scopes: testScopes}
	for _, o := range opts {
		o(&cfg)
	}
	m, err := apitoken.New(cfg)
	require.NoError(t, err)
	return m, pool
}

func TestNew_ValidatesConfig(t *testing.T) {
	pool := pgtest.Pool(t)
	ok := apitoken.Config{DB: pool, Prefix: prefix, Scopes: testScopes}
	_, err := apitoken.New(ok)
	require.NoError(t, err)

	for name, mut := range map[string]func(*apitoken.Config){
		"no db":           func(c *apitoken.Config) { c.DB = nil },
		"no prefix":       func(c *apitoken.Config) { c.Prefix = "" },
		"prefix no _":     func(c *apitoken.Config) { c.Prefix = "tst" },
		"prefix upper":    func(c *apitoken.Config) { c.Prefix = "TST_" },
		"prefix too long": func(c *apitoken.Config) { c.Prefix = strings.Repeat("a", 16) + "_" },
		"no scopes":       func(c *apitoken.Config) { c.Scopes = nil },
		"unnamed scope":   func(c *apitoken.Config) { c.Scopes = []apitoken.Scope{{Name: ""}} },
		"duplicate scope": func(c *apitoken.Config) { c.Scopes = []apitoken.Scope{{Name: "a"}, {Name: "a"}} },
	} {
		c := ok
		mut(&c)
		_, err := apitoken.New(c)
		require.Error(t, err, name)
	}
}

func TestMintAndAuthenticate(t *testing.T) {
	m, pool := newModule(t)
	ctx := context.Background()

	plain, tok, err := m.Mint(ctx, "user-1", "  laptop CLI  ", []string{"notes:write", "notes:write"})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(plain, prefix), plain)
	require.Len(t, plain, len(prefix)+43, "32 random bytes, base64url without padding")
	require.Equal(t, plain[len(prefix):len(prefix)+8], tok.DisplayPrefix)
	require.Equal(t, "laptop CLI", tok.Name, "name is trimmed")
	require.Equal(t, []string{"notes:write"}, tok.Scopes, "scopes are deduplicated")
	require.Nil(t, tok.LastUsedAt)

	// Only the hash is stored, and nothing else in the row holds the secret.
	var stored []byte
	var row string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT token_hash, t::text FROM apitoken_tokens t WHERE id = $1`, tok.ID).Scan(&stored, &row))
	sum := sha256.Sum256([]byte(plain))
	require.Equal(t, sum[:], stored)
	require.NotContains(t, row, plain[len(prefix):])

	got, err := m.Authenticate(ctx, plain)
	require.NoError(t, err)
	require.Equal(t, tok.ID, got.ID)
	require.Equal(t, "user-1", got.UserKey)
	require.True(t, got.HasScope("notes:write"))
	require.False(t, got.HasScope("admin:read"))

	list, err := m.List(ctx, "user-1")
	require.NoError(t, err)
	require.NotNil(t, list[0].LastUsedAt, "authenticating records use")

	// Two tokens never collide.
	plain2, _, err := m.Mint(ctx, "user-1", "second", []string{"notes:write"})
	require.NoError(t, err)
	require.NotEqual(t, plain, plain2)
}

func TestMint_Validates(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()
	for name, c := range map[string]struct {
		user, name string
		scopes     []string
	}{
		"no user":       {"", "cli", []string{"notes:write"}},
		"blank name":    {"u", "   ", []string{"notes:write"}},
		"long name":     {"u", strings.Repeat("x", 101), []string{"notes:write"}},
		"no scopes":     {"u", "cli", nil},
		"unknown scope": {"u", "cli", []string{"notes:write", "root"}},
	} {
		_, _, err := m.Mint(ctx, c.user, c.name, c.scopes)
		require.ErrorIs(t, err, apitoken.ErrInvalidRequest, name)
	}
	_, _, err := m.Mint(ctx, "u", strings.Repeat("é", 100), []string{"notes:write"})
	require.NoError(t, err, "the name limit counts characters, not bytes")
}

func TestAuthenticate_Rejects(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()
	plain, _, err := m.Mint(ctx, "u", "cli", []string{"notes:write"})
	require.NoError(t, err)

	other, err := apitoken.New(apitoken.Config{DB: pgtest.Pool(t), AutoMigrate: true, Prefix: "oth_pat_", Scopes: testScopes})
	require.NoError(t, err)
	foreign, _, err := other.Mint(ctx, "u", "cli", []string{"notes:write"})
	require.NoError(t, err)

	for _, bad := range []string{
		"",
		prefix,
		plain[:len(plain)-1],
		plain + "A",
		plain[:len(plain)-1] + "!",
		strings.ToUpper(plain),
		" " + plain,
		foreign,
		prefix + strings.Repeat("A", 43),
	} {
		_, err := m.Authenticate(ctx, bad)
		require.ErrorIs(t, err, apitoken.ErrInvalidToken, "%q", bad)
	}
}

func TestListRevokeDelete(t *testing.T) {
	m, _ := newModule(t)
	ctx := context.Background()

	pa1, a1, err := m.Mint(ctx, "alice", "one", []string{"notes:write"})
	require.NoError(t, err)
	_, a2, err := m.Mint(ctx, "alice", "two", []string{"notes:write", "admin:read"})
	require.NoError(t, err)
	pb, b1, err := m.Mint(ctx, "bob", "bob's", []string{"notes:write"})
	require.NoError(t, err)

	list, err := m.List(ctx, "alice")
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, a2.ID, list[0].ID, "newest first")
	require.Equal(t, []string{"admin:read", "notes:write"}, list[0].Scopes)

	// Alice cannot revoke Bob's token; it reads exactly like no such token.
	require.ErrorIs(t, m.Revoke(ctx, "alice", b1.ID), apitoken.ErrNotFound)
	require.ErrorIs(t, m.Revoke(ctx, "alice", 999999), apitoken.ErrNotFound)
	_, err = m.Authenticate(ctx, pb)
	require.NoError(t, err)

	require.NoError(t, m.Revoke(ctx, "alice", a1.ID))
	_, err = m.Authenticate(ctx, pa1)
	require.ErrorIs(t, err, apitoken.ErrInvalidToken, "a revoked token never authenticates")
	require.ErrorIs(t, m.Revoke(ctx, "alice", a1.ID), apitoken.ErrNotFound, "already revoked")

	list, err = m.List(ctx, "alice")
	require.NoError(t, err)
	require.Len(t, list, 2, "revoked tokens stay listed for audit")
	require.NotNil(t, list[1].RevokedAt)

	n, err := m.DeleteForUser(ctx, "bob")
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	_, err = m.Authenticate(ctx, pb)
	require.ErrorIs(t, err, apitoken.ErrInvalidToken)
	list, err = m.List(ctx, "alice")
	require.NoError(t, err)
	require.Len(t, list, 2, "only bob's went")
}

func TestMigrate_Idempotent(t *testing.T) {
	m, _ := newModule(t)
	require.NoError(t, m.Migrate(context.Background()))
}
