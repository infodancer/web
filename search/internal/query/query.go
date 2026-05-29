// Package query implements the read path (DESIGN §6): the full-text-search arm,
// the vector arm, reciprocal-rank fusion of the two, and the optional rerank
// stage, in the order retrieve → RRF fuse → rerank top-N → limit. An
// FTS-only deployment (no embedder) simply skips the vector arm; RRF over the
// single arm reproduces the FTS order.
package query

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"sort"
	"strings"

	embedding "github.com/matthewjhunter/go-embedding"

	"github.com/infodancer/web/search/store"
)

// DocStore is the slice of the data layer the engine needs. *store.Store
// satisfies it; tests supply a fake.
type DocStore interface {
	SearchDocumentsFTS(ctx context.Context, arg store.SearchDocumentsFTSParams) ([]store.SearchDocumentsFTSRow, error)
	SearchChunksVector(ctx context.Context, vec []float32, metric string, fanout int32) ([]store.VectorHit, error)
	GetDocumentsByIDs(ctx context.Context, ids []int64) ([]store.GetDocumentsByIDsRow, error)
}

// Result is one ranked search hit, ready to render. Snippet is already-safe
// HTML: the document text has been HTML-escaped and only our own <mark> tags
// reintroduced (DESIGN §10 — we never emit stored HTML).
type Result struct {
	DocID   int64
	URL     string
	Title   string
	Section string
	Tags    []string
	Summary string
	Snippet template.HTML
	Score   float64
}

// Options configures an Engine. Embedder, QueryCache, and Metric enable the
// vector arm; leaving Embedder nil keeps the engine FTS-only. Reranker enables
// the rerank stage.
type Options struct {
	Reranker   embedding.Reranker
	Embedder   embedding.Embedder
	QueryCache *embedding.QueryCache
	Metric     string
	Fanout     int
	RRFk       int
	RerankTopN int
	Logger     *slog.Logger
}

// Engine runs queries against the corpus. Construct with [New]; safe for
// concurrent use (it holds no per-query state).
type Engine struct {
	store      DocStore
	reranker   embedding.Reranker
	embedder   embedding.Embedder
	queryCache *embedding.QueryCache
	metric     string
	fanout     int
	rrfK       int
	rerankTopN int
	log        *slog.Logger
}

// New builds an Engine over s with opts. Defaults: fanout 100, RRF k 60, rerank
// top-N unbounded, logger slog.Default, metric "cosine".
func New(s DocStore, opts Options) *Engine {
	e := &Engine{
		store:      s,
		reranker:   opts.Reranker,
		embedder:   opts.Embedder,
		queryCache: opts.QueryCache,
		metric:     opts.Metric,
		fanout:     opts.Fanout,
		rrfK:       opts.RRFk,
		rerankTopN: opts.RerankTopN,
		log:        opts.Logger,
	}
	if e.log == nil {
		e.log = slog.Default()
	}
	if e.fanout <= 0 {
		e.fanout = 100
	}
	if e.rrfK <= 0 {
		e.rrfK = 60
	}
	if e.metric == "" {
		e.metric = "cosine"
	}
	return e
}

