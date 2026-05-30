package handler

import (
	"context"
	"errors"
	"net/http"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/store"
)

// Supported sort= values on /q. Default is "newest".
const (
	sortNewest     = "newest"
	sortVotes      = "votes"
	sortUnanswered = "unanswered"
)

// listQuestions is the M3.4 implementation of GET /q. Sort variants are
// "newest" (default), "votes", and "unanswered". HTMX requests get the
// list body partial; everything else gets the full page.
func (s *Server) listQuestions(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.resolveUser(w, r); ok {
		r = withUser(r, u)
	} else {
		return
	}

	sort := r.URL.Query().Get("sort")
	if sort == "" {
		sort = sortNewest
	}

	limit, offset, page := s.pageBounds(r)
	ctx := r.Context()

	rows, err := s.fetchList(ctx, sort, limit, offset)
	if err != nil {
		if errors.Is(err, ErrUnknownSort) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "list questions", err)
		return
	}

	total, err := s.deps.Store.CountQuestions(ctx)
	if err != nil {
		s.internalError(w, r, "count questions", err)
		return
	}

	cards, err := s.buildCards(ctx, rows)
	if err != nil {
		s.internalError(w, r, "build cards", err)
		return
	}

	view := QuestionListView{
		Title:     listTitle(sort),
		Sort:      sort,
		Questions: cards,
		Page:      page,
		PageSize:  int(limit),
		Total:     int(total),
		HasMore:   int64(offset)+int64(len(cards)) < total,
	}

	base := s.baseView(r, view)
	base.PageTitle = view.Title

	if uiassets.IsRequest(r) {
		s.deps.Templates.Render(w, "partials/q_list_body.gohtml", view)
		return
	}
	s.deps.Templates.Render(w, "pages/q_list.gohtml", base)
}

// fetchList runs the right sort variant and returns rows in the
// projected questionRow shape. Returns [ErrUnknownSort] for unknown sort
// values so the handler can 404 cleanly.
func (s *Server) fetchList(ctx context.Context, sort string, limit, offset int32) ([]questionRow, error) {
	switch sort {
	case sortNewest, "":
		qs, err := s.deps.Store.ListQuestionsNewest(ctx, store.ListQuestionsNewestParams{
			LimitN: limit, OffsetN: offset,
		})
		if err != nil {
			return nil, err
		}
		out := make([]questionRow, len(qs))
		for i, q := range qs {
			out[i] = fromFaqQuestion(q)
		}
		return out, nil

	case sortVotes:
		qs, err := s.deps.Store.ListQuestionsByVotes(ctx, store.ListQuestionsByVotesParams{
			LimitN: limit, OffsetN: offset,
		})
		if err != nil {
			return nil, err
		}
		out := make([]questionRow, len(qs))
		for i, q := range qs {
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
			out[i] = r
		}
		return out, nil

	case sortUnanswered:
		qs, err := s.deps.Store.ListQuestionsUnanswered(ctx, store.ListQuestionsUnansweredParams{
			LimitN: limit, OffsetN: offset,
		})
		if err != nil {
			return nil, err
		}
		out := make([]questionRow, len(qs))
		for i, q := range qs {
			out[i] = fromFaqQuestion(q)
		}
		return out, nil

	default:
		return nil, ErrUnknownSort
	}
}

// listTitle returns the page <title>-friendly label for a sort variant.
func listTitle(sort string) string {
	switch sort {
	case sortVotes:
		return "Top questions"
	case sortUnanswered:
		return "Unanswered questions"
	default:
		return "All questions"
	}
}
