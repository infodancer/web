package search

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/infodancer/web/search/internal/index"
	"github.com/infodancer/web/search/store"
)

// Ingestion input types. search is consumed as a library: a host application
// calls these methods in-process when a document is created, edited, or removed
// (DESIGN §4–§5). There is no HTTP ingest surface.

// BodyKind names the format of a document body. The zero value indexes as
// Markdown.
type BodyKind = index.BodyKind

// Body format constants.
const (
	BodyMarkdown = index.BodyMarkdown
	BodyHTML     = index.BodyHTML
	BodyText     = index.BodyText
)

// IngestDocument is one unit of content handed to the indexer. Populate Source,
// ExternalID, URL, and Body at minimum; Title falls back to the body's first H1.
type IngestDocument = index.Document

// IndexResult reports the outcome of indexing one document. Skipped is true when
// the content was unchanged and no write occurred.
type IndexResult = index.Result

// ReconcileOptions tunes a Reconcile run.
type ReconcileOptions = index.ReconcileOptions

// ReconcileResult summarizes a Reconcile run.
type ReconcileResult = index.ReconcileResult

// ErrBulkReap is returned by Reconcile when a single run would soft-delete more
// than the guarded fraction of a source's live documents (DESIGN §5.4).
var ErrBulkReap = index.ErrBulkReap

// ErrIndexingDisabled is returned by the ingestion methods when the module was
// built without a read-write pool. A serve-only deployment leaves
// Config.IndexerDB nil and therefore has no write surface at all (DESIGN §10).
var ErrIndexingDisabled = errors.New("search: ingestion requires Config.IndexerDB (the read-write indexer role)")

// Index validates, extracts, and upserts a single document, returning instantly
// FTS-searchable results. Unchanged content (matching content hash) is skipped
// without a write. Use it for event-driven edits; use [Module.Reconcile] for
// bulk "select the rows we want searchable" passes.
func (m *Module) Index(ctx context.Context, doc IngestDocument) (IndexResult, error) {
	if m.indexer == nil {
		return IndexResult{}, ErrIndexingDisabled
	}
	return m.indexer.Index(ctx, doc)
}

// IndexBatch indexes documents in order, stopping at the first error. It is a
// convenience over [Module.Index] for callers that have several unrelated
// documents but do not want reconcile's absent-set deletion.
func (m *Module) IndexBatch(ctx context.Context, docs []IngestDocument) error {
	if m.indexer == nil {
		return ErrIndexingDisabled
	}
	for i, d := range docs {
		if _, err := m.indexer.Index(ctx, d); err != nil {
			return fmt.Errorf("search: index batch [%d]: %w", i, err)
		}
	}
	return nil
}

// Reconcile indexes the full set of documents for one source and soft-deletes
// any live document whose key is absent from that set (DESIGN §5.4). Each doc is
// stamped with source and sourceName, so callers populate only ExternalID and
// content. The bulk-reap guard aborts (ErrBulkReap) when the run would remove
// too large a fraction of the source unless opts.AllowBulkReap is set.
func (m *Module) Reconcile(ctx context.Context, source, sourceName string, docs []IngestDocument, opts ReconcileOptions) (ReconcileResult, error) {
	if m.indexer == nil {
		return ReconcileResult{}, ErrIndexingDisabled
	}
	return m.indexer.Reconcile(ctx, source, sourceName, docs, opts)
}

// Delete soft-deletes one document by its natural key. ok is false when nothing
// live matched.
func (m *Module) Delete(ctx context.Context, source, sourceName, externalID string) (ok bool, err error) {
	if m.indexer == nil {
		return false, ErrIndexingDisabled
	}
	return m.indexer.Delete(ctx, source, sourceName, externalID)
}

// storeWriter adapts *store.Store to index.Writer, translating the pipeline's
// domain types to sqlc params and back. It holds the read-write pool; the
// serving query path never reaches it.
type storeWriter struct {
	q *store.Store
}

func (s storeWriter) Hash(ctx context.Context, key index.Key) ([]byte, bool, error) {
	h, err := s.q.GetDocumentHash(ctx, store.GetDocumentHashParams{
		Source:     key.Source,
		SourceName: key.SourceName,
		SourceKey:  key.SourceKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return h, true, nil
}

func (s storeWriter) Upsert(ctx context.Context, d index.StoredDoc) (int64, error) {
	return s.q.UpsertDocument(ctx, store.UpsertDocumentParams{
		Source:       d.Source,
		SourceName:   d.SourceName,
		SourceKey:    d.SourceKey,
		Url:          d.URL,
		Title:        d.Title,
		Section:      d.Section,
		Lang:         d.Lang,
		Tags:         d.Tags,
		Headings:     d.Headings,
		Summary:      d.Summary,
		Snippet:      d.Snippet,
		BodyText:     d.BodyText,
		ContentHash:  d.ContentHash,
		PublishedAt:  toTimestamptz(d.PublishedAt),
		Visibility:   d.Visibility,
		AuthzContext: d.AuthzContext,
	})
}

func (s storeWriter) SoftDelete(ctx context.Context, key index.Key) (bool, error) {
	n, err := s.q.SoftDeleteDocument(ctx, store.SoftDeleteDocumentParams{
		Source:     key.Source,
		SourceName: key.SourceName,
		SourceKey:  key.SourceKey,
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s storeWriter) LiveKeys(ctx context.Context, source, sourceName string) ([]string, error) {
	return s.q.ListLiveSourceKeys(ctx, store.ListLiveSourceKeysParams{
		Source:     source,
		SourceName: sourceName,
	})
}

func (s storeWriter) SoftDeleteKeys(ctx context.Context, source, sourceName string, keys []string) (int64, error) {
	return s.q.SoftDeleteSourceKeys(ctx, store.SoftDeleteSourceKeysParams{
		Source:     source,
		SourceName: sourceName,
		SourceKeys: keys,
	})
}

// toTimestamptz maps a Go time to a nullable Postgres timestamp; the zero time
// stores NULL.
func toTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}
