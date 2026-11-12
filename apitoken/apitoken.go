// Package apitoken gives a host personal access tokens: a user mints named,
// scoped tokens on an account page, and a script or CLI sends one as
// "Authorization: Bearer <token>" to call the host's API as that user.
//
// The module stores only the SHA-256 of each token, keyed on the host's user
// key. [Module.Dispatch] routes token requests to a separate handler that
// holds only the routes the host has made token-capable, so a token can never
// reach the host's ordinary form handlers. See README.md for the mount
// contract.
package apitoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/infodancer/web/apitoken/db"
)

//go:embed db/migrations/*.sql
var migrationsFS embed.FS

const migrationsTableName = "apitoken_goose_db_version"

const (
	randomBytes   = 32 // 256 bits
	encodedLen    = 43 // base64url, no padding, of 32 bytes
	displayLen    = 8
	maxNameLength = 100
)

var (
	// ErrInvalidToken: a presented token is malformed, unknown or revoked.
	// One error for all three, so a caller learns nothing about which.
	ErrInvalidToken = errors.New("apitoken: invalid token")
	// ErrInvalidRequest: a mint request with a bad name or scope list.
	ErrInvalidRequest = errors.New("apitoken: invalid token request")
	// ErrNotFound: no such live token for this user.
	ErrNotFound = errors.New("apitoken: token not found")
	// ErrNoUser is what a Config.Authenticated hook wraps when the token's
	// user no longer exists.
	ErrNoUser = errors.New("apitoken: token's user does not exist")
)

// Token is a stored token: everything but the secret, which exists only in
// its owner's hands.
type Token struct {
	ID            int64
	UserKey       string
	Name          string
	DisplayPrefix string
	Scopes        []string
	CreatedAt     time.Time
	LastUsedAt    *time.Time
	RevokedAt     *time.Time
}

// HasScope reports whether the token was granted scope.
func (t *Token) HasScope(scope string) bool { return slices.Contains(t.Scopes, scope) }

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
// tracks applied versions in apitoken_goose_db_version.
func (m *Module) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationsFS, "db/migrations")
	if err != nil {
		return fmt.Errorf("apitoken: migrate: %w", err)
	}
	sqldb := stdlib.OpenDBFromPool(m.cfg.DB)
	defer func() { _ = sqldb.Close() }()
	prov, err := goose.NewProvider(goose.DialectPostgres, sqldb, sub,
		goose.WithTableName(migrationsTableName),
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		return fmt.Errorf("apitoken: migrate: %w", err)
	}
	if _, err := prov.Up(ctx); err != nil {
		return fmt.Errorf("apitoken: migrate: %w", err)
	}
	return nil
}

func hashToken(plain string) []byte {
	sum := sha256.Sum256([]byte(plain))
	return sum[:]
}

func (m *Module) knownScope(name string) bool {
	for _, s := range m.cfg.Scopes {
		if s.Name == name {
			return true
		}
	}
	return false
}

// Mint creates a token for userKey and returns it in plain text along with
// the stored record. The plain text cannot be recovered afterwards. name is
// trimmed and must be 1-100 characters; scopes must be non-empty and every one
// configured -- an unknown scope is refused, never dropped.
func (m *Module) Mint(ctx context.Context, userKey, name string, scopes []string) (string, *Token, error) {
	if userKey == "" {
		return "", nil, fmt.Errorf("%w: no user", ErrInvalidRequest)
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > maxNameLength {
		return "", nil, fmt.Errorf("%w: name must be 1-%d characters", ErrInvalidRequest, maxNameLength)
	}
	if len(scopes) == 0 {
		return "", nil, fmt.Errorf("%w: no scopes", ErrInvalidRequest)
	}
	for _, s := range scopes {
		if !m.knownScope(s) {
			return "", nil, fmt.Errorf("%w: unknown scope %q", ErrInvalidRequest, s)
		}
	}
	scopes = slices.Compact(slices.Sorted(slices.Values(scopes)))

	b := make([]byte, randomBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("apitoken: entropy: %w", err)
	}
	random := base64.RawURLEncoding.EncodeToString(b)
	plain := m.cfg.Prefix + random

	row, err := m.q.CreateToken(ctx, db.CreateTokenParams{
		UserKey:       userKey,
		Name:          name,
		TokenHash:     hashToken(plain),
		DisplayPrefix: random[:displayLen],
		Scopes:        scopes,
	})
	if err != nil {
		return "", nil, fmt.Errorf("apitoken: mint: %w", err)
	}
	return plain, tokenFrom(row), nil
}

// Authenticate returns the live token matching plain and records its use.
// A malformed, unknown or revoked token is [ErrInvalidToken].
func (m *Module) Authenticate(ctx context.Context, plain string) (*Token, error) {
	random, ok := strings.CutPrefix(plain, m.cfg.Prefix)
	if !ok || len(random) != encodedLen {
		return nil, ErrInvalidToken
	}
	if _, err := base64.RawURLEncoding.Strict().DecodeString(random); err != nil {
		return nil, ErrInvalidToken
	}
	row, err := m.q.GetActiveTokenByHash(ctx, hashToken(plain))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, fmt.Errorf("apitoken: authenticate: %w", err)
	}
	if err := m.q.TouchToken(ctx, row.ID); err != nil {
		return nil, fmt.Errorf("apitoken: authenticate: record use: %w", err)
	}
	return tokenFrom(row), nil
}

// List returns every token userKey has minted, revoked ones included, newest
// first.
func (m *Module) List(ctx context.Context, userKey string) ([]*Token, error) {
	rows, err := m.q.ListTokensForUser(ctx, userKey)
	if err != nil {
		return nil, fmt.Errorf("apitoken: list: %w", err)
	}
	out := make([]*Token, len(rows))
	for i, r := range rows {
		out[i] = tokenFrom(r)
	}
	return out, nil
}

// Revoke revokes one of userKey's live tokens. Another user's token, an
// unknown id and an already-revoked token are all [ErrNotFound].
func (m *Module) Revoke(ctx context.Context, userKey string, id int64) error {
	n, err := m.q.RevokeToken(ctx, db.RevokeTokenParams{ID: id, UserKey: userKey})
	if err != nil {
		return fmt.Errorf("apitoken: revoke: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteForUser removes every token userKey holds, revoked ones included.
// Call it when the host deletes the account; returns how many rows went.
func (m *Module) DeleteForUser(ctx context.Context, userKey string) (int64, error) {
	n, err := m.q.DeleteTokensForUser(ctx, userKey)
	if err != nil {
		return 0, fmt.Errorf("apitoken: delete for user: %w", err)
	}
	return n, nil
}

func tokenFrom(r db.ApitokenToken) *Token {
	return &Token{
		ID:            r.ID,
		UserKey:       r.UserKey,
		Name:          r.Name,
		DisplayPrefix: r.DisplayPrefix,
		Scopes:        r.Scopes,
		CreatedAt:     r.CreatedAt.Time,
		LastUsedAt:    timePtr(r.LastUsedAt),
		RevokedAt:     timePtr(r.RevokedAt),
	}
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
