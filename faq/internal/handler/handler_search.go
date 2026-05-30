package handler

import (
	"net/http"
	"strings"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/store"
)

// searchQuestions is the M3.7 implementation of GET /search?q=...
// Searches via the M1 search_tsv generated column (title weighted A,
// body weighted B, English dictionary). Empty queries render the empty
// state without hitting the DB. There is no Count* equivalent so paging
// is "ask for size+1; trim and surface HasMore".
func (s *Server) searchQuestions(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.resolveUser(w, r); ok {
		r = withUser(r, u)
	} else {
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, offset, page := s.pageBounds(r)
	ctx := r.Context()

	var rows []questionRow
	if query != "" {
		// Probe size+1 to learn whether a next page exists without a
		// separate COUNT(*).
		probe, err := s.deps.Store.SearchQuestions(ctx, store.SearchQuestionsParams{
			SearchQuery: query, LimitN: limit + 1, OffsetN: offset,
		})
		if err != nil {
			s.internalError(w, r, "search", err)
			return
		}
		rows = make([]questionRow, 0, len(probe))
		for _, q := range probe {
			r := questionRow{
				ID:        q.ID,
				Title:     q.Title,
				BodyMD:    q.BodyMd,
				Status:    string(q.Status),
				SourceID:  q.SourceID,
				SourceURL: q.SourceUrl,
			}
			if q.CreatedAt.Valid {
				r.CreatedAt = q.CreatedAt.Time
			}
			rows = append(rows, r)
		}
	}

	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}

	cards, err := s.buildCards(ctx, rows)
	if err != nil {
		s.internalError(w, r, "build cards", err)
		return
	}

	view := SearchView{
		Query:    query,
		Results:  cards,
		Page:     page,
		PageSize: int(limit),
		Total:    int(offset) + len(cards), // best-effort lower bound
		HasMore:  hasMore,
	}
	base := s.baseView(r, view)
	if query == "" {
		base.PageTitle = "Search"
	} else {
		base.PageTitle = "Search: " + query
	}

	if uiassets.IsRequest(r) {
		s.deps.Templates.Render(w, "partials/search_body.gohtml", view)
		return
	}
	s.deps.Templates.Render(w, "pages/search.gohtml", base)
}
