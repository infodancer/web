package query

import (
	"context"
	"testing"
	"time"

	embedding "github.com/matthewjhunter/go-embedding"
)

// blockingEmbedder models a backend that accepts the request and never
// answers: it returns only when its context is done.
type blockingEmbedder struct{}

func (blockingEmbedder) Embed(ctx context.Context, _ []string) ([][]float32, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (blockingEmbedder) Model() string { return "blocking" }
func (blockingEmbedder) Fingerprint() embedding.Fingerprint {
	return embedding.Fingerprint{Model: "blocking", Dim: 3}
}

// blockingReranker is the rerank counterpart of blockingEmbedder.
type blockingReranker struct{}

func (blockingReranker) Model() string { return "blocking" }
func (blockingReranker) Rerank(ctx context.Context, _ embedding.RerankRequest) ([]embedding.RerankResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestNew_BackendTimeoutDefaults(t *testing.T) {
	if got := New(fakeStore{}, Options{}).backendTimeout; got != DefaultBackendTimeout {
		t.Errorf("zero Options.BackendTimeout resolved to %v, want %v", got, DefaultBackendTimeout)
	}
	if got := New(fakeStore{}, Options{BackendTimeout: time.Second}).backendTimeout; got != time.Second {
		t.Errorf("explicit BackendTimeout resolved to %v, want 1s", got)
	}
	if got := New(fakeStore{}, Options{BackendTimeout: -1}).backendTimeout; got != 0 {
		t.Errorf("negative BackendTimeout resolved to %v, want 0 (disabled)", got)
	}
}

// A hung embedding backend must cost the query the backend budget, not the
// whole request: the FTS results come back, promptly, with no error.
func TestSearch_HungEmbedder_DegradesToFTSWithinBudget(t *testing.T) {
	e := New(fakeStore{rows: rows("a", "b", "c")}, Options{
		Embedder: blockingEmbedder{}, Fanout: 100,
		BackendTimeout: 20 * time.Millisecond,
	})

	start := time.Now()
	got, err := e.Search(context.Background(), "anything", 0)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Search should degrade, not fail, when the embedder hangs: %v", err)
	}
	if want := []string{"a", "b", "c"}; !equal(titles(got), want) {
		t.Errorf("hung embedder should leave FTS order: got %v, want %v", titles(got), want)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Search took %v; the backend budget did not bound the embed call", elapsed)
	}
}

func TestSearch_HungReranker_KeepsFusedOrderWithinBudget(t *testing.T) {
	e := New(fakeStore{rows: rows("a", "b", "c")}, Options{
		Reranker: blockingReranker{}, Fanout: 100,
		BackendTimeout: 20 * time.Millisecond,
	})

	start := time.Now()
	got, err := e.Search(context.Background(), "anything", 0)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Search should degrade, not fail, when the reranker hangs: %v", err)
	}
	if want := []string{"a", "b", "c"}; !equal(titles(got), want) {
		t.Errorf("hung reranker should keep fused order: got %v, want %v", titles(got), want)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Search took %v; the backend budget did not bound the rerank call", elapsed)
	}
}

// The budget is per backend call, so a query that needs both an embed and a
// rerank still returns within a small multiple of it rather than their sum
// against the request.
func TestSearch_HungEmbedderAndReranker_BothBounded(t *testing.T) {
	e := New(fakeStore{rows: rows("a", "b", "c")}, Options{
		Embedder: blockingEmbedder{}, Reranker: blockingReranker{}, Fanout: 100,
		BackendTimeout: 20 * time.Millisecond,
	})

	start := time.Now()
	got, err := e.Search(context.Background(), "anything", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d results, want 3", len(got))
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Search took %v with both backends hung", elapsed)
	}
}

// A caller whose own deadline is shorter still gets it honoured, and
// disabling the budget leaves the caller's context in charge.
func TestSearch_BackendTimeoutDisabled_CallerDeadlineStillWins(t *testing.T) {
	e := New(fakeStore{rows: rows("a")}, Options{
		Embedder: blockingEmbedder{}, Fanout: 100,
		BackendTimeout: -1,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := e.Search(ctx, "anything", 0); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Search took %v; the caller's deadline was not honoured", elapsed)
	}
}
