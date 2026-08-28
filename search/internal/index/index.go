// Package index implements the ingestion pipeline (DESIGN §5): validate →
// extract → hash → gate → upsert, plus per-document delete and full-source
// reconcile. It is driven in-process by a consuming application; there is no
// HTTP ingest surface.
//
// The pipeline talks to the corpus through the narrow [Writer] interface rather
// than the store directly, so its logic is unit-testable without a database and
// the read-write coupling stays explicit (DESIGN §10).
package index

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/infodancer/web/search/internal/extract"
)

// BodyKind names the format of a document body so the pipeline picks the right
// extractor.
type BodyKind string

const (
	// BodyMarkdown is rendered through the audited Markdown pipeline, then
	// reduced to text. The default when a document leaves BodyKind empty.
	BodyMarkdown BodyKind = "markdown"
	// BodyHTML is parsed as HTML and reduced to text. Only the text is stored.
	BodyHTML BodyKind = "html"
	// BodyText is stored as-is (whitespace-normalized), no markup processing.
	BodyText BodyKind = "text"
)

// Document is one unit of content handed to the indexer. It mirrors DESIGN §4.3:
// the same shape regardless of whether the consumer pulled it from a database
// row, a file, or built it in code.
type Document struct {
	// Source and SourceName scope the document; SourceName distinguishes
	// corpora within a source (e.g. one DB view or site key). ExternalID is the
	// stable per-source identifier and becomes the document's natural key.
	Source     string
	SourceName string
	ExternalID string

	// URL is the canonical result URL. Required: the pipeline does not invent one.
	URL string

	// Title, Summary, Section, Tags, Lang are metadata. Title falls back to the
	// body's first H1 when empty. Lang defaults to "en".
	Title   string
	Summary string
	Section string
	Tags    []string
	Lang    string

	// Body is the content in BodyKind format. BodyKind defaults to BodyMarkdown.
	Body     string
	BodyKind BodyKind

	// PublishedAt is the document's own date; the zero value stores NULL.
	PublishedAt time.Time

	// Visibility must be "" or "public" in v1; anything else is hard-refused
	// (DESIGN §4.2). AuthzContext is opaque consumer metadata, "{}" when empty.
	Visibility   string
	AuthzContext []byte
}

// Key is a document's natural key.
type Key struct {
	Source     string
	SourceName string
	SourceKey  string
}

// StoredDoc is the fully-projected, text-only document the Writer persists.
type StoredDoc struct {
	Key
	URL          string
	Title        string
	Section      string
	Lang         string
	Tags         []string
	Headings     string
	Summary      string
	Snippet      string
	BodyText     string
	ContentHash  []byte
	PublishedAt  time.Time // zero => NULL
	Visibility   string
	AuthzContext []byte
}

// Writer is the slice of the data layer the pipeline needs. The store-backed
// adapter lives in the parent package; tests supply a fake.
type Writer interface {
	// Hash returns the stored content hash for a key. found is false when the
	// document has never been indexed.
	Hash(ctx context.Context, key Key) (hash []byte, found bool, err error)
	// HasChunks reports whether a live document already has at least one chunk
	// (i.e. was previously embedded). Returns false when the document does not
	// exist or has no chunks. The pipeline uses this to detect FTS-only docs
	// that need backfill when an embedder is wired in after initial indexing.
	HasChunks(ctx context.Context, key Key) (bool, error)
	// Upsert persists doc and returns its row id.
	Upsert(ctx context.Context, doc StoredDoc) (int64, error)
	// SoftDelete marks one document deleted; deleted is false when there was
	// nothing live to delete.
	SoftDelete(ctx context.Context, key Key) (deleted bool, err error)
	// LiveKeys lists the source keys of every live document in a source.
	LiveKeys(ctx context.Context, source, sourceName string) ([]string, error)
	// SoftDeleteKeys soft-deletes the named keys within a source and returns the
	// number actually deleted.
	SoftDeleteKeys(ctx context.Context, source, sourceName string, keys []string) (int64, error)
}

// defaultReapFraction caps how much of a source a single Reconcile may remove
// before it aborts as a suspected upstream failure (DESIGN §5.4).
const defaultReapFraction = 0.20

// snippetMaxRunes bounds the plain-text listing excerpt derived from body text.
const snippetMaxRunes = 240

// Piece is one embeddable slice of a section: a section short enough to embed
// whole yields exactly one, a long one yields several sharing its heading path
// (DESIGN §5.2).
type Piece struct {
	HeadingPath   string
	Text          string
	TokenEstimate int
}

