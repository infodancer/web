package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/infodancer/web/faq/store"
)

// accept handles POST /q/{id}/accept with answer_id=<aid>. Sets (or
// clears, on toggle) the accepted answer for the question. Gate:
//
//   - Admin: always accepts on any status.
//   - Author on open: accepts on their own question.
//   - Locked question + non-admin: rejected (imported content is
//     admin-only per the M4 decision).
//
// Toggling: posting the answer_id that's already accepted clears the
// accepted_answer_id (set to NULL).
func (s *Server) accept(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	qid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := parseForm(r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	aidRaw := postFormValue(r, "answer_id")
	aid, err := strconv.ParseInt(aidRaw, 10, 64)
	if err != nil || aid <= 0 {
		http.Error(w, "answer_id required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	q, err := s.deps.Store.GetQuestion(ctx, qid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get question", err)
		return
	}
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}
	if !s.canAccept(u, local.ID, q) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Confirm the answer belongs to this question — otherwise an
	// attacker could accept any answer's id on any question they
	// control.
	ans, err := s.deps.Store.GetAnswer(ctx, aid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "answer not found", http.StatusNotFound)
			return
		}
		s.internalError(w, r, "get answer", err)
		return
	}
	if ans.QuestionID != qid {
		http.Error(w, "answer does not belong to this question", http.StatusBadRequest)
		return
	}

	// Toggle semantics: re-accepting the same answer clears the bit.
	var newAccepted *int64
	if q.AcceptedAnswerID == nil || *q.AcceptedAnswerID != aid {
		newAccepted = &aid
	}
	if err := s.deps.Store.SetAcceptedAnswer(ctx, store.SetAcceptedAnswerParams{
		ID: qid, AcceptedAnswerID: newAccepted,
	}); err != nil {
		s.internalError(w, r, "set accepted", err)
		return
	}

	view, err := s.loadDetail(ctx, qid, u, local.ID)
	if err != nil {
		s.internalError(w, r, "load after accept", err)
		return
	}
	s.deps.Templates.Render(w, "partials/q_detail_body.gohtml", view)
}
