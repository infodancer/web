package media

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/media/db"
)

// PostgresBlobStore is the default [BlobStore]: bytes in Postgres (`bytea`),
// consistent with the suite's one-database posture. It is self-contained — it
// carries each blob's content type — so the serve path does not consult the
// metadata table.
type PostgresBlobStore struct {
	q *db.Queries
}

// NewPostgresBlobStore constructs a Postgres-backed blob store over the host's
// pool.
func NewPostgresBlobStore(pool *pgxpool.Pool) *PostgresBlobStore {
	return &PostgresBlobStore{q: db.New(pool)}
}

// Put stores data under hash; storing an existing hash is a no-op.
func (p *PostgresBlobStore) Put(ctx context.Context, hash, contentType string, data []byte) error {
	err := p.q.PutBlob(ctx, db.PutBlobParams{
		Hash:        hash,
		ContentType: contentType,
		Bytes:       data,
		ByteSize:    int64(len(data)),
	})
	if err != nil {
		return fmt.Errorf("media: put blob: %w", err)
	}
	return nil
}

// Get returns the bytes and content type for hash, or [ErrBlobNotFound].
func (p *PostgresBlobStore) Get(ctx context.Context, hash string) ([]byte, string, error) {
	row, err := p.q.GetBlob(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrBlobNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("media: get blob: %w", err)
	}
	return row.Bytes, row.ContentType, nil
}

// Has reports whether a blob with hash is stored.
func (p *PostgresBlobStore) Has(ctx context.Context, hash string) (bool, error) {
	ok, err := p.q.HasBlob(ctx, hash)
	if err != nil {
		return false, fmt.Errorf("media: has blob: %w", err)
	}
	return ok, nil
}

// Compile-time proof the concrete blob store satisfies the seam.
var _ BlobStore = (*PostgresBlobStore)(nil)
