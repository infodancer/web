package search_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	embedding "github.com/matthewjhunter/go-embedding"

	"github.com/infodancer/web/search"
	"github.com/infodancer/web/search/store"
)

// stubEmbedder returns deterministic fixed-dimension vectors so the embedding
// index path can be tested without a live model backend.
type stubEmbedder struct{ dim int }

func (s stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, s.dim)
		for j := range v {
			v[j] = float32((len(t) + j) % 7)
		}
		out[i] = v
	}
	return out, nil
}

func (s stubEmbedder) Model() string { return "stub-embed" }
func (s stubEmbedder) Fingerprint() embedding.Fingerprint {
	return embedding.Fingerprint{Model: "stub-embed", Dim: s.dim}
}

// TestIngest_Integration exercises the real write queries and the FTS round-trip
// against a live Postgres. It is skipped unless SEARCH_TEST_DSN points at a
// throwaway database (the schema is created by AutoMigrate and rows are written),
// so it never runs in CI, which has no Postgres service.
func TestIngest_Integration(t *testing.T) {
	dsn := os.Getenv("SEARCH_TEST_DSN")
	if dsn == "" {
		t.Skip("set SEARCH_TEST_DSN to a throwaway Postgres to run the ingestion integration test")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	m, err := search.New(search.Config{DB: pool, IndexerDB: pool, AutoMigrate: true})
	if err != nil {
		t.Fatalf("search.New: %v", err)
	}
	st := store.NewStore(pool)

	const (
		src  = "db"
		name = "itest"
	)
	// Isolate from any prior run: remove everything in this test's source.
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM search_documents WHERE source=$1 AND source_name=$2", src, name)
	})
	if _, err := pool.Exec(ctx, "DELETE FROM search_documents WHERE source=$1 AND source_name=$2", src, name); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}

	doc := search.IngestDocument{
		Source:     src,
		SourceName: name,
		ExternalID: "alpha",
		URL:        "/notes/alpha",
		Body:       "# Alpha\n\nThe quick brown fox jumps over the lazy dog.",
		Tags:       []string{"animals", "speed"},
	}

	// First index: created and FTS-searchable.
	res, err := m.Index(ctx, doc)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if !res.Created || res.Skipped {
		t.Fatalf("first index: want created, got %+v", res)
	}
	if !ftsHasURL(t, st, "brown fox", "/notes/alpha") {
		t.Errorf("indexed document not found via FTS")
	}

	// Re-index identical content: skipped, no write.
	res, err = m.Index(ctx, doc)
	if err != nil {
		t.Fatalf("re-Index: %v", err)
	}
	if !res.Skipped {
		t.Errorf("identical re-index should be skipped, got %+v", res)
	}

	// Change the body: re-indexed and searchable by the new term.
	doc.Body = "# Alpha\n\nA completely different sentence about kangaroos."
	res, err = m.Index(ctx, doc)
	if err != nil {
		t.Fatalf("changed Index: %v", err)
	}
	if res.Skipped {
		t.Errorf("changed body should re-index")
	}
	if !ftsHasURL(t, st, "kangaroos", "/notes/alpha") {
		t.Errorf("changed document not found by new term")
	}
	if ftsHasURL(t, st, "brown fox", "/notes/alpha") {
		t.Errorf("stale term still matches after re-index")
	}

	// Reconcile with the document omitted soft-deletes it.
	rec, err := m.Reconcile(ctx, src, name, nil, search.ReconcileOptions{AllowBulkReap: true})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if rec.Deleted != 1 {
		t.Errorf("reconcile should delete the absent doc, got %+v", rec)
	}
	if ftsHasURL(t, st, "kangaroos", "/notes/alpha") {
		t.Errorf("soft-deleted document still appears in FTS results")
	}

	// Re-index revives it, then Delete removes it.
	if _, err := m.Index(ctx, doc); err != nil {
		t.Fatalf("revive Index: %v", err)
	}
	ok, err := m.Delete(ctx, src, name, "alpha")
	if err != nil || !ok {
		t.Fatalf("Delete: ok=%v err=%v", ok, err)
	}
	if ftsHasURL(t, st, "kangaroos", "/notes/alpha") {
		t.Errorf("deleted document still appears in FTS results")
	}
}

