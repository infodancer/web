package handler

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/infodancer/web/faq/store"
)

// deleteAnswer handles POST /a/{id}/delete — soft-deletes an answer. The author
// may delete their own while the question is open; an admin may delete any. If
// the answer was the accepted one, the question's accepted_answer_id is cleared.
// Re-renders the detail body so htmx swaps the answer out of the list.
func (s *Server) deleteAnswer(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	aid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}
	ans, err := s.deps.Store.GetAnswer(ctx, aid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get answer", err)
		return
	}
	q, err := s.deps.Store.GetQuestion(ctx, ans.QuestionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get question", err)
		return
	}
	if !s.canDeleteAnswer(u, local.ID, ans.AuthorUserID, q.Status == store.FaqQuestionStatusOpen) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if _, err := s.deps.Store.SoftDeleteAnswer(ctx, aid); err != nil {
		s.internalError(w, r, "delete answer", err)
		return
	}
	// A deleted answer can no longer be the accepted one.
	if q.AcceptedAnswerID != nil && *q.AcceptedAnswerID == aid {
		if err := s.deps.Store.SetAcceptedAnswer(ctx, store.SetAcceptedAnswerParams{ID: q.ID}); err != nil {
			s.internalError(w, r, "clear accepted answer", err)
			return
		}
	}
	s.renderDetailBody(w, r, ans.QuestionID, u, local.ID)
}

// restoreAnswer handles POST /a/{id}/restore — admins un-delete a soft-deleted
// answer, which reappears in the list for everyone.
func (s *Server) restoreAnswer(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	aid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	ans, err := s.deps.Store.GetAnswer(ctx, aid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get answer", err)
		return
	}
	if _, err := s.deps.Store.RestoreAnswer(ctx, aid); err != nil {
		s.internalError(w, r, "restore answer", err)
		return
	}
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}
	s.renderDetailBody(w, r, ans.QuestionID, u, local.ID)
}
