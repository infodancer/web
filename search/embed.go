package search

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	embedding "github.com/matthewjhunter/go-embedding"

	"github.com/infodancer/web/search/internal/extract"
	"github.com/infodancer/web/search/internal/index"
	"github.com/infodancer/web/search/store"
)

// chunkBootstrapLock is a fixed key for the Postgres advisory lock that
// serializes the one-time embedding bootstrap (extension + chunk table + HNSW)
// across processes, so concurrent first writes cannot race the DDL (DESIGN §5.3).
const chunkBootstrapLock int64 = 0x53524348 // "SRCH"

// embedAdapter implements index.Embedder over a go-embedding Embedder. It owns
// every model-specific concern the pipeline deliberately knows nothing about:
// the input budget, how a section is cut to fit it, the mandatory asymmetric
// document-side formatting (DESIGN §5.3), and batching.
type embedAdapter struct {
	emb         embedding.Embedder
	chunkTokens int
	batchSize   int
}

// overlapDivisor and minChunkDivisor set the split's two secondary sizes as
// fractions of the body budget, rather than as absolute byte counts that would
// mean different things at a 500-byte budget and a 6000-byte one.
//
// The overlap repeats the tail of each chunk at the head of the next so an
// argument spanning a boundary is retrievable from either side (DESIGN §5.2);
// an eighth is enough for a sentence or two and small enough not to meaningfully
// inflate the corpus. The minimum absorbs a trailing sliver into its
// predecessor: a chunk of a few words embeds to a vector that matches almost
// nothing useful, and it would otherwise compete for the same slot as the real
// content it was cut from.
const (
	overlapDivisor  = 8
	minChunkDivisor = 4
)

// minBodyBytes is the smallest body budget worth splitting to. Below it the
// header is consuming the budget and the pieces would be too small to retrieve
// anything; see splitOptions.
const minBodyBytes = 64

// SplitSections cuts each section to fit the model's input budget, in document
// order. A section that already fits yields exactly one piece, so the common
// case is unchanged.
//
// Sections arrive as plain text already delimited by the extractor's headings,
// so there is no markdown structure left to read: the split falls to the
// library's paragraph → line → sentence → word preference, which is what
// DESIGN §5.2 asks for.
func (e embedAdapter) SplitSections(title string, sections []extract.Section) []index.Piece {
	model := e.emb.Model()
	out := make([]index.Piece, 0, len(sections))
	for _, s := range sections {
		opts := e.splitOptions(model, title, s.HeadingPath)
		for _, c := range embedding.Split(model, s.Text, opts) {
			out = append(out, index.Piece{
				HeadingPath:   s.HeadingPath,
				Text:          c.Text,
				TokenEstimate: estimateTokens(model, len(c.Text)),
			})
		}
	}
	return out
}

// splitOptions resolves the byte budget one piece's *body* may occupy.
//
// Two subtractions matter and both are easy to miss. The budget is the smaller
// of the model's registered ceiling and the configured token target, because a
// token target the backend will not accept is not a target. And the header
// rendered into every chunk -- task prefix, title, section field -- is charged
// against it: sizing the body at the full budget and then prepending a header
// puts every single request over the line.
func (e embedAdapter) splitOptions(model, title, headingPath string) embedding.SplitOptions {
	budget := embedding.LookupLimits(model).MaxBytes
	if target := embedding.BudgetForTokens(model, e.chunkTokens); target > 0 {
		if budget <= 0 || target < budget {
			budget = target
		}
	}

	var opts embedding.SplitOptions
	if budget <= 0 {
		// Nothing knows a size for this model. Splitting to an invented one
		// would be worse than passing the section through whole.
		return opts
	}

	body := budget - renderOverhead(model, title, headingPath)
	if body < minBodyBytes {
		// The header has eaten the budget: a long title, a deep heading path,
		// or a chunk target set far too low. Splitting anyway would emit
		// hundreds of few-byte chunks, each embedding to a vector that matches
		// nothing and all of them competing for the slot the real content
		// should hold. One oversize chunk the backend clips and reports is the
		// better failure, so pass the section through instead.
		return opts
	}

	opts.MaxBytes = body
	opts.Overlap = body / overlapDivisor
	opts.MinBytes = body / minChunkDivisor
	return opts
}

