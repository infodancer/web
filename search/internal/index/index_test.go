package index

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/infodancer/web/search/internal/extract"
)

// fakeEmbedder returns a fixed-dimension vector per piece, or a configured
// error. maxBytes, when set, makes it split sections the way a real adapter
// would, so the pipeline's handling of a multi-piece section is exercised
// without a model or a backend.
type fakeEmbedder struct {
	calls    int // EmbedPieces calls (batches), not pieces
	pieces   int // pieces embedded in total
	dim      int
	maxBytes int
	err      error
}

func (f *fakeEmbedder) SplitSections(_ string, sections []extract.Section) []Piece {
	var out []Piece
	for _, s := range sections {
		for _, text := range splitEvery(s.Text, f.maxBytes) {
			out = append(out, Piece{
				HeadingPath:   s.HeadingPath,
				Text:          text,
				TokenEstimate: len(text) / 2,
			})
		}
	}
	return out
}

func (f *fakeEmbedder) EmbedPieces(_ context.Context, _ string, pieces []Piece) ([][]float32, error) {
	f.calls++
	f.pieces += len(pieces)
	if f.err != nil {
		return nil, f.err
	}
	d := f.dim
	if d == 0 {
		d = 3
	}
	out := make([][]float32, len(pieces))
	for i, p := range pieces {
		v := make([]float32, d)
		v[0] = float32(len(p.Text)) // deterministic, content-dependent
		out[i] = v
	}
	return out, nil
}

// splitEvery cuts s into runs of at most n bytes; n <= 0 leaves it whole.
func splitEvery(s string, n int) []string {
	if n <= 0 || len(s) <= n {
		return []string{s}
	}
	var out []string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	return append(out, s)
}

// fakeChunkStore records the chunk set last written per document.
type fakeChunkStore struct {
	replaced map[int64][]Chunk
	calls    int
}

func newFakeChunkStore() *fakeChunkStore { return &fakeChunkStore{replaced: map[int64][]Chunk{}} }