// Embedder turns a document's sections into vectors. It is optional: a nil
// embedder makes the corpus FTS-only and no chunks are written.
//
// The split is part of this interface rather than the pipeline because every
// input to it is model knowledge: the byte budget the model accepts, the token
// target chunks are sized to, and the size of the document-side header that
// budget is charged for (DESIGN §5.3). The pipeline stays free of all of it and
// keeps only what it owns -- ordering, persistence, and the hash gate.
type Embedder interface {
	// SplitSections cuts sections into pieces that each fit the model's input
	// budget once document-side formatting is applied, in document order.
	SplitSections(title string, sections []extract.Section) []Piece
	// EmbedPieces returns one vector per piece, index-aligned. An error fails
	// the whole document: see embedDocument.
	EmbedPieces(ctx context.Context, title string, pieces []Piece) ([][]float32, error)
}

// Chunk is one embeddable slice of a document with its embedding, ready to
// persist (DESIGN §3.2).
type Chunk struct {
	Ord           int
	HeadingPath   string
	Text          string
	TokenEstimate int
	Embedding     []float32
}

// ChunkStore persists a document's chunks, replacing any prior set atomically.
type ChunkStore interface {
	ReplaceChunks(ctx context.Context, documentID int64, chunks []Chunk) error
}

// Indexer runs the ingestion pipeline against a Writer. Safe for concurrent use.
type Indexer struct {
	w            Writer
	emb          Embedder   // optional; nil => FTS-only, no chunks written
	chunks       ChunkStore // required iff emb != nil
	log          *slog.Logger
	reapFraction float64
}

// New builds an Indexer over w. emb and chunks are optional and must be supplied
// together: when both are non-nil, each changed document's sections are embedded
// and stored as chunks (DESIGN §5.3); when emb is nil the corpus is FTS-only.
func New(w Writer, emb Embedder, chunks ChunkStore, log *slog.Logger) *Indexer {
	if log == nil {
		log = slog.Default()
	}
	return &Indexer{w: w, emb: emb, chunks: chunks, log: log, reapFraction: defaultReapFraction}
}

// Result reports the outcome of indexing one document.
type Result struct {
	DocID int64
	// Skipped is true when the content hash was unchanged and no write occurred.
	Skipped bool
	// Created is true when the document did not exist before this call.
	Created bool
}

// Index validates, extracts, hashes, and upserts a single document. When the
// content hash matches what is stored, it skips the write entirely -- the hash
// is the incremental-indexing cache (DESIGN §5.1).
//
// Exception: when an embedder is configured and the existing document has no
// chunks (was indexed FTS-only before the embedder was wired in), the embedding
// step runs even though the content is unchanged. The upsert is still skipped --
// only the chunk backfill is applied.
func (ix *Indexer) Index(ctx context.Context, doc Document) (Result, error) {
	sd, sections, err := ix.project(doc)
	if err != nil {
		return Result{}, err
	}

	stored, found, err := ix.w.Hash(ctx, sd.Key)
	if err != nil {
		return Result{}, fmt.Errorf("index: read hash: %w", err)
	}

	hashMatch := found && bytesEqual(stored, sd.ContentHash)

	// When the hash matches and no embedder is active, nothing to do.
	if hashMatch && (ix.emb == nil || ix.chunks == nil) {
		return Result{Skipped: true}, nil
	}

	// When the hash matches but an embedder is configured, check whether this
	// doc already has chunks. If it does, it is fully up-to-date and can be
	// skipped. If it doesn't, it was indexed FTS-only and needs a chunk backfill
	// even though the content hasn't changed.
	if hashMatch {
		hasChunks, err := ix.w.HasChunks(ctx, sd.Key)
		if err != nil {
			return Result{}, fmt.Errorf("index: check chunks: %w", err)
		}
		if hasChunks {
			return Result{Skipped: true}, nil
		}
		// FTS-only doc with an active embedder: fetch the doc id from the store
		// so we can write chunks without re-upserting the (unchanged) document.
		id, err := ix.w.Upsert(ctx, sd)
		if err != nil {
			return Result{}, fmt.Errorf("index: upsert (chunk backfill): %w", err)
		}
		if err := ix.embedDocument(ctx, id, sd.Title, sections); err != nil {
			ix.log.Warn("index: embedding failed; document remains FTS-only until re-indexed",
				"source", sd.Source, "key", sd.SourceKey, "err", err)
		}
		return Result{DocID: id, Created: false}, nil
	}

	id, err := ix.w.Upsert(ctx, sd)
	if err != nil {
		return Result{}, fmt.Errorf("index: upsert: %w", err)
	}

	// Embedding is synchronous but non-fatal: the document is already
	// FTS-searchable from the upsert above, so an embedder outage degrades to
	// FTS-only for this document rather than failing the caller's write (the
	// chosen contract). A consumer that does not want embed latency on its
	// request path calls Index from its own goroutine.
	if ix.emb != nil && ix.chunks != nil {
		if err := ix.embedDocument(ctx, id, sd.Title, sections); err != nil {
			ix.log.Warn("index: embedding failed; document is FTS-only until re-indexed",
				"source", sd.Source, "key", sd.SourceKey, "err", err)
		}
	}

	return Result{DocID: id, Created: !found}, nil
}