// renderOverhead measures what document-side formatting adds to every chunk.
//
// It measures rather than assumes, and it measures with a non-empty body: the
// formatter emits a blank-line separator only when there is a body to separate,
// so probing with "" undercounts.
func renderOverhead(model, title, headingPath string) int {
	const probe = "x"
	return len(renderDocument(model, title, headingPath, probe)) - len(probe)
}

// renderDocument applies the mandatory document-side formatting (DESIGN §5.3):
// the title fills the model's title slot where it has one, and the heading path
// rides as a labeled metadata field so it informs the vector.
func renderDocument(model, title, headingPath, text string) string {
	var fields []embedding.Field
	if headingPath != "" {
		fields = append(fields, embedding.Field{Key: "section", Value: headingPath})
	}
	return embedding.FormatRetrievalDocument(model, title, fields, text)
}

// estimateTokens converts a byte count to an approximate token count, for the
// diagnostic token_estimate column.
//
// It asks the library what a thousand tokens costs in bytes and inverts that,
// so the estimate follows the ratio actually observed for this model instead of
// the bytes/4 rule of thumb it replaces -- which is off by roughly half on real
// text, and further on anything dense. The conversion is P10-based and so
// rounds *up* in token terms, which is the harmless direction for a figure
// nothing sizes a request from.
func estimateTokens(model string, bytes int) int {
	const probe = 1000
	perProbe := embedding.BudgetForTokens(model, probe)
	if perProbe <= 0 {
		return 0
	}
	return bytes * probe / perProbe
}

// documentHeaderVersion marks the layout renderDocument produces -- the title
// slot plus a "section" field. Bump it when that layout changes, so a corpus
// embedded under the old one is rebuilt rather than mixed with the new.
const documentHeaderVersion = "doc-header-v1"

// documentRecipe fingerprints everything besides the model and vector dimension
// that determines a stored vector: the task, whether the model resolves to task
// prefixes at all, the chunk target, and the header layout (DESIGN §5.3).
//
// The *configured* token target is hashed, not the byte budget it resolves to.
// BudgetForTokens moves as calibration accumulates, so hashing the resolved
// figure would make the recipe drift during normal operation and clear the
// corpus for no reason.
func documentRecipe(model string, chunkTokens int) string {
	return embedding.RecipeFingerprint(model, embedding.TaskRetrievalDocument,
		embedding.SplitOptions{MaxTokens: chunkTokens}, documentHeaderVersion)
}

// EmbedPieces embeds every piece of one document, batched.
//
// Splitting multiplies the piece count -- one long article becomes dozens --
// and pieces are uniform by construction, which makes them a far better
// batching unit than whole documents.
//
// Any failed piece fails the whole document. A partially embedded document
// reads as complete to the backfill and would keep a permanent hole; see
// index.Indexer.embedDocument.
func (e embedAdapter) EmbedPieces(ctx context.Context, title string, pieces []index.Piece) ([][]float32, error) {
	model := e.emb.Model()
	texts := make([]string, len(pieces))
	for i, p := range pieces {
		texts[i] = renderDocument(model, title, p.HeadingPath, p.Text)
	}

	results, err := embedding.BatchEmbedResults(ctx, e.emb, texts, e.batchSize, nil)
	if err != nil {
		return nil, err
	}
	vectors := make([][]float32, len(results))
	for i, r := range results {
		if r.Err != nil {
			return nil, fmt.Errorf("search: embed chunk %d of %d: %w", i, len(results), r.Err)
		}
		vectors[i] = r.Vector
	}
	return vectors, nil
}

