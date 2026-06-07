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

// Put records object metadata. It is idempotent on the (category, hash) key, so
// a re-upload of identical content does not error or duplicate.
func (s *Store) Put(ctx context.Context, m ObjectMeta) error {
	// Dimensions are bounded by Config.MaxPixels upstream in Process; guard the
	// narrowing to int32 anyway so a hand-built ObjectMeta can't overflow.
	if m.Width < 0 || m.Width > math.MaxInt32 || m.Height < 0 || m.Height > math.MaxInt32 {
		return fmt.Errorf("media: put object: dimensions out of range: %dx%d", m.Width, m.Height)
	}
	err := s.q.PutObject(ctx, db.PutObjectParams{
		Category:    m.Category,
		Hash:        m.Hash,
		ContentType: m.ContentType,
		Width:       int32(m.Width),
		Height:      int32(m.Height),
		ByteSize:    m.ByteSize,
		Subject:     m.Subject,
		CreatedAt:   m.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("media: put object: %w", err)
	}
	return nil
}

// Lookup returns the metadata for (category, hash). The boolean is false (with
// a nil error) when no object has that key.
func (s *Store) Lookup(ctx context.Context, category, hash string) (ObjectMeta, bool, error) {
	row, err := s.q.GetObject(ctx, db.GetObjectParams{Category: category, Hash: hash})
	if errors.Is(err, pgx.ErrNoRows) {
		return ObjectMeta{}, false, nil
	}
	if err != nil {
		return ObjectMeta{}, false, fmt.Errorf("media: get object: %w", err)
	}
	return metaFromRow(row), true, nil
}

// ListBySubject returns every image tied to the owning record subject within
// category, newest first — e.g. all images for one campaign.
func (s *Store) ListBySubject(ctx context.Context, category, subject string) ([]ObjectMeta, error) {
	rows, err := s.q.ListObjectsBySubject(ctx, db.ListObjectsBySubjectParams{Category: category, Subject: subject})
	if err != nil {
		return nil, fmt.Errorf("media: list by subject: %w", err)
	}
	out := make([]ObjectMeta, 0, len(rows))
	for _, row := range rows {
		out = append(out, metaFromRow(row))
	}
	return out, nil
}

// PutVariant records the metadata for one derived size (thumbnail). It is
// idempotent on the (category, parent_hash, name) key, so regenerating a variant
// does not error or duplicate. The variant's bytes are stored separately via the
// [BlobStore].
func (s *Store) PutVariant(ctx context.Context, v Variant) error {
	if v.Width < 0 || v.Width > math.MaxInt32 || v.Height < 0 || v.Height > math.MaxInt32 {
		return fmt.Errorf("media: put variant: dimensions out of range: %dx%d", v.Width, v.Height)
	}
	err := s.q.PutVariant(ctx, db.PutVariantParams{
		Category:    v.Category,
		ParentHash:  v.ParentHash,
		Name:        v.Name,
		Hash:        v.Hash,
		ContentType: v.ContentType,
		Width:       int32(v.Width),
		Height:      int32(v.Height),
		ByteSize:    v.ByteSize,
		CreatedAt:   v.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("media: put variant: %w", err)
	}
	return nil
}

// ListVariants returns every derived size of one original within category,
// ordered by spec name.
func (s *Store) ListVariants(ctx context.Context, category, parentHash string) ([]Variant, error) {
	rows, err := s.q.ListVariants(ctx, db.ListVariantsParams{Category: category, ParentHash: parentHash})
	if err != nil {
		return nil, fmt.Errorf("media: list variants: %w", err)
	}
	out := make([]Variant, 0, len(rows))
	for _, row := range rows {
		out = append(out, variantFromRow(row))
	}
	return out, nil
}

// ListObjectsAfter returns a page of objects in category, keyset-paginated by
// hash. Pass "" as cursor to start; pass the last hash of the previous page to
// continue. limit must be positive. Used by [BackfillThumbnails] to walk a
// category.
func (s *Store) ListObjectsAfter(ctx context.Context, category, cursor string, limit int) ([]ObjectMeta, error) {
	if limit <= 0 || limit > math.MaxInt32 {
		return nil, fmt.Errorf("media: list objects: invalid limit %d", limit)
	}
	rows, err := s.q.ListObjectsAfter(ctx, db.ListObjectsAfterParams{Category: category, Hash: cursor, Limit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("media: list objects: %w", err)
	}
	out := make([]ObjectMeta, 0, len(rows))
	for _, row := range rows {
		out = append(out, metaFromRow(row))
	}
	return out, nil
}

func metaFromRow(row db.MediaObjects) ObjectMeta {
	return ObjectMeta{
		Category:    row.Category,
		Hash:        row.Hash,
		ContentType: row.ContentType,
		Width:       int(row.Width),
		Height:      int(row.Height),
		ByteSize:    row.ByteSize,
		Subject:     row.Subject,
		CreatedAt:   row.CreatedAt,
	}
}

func variantFromRow(row db.MediaVariants) Variant {
	return Variant{
		Category:    row.Category,
		ParentHash:  row.ParentHash,
		Name:        row.Name,
		Hash:        row.Hash,
		ContentType: row.ContentType,
		Width:       int(row.Width),
		Height:      int(row.Height),
		ByteSize:    row.ByteSize,
		CreatedAt:   row.CreatedAt,
	}
}

// Compile-time proof the concrete store satisfies the handler's interface.
var _ objectStore = (*Store)(nil)
