package media

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/media/db"
)

// Store is the Postgres-backed object-metadata store. It wraps a pgx pool and
// maps between [ObjectMeta] and the generated query layer. Construct one with
// [NewStore]; it satisfies the handler's metadata interface.
type Store struct {
	q    *db.Queries
	pool *pgxpool.Pool
}

// NewStore constructs a Store from a pgx connection pool owned by the host.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{q: db.New(pool), pool: pool}
}

// Pool returns the underlying pool. Prefer the typed methods; provided for
// tests and callers that need transactional control.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Put records object metadata. It is idempotent on the content address, so a
// re-upload of identical content does not error or duplicate.
func (s *Store) Put(ctx context.Context, m ObjectMeta) error {
	// Dimensions are bounded by Config.MaxPixels upstream in Process; guard the
	// narrowing to int32 anyway so a hand-built ObjectMeta can't overflow.
	if m.Width < 0 || m.Width > math.MaxInt32 || m.Height < 0 || m.Height > math.MaxInt32 {
		return fmt.Errorf("media: put object: dimensions out of range: %dx%d", m.Width, m.Height)
	}
	err := s.q.PutObject(ctx, db.PutObjectParams{
		Hash:        m.Hash,
		ContentType: m.ContentType,
		Width:       int32(m.Width),
		Height:      int32(m.Height),
		ByteSize:    m.ByteSize,
		Owner:       m.Owner,
		CreatedAt:   m.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("media: put object: %w", err)
	}
	return nil
}

// Lookup returns the metadata for hash. The boolean is false (with a nil error)
// when no object has that hash.
func (s *Store) Lookup(ctx context.Context, hash string) (ObjectMeta, bool, error) {
	row, err := s.q.GetObject(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ObjectMeta{}, false, nil
	}
	if err != nil {
		return ObjectMeta{}, false, fmt.Errorf("media: get object: %w", err)
	}
	return ObjectMeta{
		Hash:        row.Hash,
		ContentType: row.ContentType,
		Width:       int(row.Width),
		Height:      int(row.Height),
		ByteSize:    row.ByteSize,
		Owner:       row.Owner,
		CreatedAt:   row.CreatedAt,
	}, true, nil
}

// Compile-time proof the concrete store satisfies the handler's interface.
var _ objectStore = (*Store)(nil)