// chunkStore implements index.ChunkStore against the read-write pool. It owns the
// lazy embedding bootstrap: the chunk table, pgvector extension, and HNSW index
// are created on first use with the dimension learned from the first embedding,
// so an FTS-only deployment never needs the extension (DESIGN §3.2/§5.3).
type chunkStore struct {
	pool    *pgxpool.Pool
	model   string
	recipe  string
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

	var storedModel, storedRecipe *string
	var storedDim *int32
	if err := tx.QueryRow(ctx,
		"SELECT embed_model, embed_dim, embed_recipe FROM search_index_meta WHERE id = 1",
	).Scan(&storedModel, &storedDim, &storedRecipe); err != nil {
		return fmt.Errorf("search: read index meta: %w", err)
	}

	current := embedding.Fingerprint{Model: c.model, Dim: dim, Recipe: c.recipe}

	switch {
	case storedDim == nil || *storedDim == 0:
		// First ever embedding run: create everything for this dimension.
		if err := c.createChunkSchema(ctx, tx, dim, opclass); err != nil {
			return err
		}
		if err := c.writeMeta(ctx, tx, current); err != nil {
			return err
		}
		c.log.Info("search: embedding bootstrap complete",
			"model", c.model, "dim", dim, "recipe", c.recipe, "metric", c.metric)

	default:
		stored := embedding.Fingerprint{Dim: int(*storedDim)}
		if storedModel != nil {
			stored.Model = *storedModel
		}
		if storedRecipe != nil {
			stored.Recipe = *storedRecipe
		}

		fp, clear, err := reconcileFingerprint(stored, current, c.reembed)
		if err != nil {
			return err
		}
		if clear {
			if err := c.reembedSchema(ctx, tx, dim, opclass); err != nil {
				return err
			}
			c.log.Warn("search: rebuilding embeddings under a new fingerprint; existing vectors dropped",
				"stored", stored, "current", current)
		}
		if err := c.writeMeta(ctx, tx, fp); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("search: bootstrap commit: %w", err)
	}
	return nil
}

// reconcileFingerprint decides what a stored-versus-current fingerprint means:
// the fingerprint to persist, whether the stored vectors must be dropped first,
// and whether the change needs a human.
//
// Fields only one side knows are adopted rather than rejected, so an existing
// corpus records its recipe on the first run after the upgrade and the *next*
// change is the one caught.
//
// The three mismatches are not the same event, which is why they are not
// handled the same way:
//
//   - A model or dimension change means something moved *underneath* the
//     deployment -- a tag repointed, a model swapped -- rather than in it. The
//     stored vectors are garbage against the new ones, but dropping them
//     automatically would hide the mistake that caused it, so the run refuses
//     unless ReembedOnMismatch says otherwise.
//   - A recipe change follows an edit someone made on purpose: a different chunk
//     target, a different task, a changed header layout. The vectors are still
//     comparable, just no longer consistent with what queries now produce, and
//     mixing the two recipes is worse than either used alone. That is the one
//     case the caller can resolve alone, so it clears and lets the backfill
//     rebuild.
//
// The distinction has to be asked for as the absence of the other two, not the
// presence of one: a model swap usually moves the dimension and the recipe as
// well, so errors.Is(err, ErrRecipeChanged) alone is true in cases where
// clearing would be wrong. RecipeOnly asks it correctly.
func reconcileFingerprint(stored, current embedding.Fingerprint, reembed bool) (embedding.Fingerprint, bool, error) {
	fp, err := embedding.Reconcile(stored, current)
	switch {
	case err == nil:
		return fp, false, nil
	case embedding.RecipeOnly(err):
		return current, true, nil
	case reembed:
		return current, true, nil
	default:
		return embedding.Fingerprint{}, false, fmt.Errorf(
			"search: embedding fingerprint mismatch (%w); set Config.ReembedOnMismatch to drop and recompute all embeddings", err)
	}
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

func (c *chunkStore) writeMeta(ctx context.Context, tx pgx.Tx, fp embedding.Fingerprint) error {
	if _, err := tx.Exec(ctx,
		`UPDATE search_index_meta
		 SET embed_model = $1, embed_dim = $2, embed_recipe = $3, embed_metric = $4, last_index_at = now()
		 WHERE id = 1`,
		fp.Model, fp.Dim, fp.Recipe, c.metric,
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
