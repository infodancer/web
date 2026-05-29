package search

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	embedding "github.com/matthewjhunter/go-embedding"

	"github.com/infodancer/web/search/internal/index"
	"github.com/infodancer/web/search/store"
)

// chunkBootstrapLock is a fixed key for the Postgres advisory lock that
// serializes the one-time embedding bootstrap (extension + chunk table + HNSW)
// across processes, so concurrent first writes cannot race the DDL (DESIGN §5.3).
const chunkBootstrapLock int64 = 0x53524348 // "SRCH"

// embedAdapter implements index.Embedder over a go-embedding Embedder, applying
// the mandatory asymmetric document-side formatting (DESIGN §5.3): the heading
// path is carried as a labeled metadata field so it informs the vector.
type embedAdapter struct {
	emb embedding.Embedder
}

func (e embedAdapter) EmbedDocument(ctx context.Context, title, headingPath, text string) ([]float32, error) {
	var fields []embedding.Field
	if headingPath != "" {
		fields = append(fields, embedding.Field{Key: "section", Value: headingPath})
	}
	formatted := embedding.FormatRetrievalDocument(e.emb.Model(), title, fields, text)
	vecs, err := e.emb.Embed(ctx, []string{formatted})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("search: embedder returned %d vectors for one input", len(vecs))
	}
	return vecs[0], nil
}

// chunkStore implements index.ChunkStore against the read-write pool. It owns the
// lazy embedding bootstrap: the chunk table, pgvector extension, and HNSW index
// are created on first use with the dimension learned from the first embedding,
// so an FTS-only deployment never needs the extension (DESIGN §3.2/§5.3).
type chunkStore struct {
	pool    *pgxpool.Pool
	model   string
	metric  string
	reembed bool
	log     *slog.Logger

	once    sync.Once
	bootErr error
}