// TestIngest_Embeddings_Integration verifies the lazy embedding bootstrap and
// chunk-write path: indexing with an embedder creates the pgvector chunk table,
// the HNSW index, and one chunk row per section, and records the dimension in
// the index meta. Requires a pgvector-capable Postgres at SEARCH_TEST_DSN.
func TestIngest_Embeddings_Integration(t *testing.T) {
	dsn := os.Getenv("SEARCH_TEST_DSN")
	if dsn == "" {
		t.Skip("set SEARCH_TEST_DSN to a pgvector-capable Postgres to run the embedding integration test")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	const (
		src  = "db"
		name = "itest-embed"
	)
	m, err := search.New(search.Config{
		DB:          pool,
		IndexerDB:   pool,
		Embedder:    stubEmbedder{dim: 8},
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("search.New: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM search_documents WHERE source=$1 AND source_name=$2", src, name)
	})
	if _, err := pool.Exec(ctx, "DELETE FROM search_documents WHERE source=$1 AND source_name=$2", src, name); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}
	// Own the embedding fingerprint: a prior run (or a real embedder) may have
	// bootstrapped a different model/dimension, which would (correctly) make this
	// stub's dim-8 vectors a refused mismatch. Reset so the bootstrap runs fresh.
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS search_chunks"); err != nil {
		t.Fatalf("reset chunks: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE search_index_meta SET embed_model=NULL, embed_dim=NULL WHERE id=1"); err != nil {
		t.Fatalf("reset meta: %v", err)
	}

	doc := search.IngestDocument{
		Source:     src,
		SourceName: name,
		ExternalID: "multi",
		URL:        "/notes/multi",
		Body:       "# Title\n\nIntro prose here.\n\n## Alpha\n\nAlpha body text.\n\n## Beta\n\nBeta body text.",
	}
	if _, err := m.Index(ctx, doc); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// One chunk per section (preamble-under-H1 + Alpha + Beta = 3).
	var chunkCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM search_chunks c
		JOIN search_documents d ON d.id = c.document_id
		WHERE d.source=$1 AND d.source_name=$2`, src, name).Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunkCount != 3 {
		t.Errorf("want 3 chunks, got %d", chunkCount)
	}

	// The index meta records the bootstrapped dimension.
	var dim *int32
	if err := pool.QueryRow(ctx, "SELECT embed_dim FROM search_index_meta WHERE id=1").Scan(&dim); err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if dim == nil || *dim != 8 {
		t.Errorf("embed_dim = %v, want 8", dim)
	}

	// The HNSW index exists.
	var idx int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_indexes WHERE indexname='search_chunks_embedding'").Scan(&idx); err != nil {
		t.Fatalf("index check: %v", err)
	}
	if idx != 1 {
		t.Errorf("expected HNSW index search_chunks_embedding to exist")
	}

	// The vector arm returns the embedded document via real pgvector distance.
	st := store.NewStore(pool)
	hits, err := st.SearchChunksVector(ctx, []float32{1, 1, 1, 1, 1, 1, 1, 1}, "cosine", 10)
	if err != nil {
		t.Fatalf("SearchChunksVector: %v", err)
	}
	var docID int64
	if err := pool.QueryRow(ctx,
		"SELECT id FROM search_documents WHERE source=$1 AND source_name=$2 AND source_key='multi'",
		src, name).Scan(&docID); err != nil {
		t.Fatalf("lookup doc id: %v", err)
	}
	found := false
	for _, h := range hits {
		if h.DocID == docID {
			found = true
		}
	}
	if !found {
		t.Errorf("vector arm did not return the embedded document (got %d hits)", len(hits))
	}

	// Re-indexing identical content must not re-embed or duplicate chunks.
	if _, err := m.Index(ctx, doc); err != nil {
		t.Fatalf("re-Index: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM search_chunks c
		JOIN search_documents d ON d.id = c.document_id
		WHERE d.source=$1 AND d.source_name=$2`, src, name).Scan(&chunkCount); err != nil {
		t.Fatalf("recount chunks: %v", err)
	}
	if chunkCount != 3 {
		t.Errorf("re-index changed chunk count to %d, want 3", chunkCount)
	}
}

func ftsHasURL(t *testing.T, st *store.Store, query, url string) bool {
	t.Helper()
	rows, err := st.SearchDocumentsFTS(context.Background(), store.SearchDocumentsFTSParams{Query: query, Fanout: 50})
	if err != nil {
		t.Fatalf("SearchDocumentsFTS(%q): %v", query, err)
	}
	for _, r := range rows {
		if r.Url == url {
			return true
		}
	}
	return false
}
