package search

import (
	"context"
	"strings"
	"testing"

	embedding "github.com/matthewjhunter/go-embedding"

	"github.com/infodancer/web/search/internal/extract"
	"github.com/infodancer/web/search/internal/index"
)

// testModel is a registered model, so limits and task prefixes resolve. The
// module bakes in nothing model-specific; this is a test fixture only.
const testModel = "nomic-embed-text"

// stubEmbedder records the texts it was asked to embed and returns a vector per
// input. It never dials anything.
type stubEmbedder struct {
	model   string
	batches [][]string
}

func (s *stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	s.batches = append(s.batches, append([]string(nil), texts...))
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(len(texts[i]))}
	}
	return out, nil
}

func (s *stubEmbedder) Model() string { return s.model }

func (s *stubEmbedder) Fingerprint() embedding.Fingerprint {
	return embedding.Fingerprint{Model: s.model, Dim: 1}
}

// seen returns every text embedded, flattened across batches.
func (s *stubEmbedder) seen() []string {
	var out []string
	for _, b := range s.batches {
		out = append(out, b...)
	}
	return out
}

func newAdapter(chunkTokens, batchSize int) (embedAdapter, *stubEmbedder) {
	stub := &stubEmbedder{model: testModel}
	return embedAdapter{emb: stub, chunkTokens: chunkTokens, batchSize: batchSize}, stub
}

// The rendered chunk -- header included -- must fit the budget. Sizing the body
// at the full budget and then prepending a task prefix, a title, and a section
// field puts every request over it, which is the mistake the header charge
// exists to prevent.
func TestSplitSections_RenderedChunkFitsTheBudget(t *testing.T) {
	// Large enough that a realistic header still leaves a usable body budget;
	// see minBodyBytes.
	const chunkTokens = 256
	a, _ := newAdapter(chunkTokens, 25)

	budget := embedding.BudgetForTokens(testModel, chunkTokens)
	if budget <= 0 {
		t.Fatalf("BudgetForTokens(%q, %d) = %d, want a positive budget", testModel, chunkTokens, budget)
	}

	title := "How AI Will Change Cyber Defense"
	sections := []extract.Section{{
		HeadingPath: "Deployment > Rollback > Manual steps",
		Text:        strings.Repeat("alpha body text that runs on and on. ", 40),
	}}

	pieces := a.SplitSections(title, sections)
	if len(pieces) < 2 {
		t.Fatalf("want the long section split, got %d piece(s)", len(pieces))
	}
	for i, p := range pieces {
		rendered := renderDocument(testModel, title, p.HeadingPath, p.Text)
		if len(rendered) > budget {
			t.Errorf("piece %d renders to %d bytes, over the %d-byte budget", i, len(rendered), budget)
		}
	}
}

// Every piece of a section keeps that section's heading path: it is what makes
// an isolated chunk searchable, and it is also the snippet breadcrumb.
func TestSplitSections_PiecesKeepTheHeadingPath(t *testing.T) {
	a, _ := newAdapter(64, 25)
	const path = "Combat > Initiative"

	pieces := a.SplitSections("T", []extract.Section{{
		HeadingPath: path,
		Text:        strings.Repeat("initiative is rolled once per round. ", 40),
	}})
	if len(pieces) < 2 {
		t.Fatalf("want several pieces, got %d", len(pieces))
	}
	for i, p := range pieces {
		if p.HeadingPath != path {
			t.Errorf("piece %d path = %q, want %q", i, p.HeadingPath, path)
		}
	}
}

// A section that already fits is not split. The common case must stay one
// chunk per section (DESIGN §5.2).
func TestSplitSections_ShortSectionStaysWhole(t *testing.T) {
	a, _ := newAdapter(512, 25)

	pieces := a.SplitSections("T", []extract.Section{
		{HeadingPath: "Alpha", Text: "Alpha body."},
		{HeadingPath: "Beta", Text: "Beta body."},
	})
	if len(pieces) != 2 {
		t.Fatalf("want one piece per short section, got %d", len(pieces))
	}
	if pieces[0].Text != "Alpha body." || pieces[1].Text != "Beta body." {
		t.Errorf("section text was altered: %q, %q", pieces[0].Text, pieces[1].Text)
	}
}