// Search runs the query and returns up to limit ranked results. A blank query
// returns no results and no error. limit <= 0 means "no cap beyond fanout".
func (e *Engine) Search(ctx context.Context, q string, limit int) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}

	ftsRows, err := e.store.SearchDocumentsFTS(ctx, store.SearchDocumentsFTSParams{
		Query:  q,
		Fanout: int32(e.fanout), //nolint:gosec // fanout is operator-bounded, small
	})
	if err != nil {
		return nil, fmt.Errorf("query: fts: %w", err)
	}

	vecHits := e.vectorArm(ctx, q)

	// Reciprocal-rank fusion over the two arms' positions (DESIGN §6.3).
	ftsIDs := make([]int64, len(ftsRows))
	for i, r := range ftsRows {
		ftsIDs[i] = r.ID
	}
	vecIDs := make([]int64, len(vecHits))
	for i, h := range vecHits {
		vecIDs[i] = h.DocID
	}
	order, scores := rrf(e.rrfK, ftsIDs, vecIDs)

	results, bodies, err := e.hydrate(ctx, order, scores, ftsRows)
	if err != nil {
		return nil, err
	}

	results, err = e.applyRerank(ctx, q, results, bodies)
	if err != nil {
		return nil, err
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// vectorArm embeds the query and retrieves the best chunk per document. It
// returns nil (degrading to FTS-only) when no embedder is configured or when the
// embedder is unavailable — a query must not fail because the embedding backend
// is down.
func (e *Engine) vectorArm(ctx context.Context, q string) []store.VectorHit {
	if e.embedder == nil {
		return nil
	}
	formatted := embedding.FormatForTask(e.embedder.Model(), embedding.TaskRetrievalQuery, q)
	vec, err := e.queryCache.Single(ctx, e.embedder, formatted)
	if err != nil {
		e.log.Warn("query: embedding query failed; using FTS only", "err", err)
		return nil
	}
	hits, err := e.store.SearchChunksVector(ctx, vec, e.metric, int32(e.fanout)) //nolint:gosec // operator-bounded
	if err != nil {
		e.log.Warn("query: vector arm failed; using FTS only", "err", err)
		return nil
	}
	return hits
}

// hydrate builds display results in fused order. Documents that came from the
// FTS arm carry a highlighted headline; documents that surfaced only via the
// vector arm are fetched by id and fall back to their stored snippet. bodies is
// the parallel rerank text for each result.
func (e *Engine) hydrate(ctx context.Context, order []int64, scores map[int64]float64, ftsRows []store.SearchDocumentsFTSRow) ([]Result, []string, error) {
	ftsByID := make(map[int64]store.SearchDocumentsFTSRow, len(ftsRows))
	for _, r := range ftsRows {
		ftsByID[r.ID] = r
	}

	var missing []int64
	for _, id := range order {
		if _, ok := ftsByID[id]; !ok {
			missing = append(missing, id)
		}
	}
	hydrated := make(map[int64]store.GetDocumentsByIDsRow, len(missing))
	if len(missing) > 0 {
		rows, err := e.store.GetDocumentsByIDs(ctx, missing)
		if err != nil {
			return nil, nil, fmt.Errorf("query: hydrate: %w", err)
		}
		for _, r := range rows {
			hydrated[r.ID] = r
		}
	}

	results := make([]Result, 0, len(order))
	bodies := make([]string, 0, len(order))
	for _, id := range order {
		if r, ok := ftsByID[id]; ok {
			results = append(results, Result{
				DocID:   r.ID,
				URL:     r.Url,
				Title:   r.Title,
				Section: r.Section,
				Tags:    r.Tags,
				Summary: r.Summary,
				Snippet: highlight(r.Headline, r.Snippet),
				Score:   scores[id],
			})
			bodies = append(bodies, rerankDoc(r.Title, r.Summary, r.BodyText))
			continue
		}
		r, ok := hydrated[id]
		if !ok {
			// Soft-deleted between retrieval and hydration; skip it.
			continue
		}
		results = append(results, Result{
			DocID:   r.ID,
			URL:     r.Url,
			Title:   r.Title,
			Section: r.Section,
			Tags:    r.Tags,
			Summary: r.Summary,
			Snippet: highlight(nil, r.Snippet),
			Score:   scores[id],
		})
		bodies = append(bodies, rerankDoc(r.Title, r.Summary, r.BodyText))
	}
	return results, bodies, nil
}

// rrf fuses the per-arm rank positions into a combined order. Each arm
// contributes 1/(k+rank) at its 1-based rank; a document missing from an arm
// contributes nothing. Ties break by document id for a stable order.
func rrf(k int, arms ...[]int64) ([]int64, map[int64]float64) {
	score := make(map[int64]float64)
	for _, arm := range arms {
		for rank, id := range arm {
			score[id] += 1.0 / float64(k+rank+1)
		}
	}
	order := make([]int64, 0, len(score))
	for id := range score {
		order = append(order, id)
	}
	sort.Slice(order, func(i, j int) bool {
		if score[order[i]] != score[order[j]] {
			return score[order[i]] > score[order[j]]
		}
		return order[i] < order[j]
	})
	return order, score
}

// applyRerank reorders the top-N fused candidates with the reranker (DESIGN
// §6.0). The tail beyond N keeps its fused order. With no reranker, or when the
// backend is unavailable, the fused order stands.
func (e *Engine) applyRerank(ctx context.Context, q string, results []Result, bodies []string) ([]Result, error) {
	if e.reranker == nil || len(results) < 2 {
		return results, nil
	}
	n := e.rerankTopN
	if n <= 0 || n > len(results) {
		n = len(results)
	}

	order, err := e.rerankOrder(ctx, q, bodies[:n])
	if err != nil {
		return nil, err
	}
	if order == nil {
		return results, nil
	}

	reordered := make([]Result, 0, len(results))
	for _, idx := range order {
		reordered = append(reordered, results[idx])
	}
	reordered = append(reordered, results[n:]...)
	return reordered, nil
}

// rerankOrder asks the reranker to rescore the candidate documents and returns
// the resulting index order, or nil to mean "keep the fused order".
//
// It returns nil (not an error) when there is nothing worth reordering or when
// the backend is unavailable — the documented degrade-to-fused-order signal
// (embedding.IsRerankAvailable reports false). A still-available error (e.g. a
// 4xx misconfiguration, surfaced as *PermanentError) is a caller bug and is
// propagated rather than swallowed.
func (e *Engine) rerankOrder(ctx context.Context, q string, docs []string) ([]int, error) {
	if len(docs) < 2 {
		return nil, nil
	}

	res, err := e.reranker.Rerank(ctx, embedding.RerankRequest{Query: q, Documents: docs})
	if err != nil {
		if !embedding.IsRerankAvailable(err) {
			e.log.Warn("rerank backend unavailable; keeping fused order", "err", err)
			return nil, nil
		}
		return nil, fmt.Errorf("query: rerank: %w", err)
	}

	order := make([]int, 0, len(res))
	for _, r := range res {
		if r.Index >= 0 && r.Index < len(docs) {
			order = append(order, r.Index)
		}
	}
	if len(order) == 0 {
		return nil, nil
	}
	return order, nil
}

// rerankDoc builds the text a reranker scores for a candidate. Title plus the
// plain body text is enough signal for a cross-encoder; the reranker truncates
// to its own sequence budget.
func rerankDoc(title, summary, body string) string {
	var b strings.Builder
	if title != "" {
		b.WriteString(title)
		b.WriteByte('\n')
	}
	if summary != "" {
		b.WriteString(summary)
		b.WriteByte('\n')
	}
	b.WriteString(body)
	return b.String()
}

// highlight turns a ts_headline result (sentinel-delimited) into safe HTML. The
// whole string is HTML-escaped first, so any markup-significant characters in
// the document text become entities; only then are the escaped sentinels
// swapped for real <mark> tags. The result therefore contains exactly the
// highlight markup we control and nothing else.
func highlight(headline []byte, fallback string) template.HTML {
	h := string(headline)
	if strings.TrimSpace(h) == "" {
		h = fallback
	}
	esc := template.HTMLEscapeString(h)
	esc = strings.ReplaceAll(esc, markStart, "<mark>")
	esc = strings.ReplaceAll(esc, markEnd, "</mark>")
	return template.HTML(esc) //nolint:gosec // text escaped above; only our own <mark> reintroduced
}

// Sentinel tokens injected by ts_headline (see store/queries.sql). They survive
// HTML escaping unchanged, which is what lets highlight() escape first and
// substitute markup second.
const (
	markStart = "ZZMARKSTARTZZ"
	markEnd   = "ZZMARKENDZZ"
)
