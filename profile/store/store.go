// Package store is profile's data layer over the sqlc-generated queries. It
// translates between the component's domain types and the generated pgx
// models, and maps database states (missing rows, unique violations) onto
// the sentinel errors handlers branch on.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/profile/db"
)

// Sentinel errors handlers translate into HTTP responses.
var (
	// ErrNotFound is returned when no profile exists for the given key or
	// handle.
	ErrNotFound = errors.New("profile: not found")
	// ErrHandleTaken is returned when the requested handle is already claimed
	// by another user.
	ErrHandleTaken = errors.New("profile: handle already taken")
	// ErrHandleAlreadySet is returned by ClaimHandle when the profile already
	// has a handle; claimed handles only change via the admin SetHandle path.
	ErrHandleAlreadySet = errors.New("profile: handle already set")
)

// Link is one entry in a profile's bio-link list, stored as ordered JSON.
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Profile is the public persona stored for one host user.
type Profile struct {
	UserKey     string
	Handle      string // empty until claimed
	DisplayName string
	BioMD       string // markdown source; sanitized at render time
	AvatarURL   string // canonical media URL; empty falls back to identicon
	Links       []Link
	IsPublic    bool // opt-out listing flag; false hides the public page
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UpsertParams carries the user-editable fields written by Upsert. Handle and
// avatar have dedicated methods because their write rules differ.
//
// IsPublic is written verbatim on every Upsert: callers must carry the
// current value forward (the module's edit handler always sets it from the
// form). A zero-value UpsertParams therefore UNLISTS the profile -- direct
// Store callers creating rows outside the edit flow should set IsPublic
// explicitly, normally to true to match the column default.
type UpsertParams struct {
	UserKey     string
	DisplayName string
	BioMD       string
	Links       []Link
	IsPublic    bool
}

// Store wraps the generated queries with domain types. Construct with [New].
type Store struct {
	q *db.Queries
}

// New builds a Store over the host-owned pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{q: db.New(pool)}
}

// Get returns the profile for userKey, or [ErrNotFound].
func (s *Store) Get(ctx context.Context, userKey string) (*Profile, error) {
	row, err := s.q.GetProfile(ctx, userKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get profile: %w", err)
	}
	return fromRow(row)
}

// GetByHandle returns the profile claimed under handle (lowercase-normalized
// by the caller), or [ErrNotFound].
func (s *Store) GetByHandle(ctx context.Context, handle string) (*Profile, error) {
	row, err := s.q.GetProfileByHandle(ctx, pgtype.Text{String: handle, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get profile by handle: %w", err)
	}
	return fromRow(row)
}

// Upsert creates or updates the user-editable fields of a profile and returns
// the stored row. Handle and avatar are untouched.
func (s *Store) Upsert(ctx context.Context, p UpsertParams) (*Profile, error) {
	links := p.Links
	if links == nil {
		links = []Link{}
	}
	raw, err := json.Marshal(links)
	if err != nil {
		return nil, fmt.Errorf("marshal links: %w", err)
	}
	row, err := s.q.UpsertProfile(ctx, db.UpsertProfileParams{
		UserKey:     p.UserKey,
		DisplayName: p.DisplayName,
		BioMd:       p.BioMD,
		Links:       raw,
		IsPublic:    p.IsPublic,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert profile: %w", err)
	}
	return fromRow(row)
}

// ClaimHandle sets the handle on a profile that has none. It returns
// [ErrHandleTaken] when another user holds the handle, [ErrHandleAlreadySet]
// when this profile already claimed one, and [ErrNotFound] when no profile
// row exists yet (Upsert creates it).
func (s *Store) ClaimHandle(ctx context.Context, userKey, handle string) error {
	n, err := s.q.ClaimHandle(ctx, db.ClaimHandleParams{
		UserKey: userKey,
		Handle:  pgtype.Text{String: handle, Valid: true},
	})
	if err != nil {
		if isUniqueViolation(err) {
			return ErrHandleTaken
		}
		return fmt.Errorf("claim handle: %w", err)
	}
	if n == 0 {
		// Either the profile is missing or the handle is already set;
		// distinguish for a precise error.
		if _, gerr := s.Get(ctx, userKey); gerr != nil {
			return gerr
		}
		return ErrHandleAlreadySet
	}
	return nil
}

// SetHandle overwrites the handle unconditionally -- the admin-override path.
// Returns [ErrHandleTaken] or [ErrNotFound].
func (s *Store) SetHandle(ctx context.Context, userKey, handle string) error {
	n, err := s.q.SetHandle(ctx, db.SetHandleParams{
		UserKey: userKey,
		Handle:  pgtype.Text{String: handle, Valid: true},
	})
	if err != nil {
		if isUniqueViolation(err) {
			return ErrHandleTaken
		}
		return fmt.Errorf("set handle: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListPublic returns listed profiles (is_public, handle claimed) ordered by
// most recent update -- the "recently active" feel host directories want.
// Pagination is plain limit/offset; hosts with richer listing needs (geo,
// tags) join profile_profiles in their own queries instead.
func (s *Store) ListPublic(ctx context.Context, limit, offset int) ([]*Profile, error) {
	rows, err := s.q.ListPublicProfiles(ctx, db.ListPublicProfilesParams{
		Limit:  clamp32(limit),
		Offset: clamp32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("list public profiles: %w", err)
	}
	out := make([]*Profile, 0, len(rows))
	for _, row := range rows {
		p, err := fromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// clamp32 converts a caller-supplied limit/offset to int32 without overflow.
func clamp32(n int) int32 {
	if n < 0 {
		return 0
	}
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n)
}

// SetAvatarURL records the canonical media URL for the profile's picture.
// An empty url reverts the profile to the identicon fallback. Returns
// [ErrNotFound] when no profile row exists.
func (s *Store) SetAvatarURL(ctx context.Context, userKey, url string) error {
	n, err := s.q.SetAvatarURL(ctx, db.SetAvatarURLParams{UserKey: userKey, AvatarUrl: url})
	if err != nil {
		return fmt.Errorf("set avatar url: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func fromRow(row db.ProfileProfile) (*Profile, error) {
	var links []Link
	if len(row.Links) > 0 {
		if err := json.Unmarshal(row.Links, &links); err != nil {
			return nil, fmt.Errorf("unmarshal links: %w", err)
		}
	}
	return &Profile{
		UserKey:     row.UserKey,
		Handle:      row.Handle.String,
		DisplayName: row.DisplayName,
		BioMD:       row.BioMd,
		AvatarURL:   row.AvatarUrl,
		Links:       links,
		IsPublic:    row.IsPublic,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