// A model with no registered ceiling still splits to the configured token
// target. The target is the operator's choice about retrieval quality, not a
// property of the model, so an unrecognised name is no reason to abandon it --
// and a long document left whole is the failure this work exists to remove.
func TestSplitSections_UnknownModelStillSplitsToTheTarget(t *testing.T) {
	stub := &stubEmbedder{model: "no-such-model-in-any-registry"}
	a := embedAdapter{emb: stub, chunkTokens: 128, batchSize: 25}

	text := strings.Repeat("body text that keeps going. ", 200)
	pieces := a.SplitSections("T", []extract.Section{{HeadingPath: "H", Text: text}})
	if len(pieces) < 2 {
		t.Fatalf("want the section split to the configured target, got %d piece(s)", len(pieces))
	}
}

// When the header consumes the budget, the section is passed through rather
// than shredded. Hundreds of few-byte chunks retrieve nothing and cost a
// request each; one oversize chunk the backend clips and reports is a better
// failure.
func TestSplitSections_PathologicalHeaderDoesNotShred(t *testing.T) {
	a, _ := newAdapter(4, 25) // a 4-token target cannot fit any header

	text := strings.Repeat("body. ", 500)
	pieces := a.SplitSections(strings.Repeat("long title ", 20),
		[]extract.Section{{HeadingPath: strings.Repeat("deep > ", 20), Text: text}})
	if len(pieces) != 1 {
		t.Fatalf("want the section passed through whole, got %d pieces", len(pieces))
	}
}

// The token estimate must track the library's observed ratio, not the bytes/4
// guess it replaces. Checking the ratio rather than a literal keeps the test
// honest as calibration accumulates.
func TestEstimateTokens(t *testing.T) {
	if got := estimateTokens(testModel, 0); got != 0 {
		t.Errorf("estimateTokens(0 bytes) = %d, want 0", got)
	}

	const bytes = 4096
	got := estimateTokens(testModel, bytes)
	budget := embedding.BudgetForTokens(testModel, 1000)
	want := bytes * 1000 / budget
	if got != want {
		t.Errorf("estimateTokens(%d) = %d, want %d (the library's own ratio)", bytes, got, want)
	}
	// bytes/4 was the old guess; the observed ratio is denser than that, so a
	// correct estimate is higher.
	if got <= bytes/4 {
		t.Errorf("estimateTokens(%d) = %d, want more than the bytes/4 guess (%d)", bytes, got, bytes/4)
	}
}

// Document-side formatting is mandatory and asymmetric (DESIGN §5.3): every
// embedded chunk carries the retrieval-document prefix, and the heading path
// rides as a labeled field.
func TestEmbedPieces_AppliesDocumentSideFormatting(t *testing.T) {
	a, stub := newAdapter(512, 25)

	pieces := []index.Piece{{HeadingPath: "Combat > Initiative", Text: "Roll once per round."}}
	vecs, err := a.EmbedPieces(context.Background(), "Rules", pieces)
	if err != nil {
		t.Fatalf("EmbedPieces: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("want 1 vector, got %d", len(vecs))
	}

	sent := stub.seen()[0]
	if !strings.Contains(sent, "search_document:") {
		t.Errorf("embedded text is missing the retrieval-document prefix: %q", sent)
	}
	if !strings.Contains(sent, "section: Combat > Initiative") {
		t.Errorf("embedded text is missing the section field: %q", sent)
	}
	if !strings.Contains(sent, "Roll once per round.") {
		t.Errorf("embedded text is missing the body: %q", sent)
	}
}

// Pieces are batched, and the vectors come back index-aligned with them.
func TestEmbedPieces_BatchesAndStaysAligned(t *testing.T) {
	a, stub := newAdapter(512, 3)

	pieces := make([]index.Piece, 7)
	for i := range pieces {
		pieces[i] = index.Piece{HeadingPath: "H", Text: strings.Repeat("x", i+1)}
	}
	vecs, err := a.EmbedPieces(context.Background(), "T", pieces)
	if err != nil {
		t.Fatalf("EmbedPieces: %v", err)
	}
	if len(vecs) != len(pieces) {
		t.Fatalf("got %d vectors for %d pieces", len(vecs), len(pieces))
	}
	if len(stub.batches) != 3 { // 3 + 3 + 1
		t.Errorf("want 3 batches at size 3, got %d", len(stub.batches))
	}
	// The stub encodes each input's length, so alignment is checkable.
	for i, p := range pieces {
		want := float32(len(renderDocument(testModel, "T", p.HeadingPath, p.Text)))
		if vecs[i][0] != want {
			t.Errorf("vector %d = %v, want the vector for piece %d", i, vecs[i][0], i)
		}
	}
}
