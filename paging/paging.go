// Package paging provides offset-based pagination primitives shared across the
// infodancer/web components and the sites that host them. It is the data-layer
// companion to the presentation-layer infodancer/ui Pager: [Paginate] decides
// "is there a next page" without a COUNT(*), and its [Page] result feeds
// ui.NewPager to render the prev/next nav.
//
// The helper is storage-agnostic. Paginate takes a fetch closure rather than a
// query handle, so it works with any source that can honor a limit and offset
// -- a pgx/sqlc query, database/sql, or an in-memory slice -- and the package
// pulls in no database driver. The host writes its own typed list query; this
// package owns only the over-fetch math and the request-parsing clamp.
package paging

import (
	"net/http"
	"strconv"
)

// Params is one page request: a zero-based Offset into a result set and the
// PageSize (rows per page). Build it from a fixed per-page constant plus the
// request's ?offset=, or with [ParseParams].
type Params struct {
	Offset   int
	PageSize int
}

// Page is one page of results plus the prev/next signal a pager needs. HasNext
// is detected without a COUNT(*): [Paginate] over-fetches one row and reports
// whether it came back. HasPrev is simply Offset > 0.
type Page[T any] struct {
	Items    []T
	Offset   int
	PageSize int
	HasPrev  bool
	HasNext  bool
}

// Paginate runs fetch once with limit = PageSize+1, trims the sentinel row, and
// sets HasNext from whether it was present -- so prev/next paging costs a single
// query, not a page query plus a COUNT(*). The caller's own typed list query is
// the fetcher; Paginate owns only the over-fetch math.
//
//	page, err := paging.Paginate(pp, func(limit, offset int) ([]*Creature, error) {
//	    p := listParams; p.Limit, p.Offset = limit, offset
//	    return store.ListCreatures(ctx, p)
//	})
//	pager := ui.NewPager("/bestiary/", q, page.Offset, page.PageSize, len(page.Items), page.HasNext)
//
// A PageSize < 1 is treated as 1 and a negative Offset as 0, so a malformed
// request can't ask for a zero-or-negative limit. Numbered "page X of Y" UI,
// which does need a total count, is a separate opt-in concern and not built here.
func Paginate[T any](p Params, fetch func(limit, offset int) ([]T, error)) (Page[T], error) {
	size := p.PageSize
	if size < 1 {
		size = 1
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}
	items, err := fetch(size+1, offset)
	if err != nil {
		return Page[T]{}, err
	}
	page := Page[T]{Offset: offset, PageSize: size, HasPrev: offset > 0}
	if len(items) > size {
		page.HasNext = true
		items = items[:size]
	}
	page.Items = items
	return page, nil
}

// ParseParams reads ?offset= and ?size= from the request and clamps them into a
// safe [Params]: Offset is non-negative; PageSize defaults to defaultSize, and a
// user-supplied ?size= is honored but bounded to [1, maxSize] (maxSize <= 0
// means no upper bound). It is the one place a handler parses paging input, for
// pages that let the reader choose a page size.
//
// A page whose size is fixed (not user-selectable) should skip this helper and
// build [Params] directly from its own constant plus the parsed ?offset= -- that
// way no ?size= can change it at all.
func ParseParams(r *http.Request, defaultSize, maxSize int) Params {
	q := r.URL.Query()

	offset := 0
	if n, err := strconv.Atoi(q.Get("offset")); err == nil && n > 0 {
		offset = n
	}

	size := defaultSize
	if n, err := strconv.Atoi(q.Get("size")); err == nil && n > 0 {
		size = n
	}
	if size < 1 {
		size = 1
	}
	if maxSize > 0 && size > maxSize {
		size = maxSize
	}

	return Params{Offset: offset, PageSize: size}
}