// embedDocument splits the document's non-empty sections into budget-sized
// pieces, embeds them, and replaces the document's chunk set (DESIGN §5.2/§5.3).
//
// One failed piece fails the whole document, deliberately. A partially embedded
// document reads as *complete* to the backfill -- HasChunks is true, so the next
// run skips it -- leaving a permanent hole in the middle of the document that
// nothing will ever come back for. Failing whole means it is retried whole.
func (ix *Indexer) embedDocument(ctx context.Context, docID int64, title string, sections []extract.Section) error {
	nonEmpty := make([]extract.Section, 0, len(sections))
	for _, s := range sections {
		if s.Text != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}

	pieces := ix.emb.SplitSections(title, nonEmpty)
	vectors := make([][]float32, 0, len(pieces))
	if len(pieces) > 0 {
		var err error
		if vectors, err = ix.emb.EmbedPieces(ctx, title, pieces); err != nil {
			return fmt.Errorf("embed pieces: %w", err)
		}
		if len(vectors) != len(pieces) {
			return fmt.Errorf("embed pieces: got %d vectors for %d pieces", len(vectors), len(pieces))
		}
	}

	chunks := make([]Chunk, len(pieces))
	for i, p := range pieces {
		chunks[i] = Chunk{
			Ord:           i,
			HeadingPath:   p.HeadingPath,
			Text:          p.Text,
			TokenEstimate: p.TokenEstimate,
			Embedding:     vectors[i],
		}
	}
	if err := ix.chunks.ReplaceChunks(ctx, docID, chunks); err != nil {
		return fmt.Errorf("replace chunks: %w", err)
	}
	return nil
}

// Delete soft-deletes one document by its natural key. ok is false when nothing
// live matched.
func (ix *Indexer) Delete(ctx context.Context, source, sourceName, externalID string) (bool, error) {
	key := Key{Source: source, SourceName: sourceName, SourceKey: externalID}
	if key.Source == "" || key.SourceKey == "" {
		return false, errors.New("index: delete requires source and external id")
	}
	ok, err := ix.w.SoftDelete(ctx, key)
	if err != nil {
		return false, fmt.Errorf("index: delete: %w", err)
	}
	return ok, nil
}

// ReconcileOptions tunes a Reconcile run.
type ReconcileOptions struct {
	// AllowBulkReap disables the >reapFraction abort guard for this run. Use it
	// for a legitimately large removal the guard would otherwise reject.
	AllowBulkReap bool
}

// ReconcileResult summarizes a reconcile run.
type ReconcileResult struct {
	Indexed int // documents written (created or updated)
	Skipped int // documents unchanged
	Deleted int // documents soft-deleted as absent
}

// ErrBulkReap is returned when a Reconcile would soft-delete more than the
// configured fraction of a source's live documents, which usually signals an
// upstream failure (an empty rebuild, a broken query) rather than real deletions.
var ErrBulkReap = errors.New("index: reconcile would delete too large a fraction of the source")

// Reconcile indexes the full set of documents for one source and soft-deletes
// any live document whose key is absent from that set (DESIGN §5.4). It is the
// correct entry point for the "select the rows we want searchable and hand them
// over" path: per-document Index alone would orphan rows deleted upstream.
//
// Every doc is stamped with the given source and sourceName, so callers need
// only populate ExternalID and content on each.
func (ix *Indexer) Reconcile(ctx context.Context, source, sourceName string, docs []Document, opts ReconcileOptions) (ReconcileResult, error) {
	if source == "" {
		return ReconcileResult{}, errors.New("index: reconcile requires a source")
	}

	var res ReconcileResult
	kept := make(map[string]struct{}, len(docs))
	for _, d := range docs {
		d.Source = source
		d.SourceName = sourceName
		r, err := ix.Index(ctx, d)
		if err != nil {
			return res, err
		}
		kept[d.ExternalID] = struct{}{}
		if r.Skipped {
			res.Skipped++
		} else {
			res.Indexed++
		}
	}

	live, err := ix.w.LiveKeys(ctx, source, sourceName)
	if err != nil {
		return res, fmt.Errorf("index: reconcile: live keys: %w", err)
	}
	absent := make([]string, 0)
	for _, k := range live {
		if _, ok := kept[k]; !ok {
			absent = append(absent, k)
		}
	}
	if len(absent) == 0 {
		return res, nil
	}

	if !opts.AllowBulkReap && len(live) > 0 {
		if float64(len(absent)) > ix.reapFraction*float64(len(live)) {
			return res, fmt.Errorf("%w: %d of %d live documents in source %q/%q (pass AllowBulkReap to override)",
				ErrBulkReap, len(absent), len(live), source, sourceName)
		}
	}

	n, err := ix.w.SoftDeleteKeys(ctx, source, sourceName, absent)
	if err != nil {
		return res, fmt.Errorf("index: reconcile: soft-delete: %w", err)
	}
	res.Deleted = int(n)
	return res, nil
}

