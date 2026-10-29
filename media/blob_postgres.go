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
// metadata table. Bytes are keyed by (category, hash), so one store serves
// every category.
type PostgresBlobStore struct {
	q *db.Queries
}

// NewPostgresBlobStore constructs a Postgres-backed blob store over the host's
// pool.
func NewPostgresBlobStore(pool *pgxpool.Pool) *PostgresBlobStore {
	return &PostgresBlobStore{q: db.New(pool)}
}

// Put stores data under (category, hash); storing an existing key is a no-op.
func (p *PostgresBlobStore) Put(ctx context.Context, category, hash, contentType string, data []byte) error {
	err := p.q.PutBlob(ctx, db.PutBlobParams{
		Category:    category,
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

// Get returns the bytes and content type for (category, hash), or [ErrBlobNotFound].
func (p *PostgresBlobStore) Get(ctx context.Context, category, hash string) ([]byte, string, error) {
	row, err := p.q.GetBlob(ctx, db.GetBlobParams{Category: category, Hash: hash})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrBlobNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("media: get blob: %w", err)
	}
	return row.Bytes, row.ContentType, nil
}

// Has reports whether a blob with (category, hash) is stored.
func (p *PostgresBlobStore) Has(ctx context.Context, category, hash string) (bool, error) {
	ok, err := p.q.HasBlob(ctx, db.HasBlobParams{Category: category, Hash: hash})
	if err != nil {
		return false, fmt.Errorf("media: has blob: %w", err)
	}
	return ok, nil
}

// Delete removes the blob for (category, hash); a missing key is not an error.
func (p *PostgresBlobStore) Delete(ctx context.Context, category, hash string) error {
	if err := p.q.DeleteBlob(ctx, db.DeleteBlobParams{Category: category, Hash: hash}); err != nil {
		return fmt.Errorf("media: delete blob: %w", err)
	}
	return nil
}

// Compile-time proof the concrete blob store satisfies the seam.
var _ BlobStore = (*PostgresBlobStore)(nil)