func (f *fakeChunkStore) ReplaceChunks(_ context.Context, docID int64, chunks []Chunk) error {
	f.calls++
	f.replaced[docID] = chunks
	return nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeWriter is an in-memory Writer recording upserts and deletes by key.
type fakeWriter struct {
	docs    map[Key]StoredDoc
	upserts int
	deletes int
	nextID  int64
	// ids maps Key -> doc id so HasChunks can cross-reference the chunk store.
	ids map[Key]int64
	// chunks is the companion fakeChunkStore; HasChunks returns false when nil.
	chunks *fakeChunkStore
}

func newFakeWriter() *fakeWriter {
	return &fakeWriter{docs: map[Key]StoredDoc{}, ids: map[Key]int64{}}
}

// newFakeWriterWithChunks links the writer to a chunk store so HasChunks
// reflects actual chunk state written by the Indexer.
func newFakeWriterWithChunks(cs *fakeChunkStore) *fakeWriter {
	w := newFakeWriter()
	w.chunks = cs
	return w
}

func (f *fakeWriter) Hash(_ context.Context, key Key) ([]byte, bool, error) {
	d, ok := f.docs[key]
	if !ok {
		return nil, false, nil
	}
	return d.ContentHash, true, nil
}

// HasChunks returns true when the document exists and has at least one chunk
// recorded in the linked fakeChunkStore.
func (f *fakeWriter) HasChunks(_ context.Context, key Key) (bool, error) {
	if f.chunks == nil {
		return false, nil
	}
	id, ok := f.ids[key]
	if !ok {
		return false, nil
	}
	ch, ok := f.chunks.replaced[id]
	return ok && len(ch) > 0, nil
}

func (f *fakeWriter) Upsert(_ context.Context, doc StoredDoc) (int64, error) {
	f.upserts++
	if _, ok := f.docs[doc.Key]; !ok {
		f.nextID++
		f.ids[doc.Key] = f.nextID
	}
	f.docs[doc.Key] = doc
	return f.ids[doc.Key], nil
}

func (f *fakeWriter) SoftDelete(_ context.Context, key Key) (bool, error) {
	if _, ok := f.docs[key]; !ok {
		return false, nil
	}
	delete(f.docs, key)
	f.deletes++
	return true, nil
}

func (f *fakeWriter) LiveKeys(_ context.Context, source, sourceName string) ([]string, error) {
	var out []string
	for k := range f.docs {
		if k.Source == source && k.SourceName == sourceName {
			out = append(out, k.SourceKey)
		}
	}
	return out, nil
}

func (f *fakeWriter) SoftDeleteKeys(_ context.Context, source, sourceName string, keys []string) (int64, error) {
	var n int64
	for _, key := range keys {
		k := Key{Source: source, SourceName: sourceName, SourceKey: key}
		if _, ok := f.docs[k]; ok {
			delete(f.docs, k)
			n++
		}
	}
	return n, nil
}

func mdDoc(id, body string) Document {
	return Document{Source: "db", SourceName: "notes", ExternalID: id, URL: "/n/" + id, Body: body}
}

func TestIndex_NewDocumentIsCreatedAndStored(t *testing.T) {
	w := newFakeWriter()
	ix := New(w, nil, nil, nil)

	res, err := ix.Index(context.Background(), mdDoc("1", "# Hello\n\nWorld body."))
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if !res.Created || res.Skipped {
		t.Fatalf("want created, got %+v", res)
	}
	got := w.docs[Key{Source: "db", SourceName: "notes", SourceKey: "1"}]
	if got.Title != "Hello" {
		t.Errorf("title fallback to H1 failed: %q", got.Title)
	}
	if !strings.Contains(got.BodyText, "World body") {
		t.Errorf("body text not extracted: %q", got.BodyText)
	}
	if got.Visibility != "public" {
		t.Errorf("visibility default = %q", got.Visibility)
	}
	if string(got.AuthzContext) != "{}" {
		t.Errorf("authz default = %q", got.AuthzContext)
	}
}

func TestIndex_UnchangedContentIsSkipped(t *testing.T) {
	w := newFakeWriter()
	ix := New(w, nil, nil, nil)
	doc := mdDoc("1", "# H\n\nBody.")

	if _, err := ix.Index(context.Background(), doc); err != nil {
		t.Fatalf("first Index: %v", err)
	}
	if w.upserts != 1 {
		t.Fatalf("first index should upsert once, got %d", w.upserts)
	}

	res, err := ix.Index(context.Background(), doc)
	if err != nil {
		t.Fatalf("second Index: %v", err)
	}
	if !res.Skipped {
		t.Errorf("identical content should be skipped, got %+v", res)
	}
	if w.upserts != 1 {
		t.Errorf("skip should not upsert again, got %d upserts", w.upserts)
	}
}

func TestIndex_ChangedContentReindexes(t *testing.T) {
	w := newFakeWriter()
	ix := New(w, nil, nil, nil)

	if _, err := ix.Index(context.Background(), mdDoc("1", "# H\n\nBody one.")); err != nil {
		t.Fatalf("Index: %v", err)
	}
	res, err := ix.Index(context.Background(), mdDoc("1", "# H\n\nBody two."))
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if res.Skipped {
		t.Errorf("changed body should re-index, was skipped")
	}
	if w.upserts != 2 {
		t.Errorf("want 2 upserts, got %d", w.upserts)
	}
}

func TestIndex_TagOrderIsNotIdentity(t *testing.T) {
	w := newFakeWriter()
	ix := New(w, nil, nil, nil)

	d1 := mdDoc("1", "Body.")
	d1.Tags = []string{"b", "a"}
	if _, err := ix.Index(context.Background(), d1); err != nil {
		t.Fatalf("Index: %v", err)
	}
	d2 := mdDoc("1", "Body.")
	d2.Tags = []string{"a", "b"} // same set, different order
	res, err := ix.Index(context.Background(), d2)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if !res.Skipped {
		t.Errorf("reordered tags should not count as a content change")
	}
}

func TestIndex_NonPublicVisibilityRefused(t *testing.T) {
	w := newFakeWriter()
	ix := New(w, nil, nil, nil)
	doc := mdDoc("1", "Body.")
	doc.Visibility = "members"

	if _, err := ix.Index(context.Background(), doc); err == nil {
		t.Fatal("expected non-public visibility to be refused")
	}
	if w.upserts != 0 {
		t.Errorf("refused document must not be written, got %d upserts", w.upserts)
	}
}

func TestIndex_RequiredFields(t *testing.T) {
	ix := New(newFakeWriter(), nil, nil, nil)
	cases := map[string]Document{
		"no source": {ExternalID: "1", URL: "/x", Body: "b"},
		"no id":     {Source: "db", URL: "/x", Body: "b"},
		"no url":    {Source: "db", ExternalID: "1", Body: "b"},
		"blank url": {Source: "db", ExternalID: "1", URL: "   ", Body: "b"},
	}
	for name, doc := range cases {
		if _, err := ix.Index(context.Background(), doc); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestDelete_SoftDeletes(t *testing.T) {
	w := newFakeWriter()
	ix := New(w, nil, nil, nil)
	if _, err := ix.Index(context.Background(), mdDoc("1", "Body.")); err != nil {
		t.Fatalf("Index: %v", err)
	}

	ok, err := ix.Delete(context.Background(), "db", "notes", "1")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !ok || w.deletes != 1 {
		t.Errorf("expected one delete, ok=%v deletes=%d", ok, w.deletes)
	}

	ok, err = ix.Delete(context.Background(), "db", "notes", "1")
	if err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if ok {
		t.Errorf("deleting an absent document should report ok=false")
	}
}

func TestReconcile_IndexesAndReapsAbsent(t *testing.T) {
	w := newFakeWriter()
	ix := New(w, nil, nil, nil)
	// Seed five documents.
	seed := make([]Document, 0, 5)
	for _, id := range []string{"1", "2", "3", "4", "5"} {
		seed = append(seed, mdDoc(id, "Body "+id))
	}
	if _, err := ix.Reconcile(context.Background(), "db", "notes", seed, ReconcileOptions{}); err != nil {
		t.Fatalf("seed reconcile: %v", err)
	}

	// Re-reconcile with one document removed (1 of 5 = 20%, at the limit, allowed).
	next := seed[:4]
	res, err := ix.Reconcile(context.Background(), "db", "notes", next, ReconcileOptions{})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Deleted != 1 {
		t.Errorf("want 1 deleted, got %d", res.Deleted)
	}
	if res.Skipped != 4 {
		t.Errorf("unchanged docs should be skipped, got %d skipped", res.Skipped)
	}
	if _, ok := w.docs[Key{Source: "db", SourceName: "notes", SourceKey: "5"}]; ok {
		t.Errorf("absent document 5 should have been soft-deleted")
	}
}

func TestReconcile_BulkReapGuard(t *testing.T) {
	w := newFakeWriter()
	ix := New(w, nil, nil, nil)
	seed := make([]Document, 0, 5)
	for _, id := range []string{"1", "2", "3", "4", "5"} {
		seed = append(seed, mdDoc(id, "Body "+id))
	}
	if _, err := ix.Reconcile(context.Background(), "db", "notes", seed, ReconcileOptions{}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Hand over only one document: would reap 4 of 5 (80% > 20%) → abort.
	_, err := ix.Reconcile(context.Background(), "db", "notes", seed[:1], ReconcileOptions{})
	if !errors.Is(err, ErrBulkReap) {
		t.Fatalf("expected ErrBulkReap, got %v", err)
	}
	if w.deletes != 0 {
		t.Errorf("guard must abort before deleting, got %d deletes", w.deletes)
	}

	// Same removal with the override proceeds.
	res, err := ix.Reconcile(context.Background(), "db", "notes", seed[:1], ReconcileOptions{AllowBulkReap: true})
	if err != nil {
		t.Fatalf("override reconcile: %v", err)
	}
	if res.Deleted != 4 {
		t.Errorf("override should delete 4, got %d", res.Deleted)
	}
}

func TestReconcile_StampsSourceOntoDocs(t *testing.T) {
	w := newFakeWriter()
	ix := New(w, nil, nil, nil)
	// Document carries the wrong source/name; Reconcile must stamp the scope.
	d := Document{Source: "wrong", SourceName: "wrong", ExternalID: "1", URL: "/n/1", Body: "Body."}
	if _, err := ix.Reconcile(context.Background(), "db", "notes", []Document{d}, ReconcileOptions{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, ok := w.docs[Key{Source: "db", SourceName: "notes", SourceKey: "1"}]; !ok {
		t.Errorf("document was not stored under the reconcile scope; keys=%v", keysOf(w))
	}
}

func TestIndex_EmbedsSectionsAsChunks(t *testing.T) {
	w := newFakeWriter()
	emb := &fakeEmbedder{dim: 4}
	cs := newFakeChunkStore()
	ix := New(w, emb, cs, nil)

	body := "# Title\n\nIntro.\n\n## Alpha\n\nAlpha body.\n\n## Beta\n\nBeta body."
	res, err := ix.Index(context.Background(), mdDoc("1", body))
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	chunks := cs.replaced[res.DocID]
	if len(chunks) != 3 {
		t.Fatalf("want 3 chunks (one per section), got %d", len(chunks))
	}
	if chunks[0].Ord != 0 || chunks[1].Ord != 1 || chunks[2].Ord != 2 {
		t.Errorf("ords not sequential: %d,%d,%d", chunks[0].Ord, chunks[1].Ord, chunks[2].Ord)
	}
	if chunks[1].HeadingPath != "Title > Alpha" {
		t.Errorf("heading path = %q, want %q", chunks[1].HeadingPath, "Title > Alpha")
	}
	if len(chunks[0].Embedding) != 4 {
		t.Errorf("embedding dim = %d, want 4", len(chunks[0].Embedding))
	}
	if emb.pieces != 3 {
		t.Errorf("embedded %d pieces, want 3", emb.pieces)
	}
	if emb.calls != 1 {
		t.Errorf("embedder called %d times, want 1 batch for the whole document", emb.calls)
	}
}

func TestIndex_SkippedContentDoesNotReembed(t *testing.T) {
	emb := &fakeEmbedder{}
	cs := newFakeChunkStore()
	// Link the writer to the chunk store so HasChunks reflects actual state.
	w := newFakeWriterWithChunks(cs)
	ix := New(w, emb, cs, nil)
	doc := mdDoc("1", "# H\n\nBody.")

	if _, err := ix.Index(context.Background(), doc); err != nil {
		t.Fatalf("first Index: %v", err)
	}
	first := emb.pieces
	if _, err := ix.Index(context.Background(), doc); err != nil {
		t.Fatalf("second Index: %v", err)
	}
	if emb.pieces != first {
		t.Errorf("unchanged content should not re-embed: %d then %d pieces", first, emb.pieces)
	}
}

func TestIndex_EmbedFailureIsNonFatal(t *testing.T) {
	w := newFakeWriter()
	emb := &fakeEmbedder{err: errors.New("embedder down")}
	cs := newFakeChunkStore()
	ix := New(w, emb, cs, quietLogger())

	res, err := ix.Index(context.Background(), mdDoc("1", "# H\n\nBody."))
	if err != nil {
		t.Fatalf("embed failure must be non-fatal, got %v", err)
	}
	if res.Skipped || !res.Created {
		t.Errorf("document should still be created/FTS-indexed, got %+v", res)
	}
	if w.upserts != 1 {
		t.Errorf("document must be upserted despite embed failure, got %d upserts", w.upserts)
	}
	if cs.calls != 0 {
		t.Errorf("chunk store should not be written when embedding failed, got %d calls", cs.calls)
	}
}

// TestIndex_BackfillsVectorsForFTSOnlyDoc verifies the core backfill scenario:
// a doc indexed with no embedder (FTS-only) gets its chunks written when an
// embedder is wired in and the same -- unchanged -- document is re-indexed.
// The content hash must not be the gating factor for the embedding step.
func TestIndex_BackfillsVectorsForFTSOnlyDoc(t *testing.T) {
	cs := newFakeChunkStore()
	w := newFakeWriterWithChunks(cs)
	doc := mdDoc("1", "# Title\n\nBody text.")

	// Phase 1: index with no embedder -- FTS-only.
	ixFTS := New(w, nil, nil, nil)
	res, err := ixFTS.Index(context.Background(), doc)
	if err != nil {
		t.Fatalf("FTS index: %v", err)
	}
	if !res.Created {
		t.Fatalf("expected doc to be created, got %+v", res)
	}
	if cs.calls != 0 {
		t.Errorf("no embedder: chunk store should not be written, got %d calls", cs.calls)
	}

	// Phase 2: re-index the same unchanged doc with an embedder now configured.
	emb := &fakeEmbedder{dim: 3}
	ixEmbed := New(w, emb, cs, quietLogger())
	res2, err := ixEmbed.Index(context.Background(), doc)
	if err != nil {
		t.Fatalf("backfill index: %v", err)
	}
	if res2.Skipped {
		t.Errorf("FTS-only doc with active embedder must not be skipped")
	}
	if emb.calls == 0 {
		t.Errorf("embedder was never called during backfill")
	}
	chunks := cs.replaced[res2.DocID]
	if len(chunks) == 0 {
		t.Errorf("doc should now have chunks after backfill, got none")
	}

	// Phase 3: re-index once more -- now it has chunks, so it should be skipped.
	callsBefore := emb.calls
	res3, err := ixEmbed.Index(context.Background(), doc)
	if err != nil {
		t.Fatalf("third index: %v", err)
	}
	if !res3.Skipped {
		t.Errorf("doc already has chunks and unchanged content: should be skipped, got %+v", res3)
	}
	if emb.calls != callsBefore {
		t.Errorf("embedder should not be called again: %d before, %d after", callsBefore, emb.calls)
	}
}

func keysOf(w *fakeWriter) []Key {
	out := make([]Key, 0, len(w.docs))
	for k := range w.docs {
		out = append(out, k)
	}
	return out
}

// A section too large to embed whole becomes several chunks that share its
// heading path, in document order. Before this, the tail of a long section was
// handed to the backend whole and silently truncated -- a normal-looking vector
// computed from a document whose end was thrown away (DESIGN §5.2).
func TestIndex_OversizedSectionBecomesSeveralChunks(t *testing.T) {
	w := newFakeWriter()
	emb := &fakeEmbedder{dim: 4, maxBytes: 20}
	cs := newFakeChunkStore()
	ix := New(w, emb, cs, nil)

	long := strings.Repeat("alpha body text. ", 6) // ~102 bytes => 6 pieces
	body := "# Title\n\n## Alpha\n\n" + long
	res, err := ix.Index(context.Background(), mdDoc("1", body))
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	chunks := cs.replaced[res.DocID]
	if len(chunks) < 4 {
		t.Fatalf("want the long section split into several chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c.Ord != i {
			t.Errorf("chunk %d has Ord %d; ords must be sequential across the split", i, c.Ord)
		}
		if len(c.Text) > emb.maxBytes {
			t.Errorf("chunk %d is %d bytes, over the %d-byte budget", i, len(c.Text), emb.maxBytes)
		}
		if c.HeadingPath != "Title > Alpha" {
			t.Errorf("chunk %d heading path = %q, want every piece to keep its section's path",
				i, c.HeadingPath)
		}
		if len(c.Embedding) != 4 {
			t.Errorf("chunk %d has no embedding", i)
		}
	}
}

// Every piece is embedded in one call, not one call each. Splitting multiplies
// the piece count, so a request per piece would turn one long document into
// dozens of round trips.
func TestIndex_EmbedsAllPiecesInOneCall(t *testing.T) {
	w := newFakeWriter()
	emb := &fakeEmbedder{dim: 3, maxBytes: 20}
	cs := newFakeChunkStore()
	ix := New(w, emb, cs, nil)

	body := "# Title\n\n## Alpha\n\n" + strings.Repeat("alpha body text. ", 6)
	if _, err := ix.Index(context.Background(), mdDoc("1", body)); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if emb.calls != 1 {
		t.Errorf("embedder called %d times, want 1 for the whole document", emb.calls)
	}
	if emb.pieces < 4 {
		t.Errorf("embedded %d pieces, want the split to have produced several", emb.pieces)
	}
}

// The token estimate comes from the splitter, which knows the model, rather
// than from a bytes/4 guess in the pipeline, which does not.
func TestIndex_TokenEstimateComesFromTheSplitter(t *testing.T) {
	w := newFakeWriter()
	emb := &fakeEmbedder{dim: 3}
	cs := newFakeChunkStore()
	ix := New(w, emb, cs, nil)

	res, err := ix.Index(context.Background(), mdDoc("1", "# Title\n\n## Alpha\n\nAlpha body."))
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	for i, c := range cs.replaced[res.DocID] {
		// The fake reports len/2; the pipeline must pass it through untouched.
		if want := len(c.Text) / 2; c.TokenEstimate != want {
			t.Errorf("chunk %d token estimate = %d, want the splitter's %d", i, c.TokenEstimate, want)
		}
	}
}

// A document with nothing embeddable clears its chunk set rather than skipping
// the write: the content that used to be there must not keep matching.
func TestIndex_EmptySectionsClearChunks(t *testing.T) {
	w := newFakeWriter()
	emb := &fakeEmbedder{dim: 3}
	cs := newFakeChunkStore()
	ix := New(w, emb, cs, nil)

	res, err := ix.Index(context.Background(), mdDoc("1", "# Title Only"))
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	chunks, ok := cs.replaced[res.DocID]
	if !ok {
		t.Fatal("ReplaceChunks was not called for a document with no embeddable text")
	}
	if len(chunks) != 0 {
		t.Errorf("want no chunks, got %d", len(chunks))
	}
	if emb.calls != 0 {
		t.Errorf("embedder called %d times for a document with nothing to embed, want 0", emb.calls)
	}
}