// ReplaceChunks atomically replaces a document's chunk set. With no chunks it
// just clears any prior set (the content lost all sections). Vectors are sent as
// pgvector text literals cast to ::vector, so no per-connection type registration
// is needed.
func (c *chunkStore) ReplaceChunks(ctx context.Context, documentID int64, chunks []index.Chunk) error {
	if len(chunks) == 0 {
		_, err := c.pool.Exec(ctx, "DELETE FROM search_chunks WHERE document_id = $1", documentID)
		if err != nil {
			return fmt.Errorf("search: clear chunks: %w", err)
		}
		return nil
	}

	dim := len(chunks[0].Embedding)
	if err := c.ensureBootstrap(ctx, dim); err != nil {
		return err
	}

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("search: chunks tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "DELETE FROM search_chunks WHERE document_id = $1", documentID); err != nil {
		return fmt.Errorf("search: replace chunks: delete: %w", err)
	}
	for _, ch := range chunks {
		if len(ch.Embedding) != dim {
			return fmt.Errorf("search: ragged embedding dimension %d != %d", len(ch.Embedding), dim)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO search_chunks (document_id, ord, heading_path, text, token_estimate, embedding)
			 VALUES ($1, $2, $3, $4, $5, $6::vector)`,
			documentID, ch.Ord, ch.HeadingPath, ch.Text, ch.TokenEstimate, store.VectorLiteral(ch.Embedding),
		); err != nil {
			return fmt.Errorf("search: insert chunk %d: %w", ch.Ord, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("search: commit chunks: %w", err)
	}
	return nil
}

// ensureBootstrap runs the one-time schema bootstrap, guarded by sync.Once within
// the process and a transaction-scoped advisory lock across processes.
func (c *chunkStore) ensureBootstrap(ctx context.Context, dim int) error {
	c.once.Do(func() { c.bootErr = c.bootstrap(ctx, dim) })
	return c.bootErr
}

func (c *chunkStore) bootstrap(ctx context.Context, dim int) error {
	opclass, ok := hnswOpclass[c.metric]
	if !ok {
		return fmt.Errorf("search: unknown embed metric %q", c.metric)
	}

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("search: bootstrap tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialize across processes for the duration of this transaction.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", chunkBootstrapLock); err != nil {
		return fmt.Errorf("search: bootstrap lock: %w", err)
	}

	var storedModel *string
	var storedDim *int32
	if err := tx.QueryRow(ctx,
		"SELECT embed_model, embed_dim FROM search_index_meta WHERE id = 1",
	).Scan(&storedModel, &storedDim); err != nil {
		return fmt.Errorf("search: read index meta: %w", err)
	}

	switch {
	case storedDim == nil || *storedDim == 0:
		// First ever embedding run: create everything for this dimension.
		if err := c.createChunkSchema(ctx, tx, dim, opclass); err != nil {
			return err
		}
		if err := c.writeMeta(ctx, tx, dim); err != nil {
			return err
		}
		c.log.Info("search: embedding bootstrap complete", "model", c.model, "dim", dim, "metric", c.metric)

	default:
		// Already bootstrapped: refuse a model/dimension change unless re-embedding.
		stored := embedding.Fingerprint{Dim: int(*storedDim)}
		if storedModel != nil {
			stored.Model = *storedModel
		}
		current := embedding.Fingerprint{Model: c.model, Dim: dim}
		if ferr := embedding.CheckFingerprint(stored, current); ferr != nil {
			if !c.reembed {
				return fmt.Errorf("search: embedding fingerprint mismatch (%w); set Config.ReembedOnMismatch to drop and recompute all embeddings", ferr)
			}
			if err := c.reembedSchema(ctx, tx, dim, opclass); err != nil {
				return err
			}
			if err := c.writeMeta(ctx, tx, dim); err != nil {
				return err
			}
			c.log.Warn("search: re-embedding under new fingerprint; existing embeddings dropped", "model", c.model, "dim", dim)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("search: bootstrap commit: %w", err)
	}
	return nil
}

func (c *chunkStore) createChunkSchema(ctx context.Context, tx pgx.Tx, dim int, opclass string) error {
	stmts := []string{
		"CREATE EXTENSION IF NOT EXISTS vector",
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS search_chunks (
			id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			document_id    bigint NOT NULL REFERENCES search_documents(id) ON DELETE CASCADE,
			ord            int    NOT NULL,
			heading_path   text   NOT NULL DEFAULT '',
			text           text   NOT NULL,
			token_estimate int    NOT NULL DEFAULT 0,
			embedding      vector(%d) NOT NULL,
			UNIQUE (document_id, ord)
		)`, dim),
		"CREATE INDEX IF NOT EXISTS search_chunks_document ON search_chunks (document_id)",
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS search_chunks_embedding ON search_chunks USING hnsw (embedding %s)", opclass),
	}
	for _, s := range stmts {
		if _, err := tx.Exec(ctx, s); err != nil {
			return fmt.Errorf("search: create chunk schema: %w", err)
		}
	}
	return nil
}

func (c *chunkStore) reembedSchema(ctx context.Context, tx pgx.Tx, dim int, opclass string) error {
	stmts := []string{
		"DELETE FROM search_chunks",
		"DROP INDEX IF EXISTS search_chunks_embedding",
		fmt.Sprintf("ALTER TABLE search_chunks ALTER COLUMN embedding TYPE vector(%d)", dim),
		fmt.Sprintf("CREATE INDEX search_chunks_embedding ON search_chunks USING hnsw (embedding %s)", opclass),
	}
	for _, s := range stmts {
		if _, err := tx.Exec(ctx, s); err != nil {
			return fmt.Errorf("search: re-embed schema: %w", err)
		}
	}
	return nil
}

func (c *chunkStore) writeMeta(ctx context.Context, tx pgx.Tx, dim int) error {
	if _, err := tx.Exec(ctx,
		`UPDATE search_index_meta
		 SET embed_model = $1, embed_dim = $2, embed_metric = $3, last_index_at = now()
		 WHERE id = 1`,
		c.model, dim, c.metric,
	); err != nil {
		return fmt.Errorf("search: write index meta: %w", err)
	}
	return nil
}

// hnswOpclass maps a distance metric to its pgvector HNSW operator class.
var hnswOpclass = map[string]string{
	"cosine": "vector_cosine_ops",
	"ip":     "vector_ip_ops",
	"l2":     "vector_l2_ops",
}
