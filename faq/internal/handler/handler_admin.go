package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/store"
)

// M5 admin handlers. All gated by requireAdmin (anon → 401, authed
// non-admin → 403). The host is expected to either mount the module
// under a path it already restricts to admins, or leave the gate to
// requireAdmin itself; either is sufficient.

// adminReviewQueue handles GET /admin/review. Lists questions where
// the question OR any of its answers is flagged needs_review. Optional
// ?source=<id> filter narrows by faq_sources.id.
func (s *Server) adminReviewQueue(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)

	var sourceID int64
	if raw := r.URL.Query().Get("source"); raw != "" {
		// Strict parse; "all" / "" / garbage all collapse to 0 (no filter).
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
			sourceID = n
		}
	}

	limit, offset, page := s.pageBounds(r)
	ctx := r.Context()

	questions, err := s.deps.Store.ListNeedsReviewQuestions(ctx, store.ListNeedsReviewQuestionsParams{
		SourceID: sourceID, LimitN: limit, OffsetN: offset,
	})
	if err != nil {
		s.internalError(w, r, "list needs-review", err)
		return
	}
	total, err := s.deps.Store.CountNeedsReviewQuestions(ctx, sourceID)
	if err != nil {
		s.internalError(w, r, "count needs-review", err)
		return
	}

	rows := make([]questionRow, len(questions))
	for i, q := range questions {
		rows[i] = fromFaqQuestion(q)
	}
	cards, err := s.buildCards(ctx, rows)
	if err != nil {
		s.internalError(w, r, "build cards", err)
		return
	}

	sources, err := s.deps.Store.ListSources(ctx)
	if err != nil {
		s.internalError(w, r, "list sources", err)
		return
	}
	sourceOpts := make([]AdminSourceOption, 0, len(sources))
	for _, src := range sources {
		sourceOpts = append(sourceOpts, AdminSourceOption{
			ID:          src.ID,
			DisplayName: src.DisplayName,
			Selected:    src.ID == sourceID,
		})
	}

	view := AdminReviewView{
		Questions:      cards,
		Sources:        sourceOpts,
		SelectedSource: sourceID,
		Page:           page,
		PageSize:       int(limit),
		Total:          int(total),
		HasMore:        int64(offset)+int64(len(cards)) < total,
	}
	base := s.baseView(r, view)
	base.PageTitle = "Review queue"

	if uiassets.IsRequest(r) {
		s.deps.Templates.Render(w, "partials/admin_review_body.gohtml", view)
		return
	}
	s.deps.Templates.Render(w, "pages/admin_review.gohtml", base)
}

// adminMarkReviewed handles POST /admin/review/{id}/reviewed. Clears
// needs_review on the question AND every one of its answers (per the
// M5 decision that reviewing a question counts as reviewing its
// answers). After clearing, re-renders the queue body so htmx swaps
// the row out.
func (s *Server) adminMarkReviewed(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	qid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	if _, err := s.deps.Store.GetQuestionIncludingDeleted(ctx, qid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get question", err)
		return
	}
	if err := s.deps.Store.ClearNeedsReviewCascade(ctx, qid); err != nil {
		s.internalError(w, r, "clear needs_review cascade", err)
		return
	}
	if err := parseForm(r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// Preserve the active source filter so the admin stays on the same
	// queue page after the row drops out. htmx posts it via hx-vals on
	// the mark-reviewed button.
	r2 := r.Clone(ctx)
	if raw := postFormValue(r, "source"); raw != "" {
		r2.URL.RawQuery = "source=" + raw
	} else {
		r2.URL.RawQuery = ""
	}
	s.adminReviewQueue(w, r2)
}

// adminDeletedQueue handles GET /admin/deleted. Lists rows where
// status = 'deleted' so the admin can restore them.
func (s *Server) adminDeletedQueue(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)

	limit, offset, page := s.pageBounds(r)
	ctx := r.Context()

	questions, err := s.deps.Store.ListDeletedQuestions(ctx, store.ListDeletedQuestionsParams{
		LimitN: limit, OffsetN: offset,
	})
	if err != nil {
		s.internalError(w, r, "list deleted", err)
		return
	}
	total, err := s.deps.Store.CountDeletedQuestions(ctx)
	if err != nil {
		s.internalError(w, r, "count deleted", err)
		return
	}

	rows := make([]questionRow, len(questions))
	for i, q := range questions {
		rows[i] = fromFaqQuestion(q)
	}
	cards, err := s.buildCards(ctx, rows)
	if err != nil {
		s.internalError(w, r, "build cards", err)
		return
	}

	view := AdminDeletedView{
		Questions: cards,
		Page:      page,
		PageSize:  int(limit),
		Total:     int(total),
		HasMore:   int64(offset)+int64(len(cards)) < total,
	}
	base := s.baseView(r, view)
	base.PageTitle = "Deleted questions"

	if uiassets.IsRequest(r) {
		s.deps.Templates.Render(w, "partials/admin_deleted_body.gohtml", view)
		return
	}
	s.deps.Templates.Render(w, "pages/admin_deleted.gohtml", base)
}
