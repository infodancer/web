package query

import (
	"context"
	"errors"
	"fmt"
	"testing"

	embedding "github.com/matthewjhunter/go-embedding"

	"github.com/infodancer/web/search/store"
)

// fakeStore returns a fixed set of FTS rows regardless of query. The vector and
// hydrate methods return nothing, so it models an FTS-only deployment unless a
// test sets vecHits/byID.
type fakeStore struct {
	rows    []store.SearchDocumentsFTSRow
	err     error
	vecHits []store.VectorHit
	byID    []store.GetDocumentsByIDsRow
}

func (f fakeStore) SearchDocumentsFTS(context.Context, store.SearchDocumentsFTSParams) ([]store.SearchDocumentsFTSRow, error) {
	return f.rows, f.err
}

func (f fakeStore) SearchChunksVector(context.Context, []float32, string, int32) ([]store.VectorHit, error) {
	return f.vecHits, nil
}

func (f fakeStore) GetDocumentsByIDs(_ context.Context, ids []int64) ([]store.GetDocumentsByIDsRow, error) {
	want := make(map[int64]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []store.GetDocumentsByIDsRow
	for _, r := range f.byID {
		if want[r.ID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func rows(titles ...string) []store.SearchDocumentsFTSRow {
	out := make([]store.SearchDocumentsFTSRow, len(titles))
	for i, t := range titles {
		out[i] = store.SearchDocumentsFTSRow{
			ID:       int64(i + 1),
			Url:      "/" + t,
			Title:    t,
			BodyText: "body of " + t,
			Rank:     float32(len(titles) - i), // descending FTS rank by input order
		}
	}
	return out
}

func titles(rs []Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Title
	}
	return out
}

// fakeQueryEmbedder lets the vector arm run; the actual vector it returns is
// irrelevant because the fake store ignores it and returns canned hits.
type fakeQueryEmbedder struct{}

func (fakeQueryEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}
func (fakeQueryEmbedder) Model() string { return "fake" }
func (fakeQueryEmbedder) Fingerprint() embedding.Fingerprint {
	return embedding.Fingerprint{Model: "fake", Dim: 3}
}

// reverseReranker reorders candidates into reverse input order, proving the
// stage actually applies the reranker's ordering.
type reverseReranker struct{}

func (reverseReranker) Model() string { return "reverse-test" }
func (reverseReranker) Rerank(_ context.Context, req embedding.RerankRequest) ([]embedding.RerankResult, error) {
	out := make([]embedding.RerankResult, len(req.Documents))
	for i := range req.Documents {
		// Highest score to the last document.
		out[i] = embedding.RerankResult{Index: len(req.Documents) - 1 - i, Score: float64(i)}
	}
	return out, nil
}

// errReranker fails every call with a configurable error.
type errReranker struct{ err error }

func (errReranker) Model() string { return "err-test" }
func (e errReranker) Rerank(context.Context, embedding.RerankRequest) ([]embedding.RerankResult, error) {
	return nil, e.err
}

func TestSearch_NilReranker_IsPassThrough(t *testing.T) {
	e := New(fakeStore{rows: rows("a", "b", "c")}, Options{Fanout: 100})

	got, err := e.Search(context.Background(), "anything", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if want := []string{"a", "b", "c"}; !equal(titles(got), want) {
		t.Errorf("nil reranker should preserve FTS order: got %v, want %v", titles(got), want)
	}
}

func TestSearch_Reranker_ReordersResults(t *testing.T) {
	e := New(fakeStore{rows: rows("a", "b", "c")}, Options{Reranker: reverseReranker{}, Fanout: 100})

	got, err := e.Search(context.Background(), "anything", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if want := []string{"c", "b", "a"}; !equal(titles(got), want) {
		t.Errorf("reranker order not applied: got %v, want %v", titles(got), want)
	}
}

func TestSearch_RerankUnavailable_KeepsFirstStageOrder(t *testing.T) {
	e := New(fakeStore{rows: rows("a", "b", "c")}, Options{Reranker: errReranker{err: embedding.ErrRerankUnavailable}, Fanout: 100})

	got, err := e.Search(context.Background(), "anything", 0)
	if err != nil {
		t.Fatalf("Search should degrade, not fail, when backend unavailable: %v", err)
	}
	if want := []string{"a", "b", "c"}; !equal(titles(got), want) {
		t.Errorf("unavailable rerank should keep FTS order: got %v, want %v", titles(got), want)
	}
}

func TestSearch_RerankCallerBug_Surfaces(t *testing.T) {
	// A non-unavailable error (e.g. a 4xx misconfiguration) must surface
	// rather than silently degrade (DESIGN §8 / go-embedding contract).
	bug := errors.New("400 bad request")
	e := New(fakeStore{rows: rows("a", "b", "c")}, Options{Reranker: errReranker{err: bug}, Fanout: 100})

	_, err := e.Search(context.Background(), "anything", 0)
	if err == nil {
		t.Fatal("expected rerank caller-bug error to surface")
	}
}

func TestSearch_BlankQuery_ReturnsNothing(t *testing.T) {
	e := New(fakeStore{rows: rows("a")}, Options{Fanout: 100})
	got, err := e.Search(context.Background(), "   ", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("blank query should return no results, got %d", len(got))
	}
}

func TestSearch_Limit_Truncates(t *testing.T) {
	e := New(fakeStore{rows: rows("a", "b", "c", "d")}, Options{Fanout: 100})
	got, err := e.Search(context.Background(), "x", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("limit 2: got %d results", len(got))
	}
}

func TestSearch_StorePropagatesError(t *testing.T) {
	e := New(fakeStore{err: fmt.Errorf("boom")}, Options{Fanout: 100})
	if _, err := e.Search(context.Background(), "x", 0); err == nil {
		t.Fatal("expected store error to propagate")
	}
}

func TestSearch_RRF_VectorArmReordersFused(t *testing.T) {
	// FTS order is a,b,c (ids 1,2,3). The vector arm ranks c first, a second.
	// RRF should pull c up past b, giving a,c,b.
	fs := fakeStore{
		rows:    rows("a", "b", "c"),
		vecHits: []store.VectorHit{{DocID: 3, Distance: 0.1}, {DocID: 1, Distance: 0.2}},
	}
	e := New(fs, Options{Embedder: fakeQueryEmbedder{}, Fanout: 100})

	got, err := e.Search(context.Background(), "anything", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if want := []string{"a", "c", "b"}; !equal(titles(got), want) {
		t.Errorf("RRF fusion order = %v, want %v", titles(got), want)
	}
}

func TestSearch_RRF_HydratesVectorOnlyDocument(t *testing.T) {
	// Doc 9 surfaces only via the vector arm and must be hydrated by id and
	// included in results (with its stored snippet, since it has no FTS headline).
	fs := fakeStore{
		rows:    rows("a", "b"), // ids 1,2
		vecHits: []store.VectorHit{{DocID: 9, Distance: 0.01}},
		byID: []store.GetDocumentsByIDsRow{
			{ID: 9, Url: "/z", Title: "zeta", Snippet: "zeta snippet", BodyText: "zeta body"},
		},
	}
	e := New(fs, Options{Embedder: fakeQueryEmbedder{}, Fanout: 100})

	got, err := e.Search(context.Background(), "anything", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var zeta *Result
	for i := range got {
		if got[i].DocID == 9 {
			zeta = &got[i]
		}
	}
	if zeta == nil {
		t.Fatalf("vector-only document 9 missing from results: %v", titles(got))
	}
	if zeta.URL != "/z" || string(zeta.Snippet) != "zeta snippet" {
		t.Errorf("hydrated doc not populated: %+v", zeta)
	}
}

func TestHighlight_EscapesTextAndKeepsMarks(t *testing.T) {
	// Document text with markup-significant characters wrapped by sentinels.
	headline := []byte(markStart + "<script>" + markEnd + " & ok")
	got := string(highlight(headline, ""))
	want := "<mark>&lt;script&gt;</mark> &amp; ok"
	if got != want {
		t.Errorf("highlight() = %q, want %q", got, want)
	}
}

func TestHighlight_FallsBackToSnippet(t *testing.T) {
	got := string(highlight(nil, "plain <b> snippet"))
	want := "plain &lt;b&gt; snippet"
	if got != want {
		t.Errorf("highlight() fallback = %q, want %q", got, want)
	}
}

func equal(a, b []string) bool {
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
