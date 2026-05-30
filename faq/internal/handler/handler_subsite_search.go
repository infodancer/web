package handler

import (
	"net/http"
	"strings"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/store"
)

// subsiteSearch renders /{subsite}/search — the subsite-intersected
// search. Same probe-size+1 / HasMore pattern as the global search;
// the only difference is that every result is filtered through the
// subsite membership predicate at the SQL layer.
func (s *Server) subsiteSearch(w http.ResponseWriter, r *http.Request) {
	sub, ok := subsiteFromContext(r.Context())
	if !ok {
		s.notFound(w, r)
		return
	}
	if u, ok := s.resolveUser(w, r); ok {
		r = withUser(r, u)
	} else {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, offset, page := s.pageBounds(r)
	ctx := r.Context()
	subPrefix := s.deps.BasePath + "/" + sub.Slug

	var rows []questionRow
	if query != "" {
		probe, err := s.deps.Store.SearchSubsiteQuestions(ctx, store.SearchSubsiteQuestionsParams{
			SubsiteID:   &sub.ID,
			SearchQuery: query,
			LimitN:      limit + 1,
			OffsetN:     offset,
		})
		if err != nil {
			s.internalError(w, r, "subsite search", err)
			return
		}
		rows = make([]questionRow, 0, len(probe))
		for _, q := range probe {
			row := questionRow{
				ID:        q.ID,
				Title:     q.Title,
				BodyMD:    q.BodyMd,
				Status:    string(q.Status),
				SourceID:  q.SourceID,
				SourceURL: q.SourceUrl,
			}
			if q.CreatedAt.Valid {
				row.CreatedAt = q.CreatedAt.Time
			}
			rows = append(rows, row)
		}
	}

	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}

	cards, err := s.buildCardsForPrefix(ctx, rows, subPrefix)
	if err != nil {
		s.internalError(w, r, "subsite search: cards", err)
		return
	}

	view := SearchView{
		Query:    query,
		Results:  cards,
		Page:     page,
		PageSize: int(limit),
		Total:    int(offset) + len(cards),
		HasMore:  hasMore,
	}
	base := s.baseView(r, view)
	if query == "" {
		base.PageTitle = sub.DisplayName + " — Search"
	} else {
		base.PageTitle = sub.DisplayName + " — Search: " + query
	}
	if uiassets.IsRequest(r) {
		s.deps.Templates.Render(w, "partials/search_body.gohtml", view)
		return
	}
	s.deps.Templates.Render(w, "pages/search.gohtml", base)
}
