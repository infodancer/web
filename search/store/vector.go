package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// VectorHit is one document's best (minimum) distance to the query vector.
type VectorHit struct {
	DocID    int64
	Distance float64
}

// vectorOp maps a distance metric to its pgvector distance operator. The metric
// is operator configuration, never user input, so interpolating the operator
// into the statement is safe.
var vectorOp = map[string]string{
	"cosine": "<=>",
	"ip":     "<#>",
	"l2":     "<->",
}

// SearchChunksVector runs the vector arm (DESIGN §6.2): the best chunk per live
// document, ranked by distance to the query vector. The query vector is sent as
// a pgvector text literal cast to ::vector. When no documents have been embedded
// yet the chunk table does not exist; that is reported as no results rather than
// an error, so a query against an as-yet-unembedded corpus simply falls back to
// the FTS arm.
func (s *Store) SearchChunksVector(ctx context.Context, vec []float32, metric string, fanout int32) ([]VectorHit, error) {
	op, ok := vectorOp[metric]
	if !ok {
		return nil, fmt.Errorf("store: unknown embed metric %q", metric)
	}
	q := fmt.Sprintf(`
		SELECT c.document_id, min(c.embedding %s $1::vector) AS dist
		FROM search_chunks c
		JOIN search_documents d ON d.id = c.document_id
		WHERE d.deleted_at IS NULL
		GROUP BY c.document_id
		ORDER BY dist
		LIMIT $2`, op)

	rows, err := s.pool.Query(ctx, q, VectorLiteral(vec), fanout)
	if err != nil {
		if undefinedTable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: vector search: %w", err)
	}
	defer rows.Close()

	var hits []VectorHit
	for rows.Next() {
		var h VectorHit
		if err := rows.Scan(&h.DocID, &h.Distance); err != nil {
			return nil, fmt.Errorf("store: scan vector hit: %w", err)
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		if undefinedTable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: vector rows: %w", err)
	}
	return hits, nil
}

// IsUndefinedTable reports whether err is the Postgres "relation does not exist"
// error (42P01). That is the expected condition when search_chunks has not been
// created yet because no document has ever been embedded.
func IsUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}

// undefinedTable is the package-internal alias kept for the existing callers in
// this file.
func undefinedTable(err error) bool { return IsUndefinedTable(err) }

// VectorLiteral renders a float32 slice as a pgvector text literal "[a,b,c]" for
// binding as a ::vector parameter.
func VectorLiteral(v []float32) string {
	var b strings.Builder
	b.Grow(len(v)*8 + 2)
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