// project validates a Document and reduces it to the text-only StoredDoc the
// writer persists, computing the content hash over the normalized projection
// (DESIGN §5.1).
func (ix *Indexer) project(doc Document) (StoredDoc, []extract.Section, error) {
	if doc.Source == "" {
		return StoredDoc{}, nil, errors.New("index: document Source is required")
	}
	if doc.ExternalID == "" {
		return StoredDoc{}, nil, errors.New("index: document ExternalID is required")
	}
	if strings.TrimSpace(doc.URL) == "" {
		return StoredDoc{}, nil, errors.New("index: document URL is required")
	}

	visibility := doc.Visibility
	if visibility == "" {
		visibility = "public"
	}
	if visibility != "public" {
		// v1 hard-refusal: nothing non-public is ever indexed, so nothing
		// non-public can leak (DESIGN §4.2).
		return StoredDoc{}, nil, fmt.Errorf("index: refusing document %q: visibility %q is not public", doc.ExternalID, visibility)
	}

	ex, err := ix.extract(doc)
	if err != nil {
		return StoredDoc{}, nil, err
	}

	title := doc.Title
	if title == "" {
		title = ex.Title
	}
	lang := doc.Lang
	if lang == "" {
		lang = "en"
	}
	authz := doc.AuthzContext
	if len(authz) == 0 {
		authz = []byte("{}")
	}
	tags := normalizeTags(doc.Tags)

	sd := StoredDoc{
		Key:          Key{Source: doc.Source, SourceName: doc.SourceName, SourceKey: doc.ExternalID},
		URL:          strings.TrimSpace(doc.URL),
		Title:        title,
		Section:      doc.Section,
		Lang:         lang,
		Tags:         tags,
		Headings:     ex.Headings,
		Summary:      doc.Summary,
		Snippet:      snippet(ex.BodyText),
		BodyText:     ex.BodyText,
		PublishedAt:  doc.PublishedAt,
		Visibility:   visibility,
		AuthzContext: authz,
	}
	sd.ContentHash = contentHash(sd)
	return sd, ex.Sections, nil
}

// extract dispatches to the right body extractor for the document's kind.
func (ix *Indexer) extract(doc Document) (extract.Extracted, error) {
	kind := doc.BodyKind
	if kind == "" {
		kind = BodyMarkdown
	}
	switch kind {
	case BodyMarkdown:
		return extract.Markdown(doc.Body)
	case BodyHTML:
		return extract.HTML(doc.Body)
	case BodyText:
		return extract.Text(doc.Body), nil
	default:
		return extract.Extracted{}, fmt.Errorf("index: unknown body kind %q", kind)
	}
}

// contentHash is the sha256 over the normalized projection that drives
// incremental indexing (DESIGN §5.1): title, body text, tags, url, visibility,
// and authz context. Fields are length-delimited so no concatenation collision
// can forge a matching hash, and tags are sorted so order is not identity.
func contentHash(sd StoredDoc) []byte {
	var buf []byte
	add := func(label, v string) {
		buf = append(buf, label...)
		buf = append(buf, ':')
		buf = strconv.AppendInt(buf, int64(len(v)), 10)
		buf = append(buf, ':')
		buf = append(buf, v...)
	}
	add("url", sd.URL)
	add("title", sd.Title)
	add("visibility", sd.Visibility)
	add("body", sd.BodyText)
	add("authz", string(sd.AuthzContext))
	for _, t := range sd.Tags {
		add("tag", t)
	}
	sum := sha256.Sum256(buf)
	return sum[:]
}

// normalizeTags trims, drops empties, and sorts tags so equal tag sets hash and
// store identically regardless of input order.
func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// snippet builds a plain-text listing excerpt from body text. It is stored as
// text and HTML-escaped at render; it is only a fallback for ts_headline.
func snippet(body string) string {
	r := []rune(body)
	if len(r) <= snippetMaxRunes {
		return body
	}
	cut := snippetMaxRunes
	// Prefer to break on the last space within the budget.
	for i := cut; i > 0; i-- {
		if r[i] == ' ' {
			cut = i
			break
		}
	}
	return strings.TrimSpace(string(r[:cut])) + "…"
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
