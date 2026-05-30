package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/infodancer/web/faq/store"
)

// vote handles POST /a/{id}/vote with value=up|down. Upserts the
// (user, answer) row in faq_votes; the M1 trigger maintains the answer
// score atomically. Trigger errors (self-vote, locked question) are
// mapped to 403.
//
// Empty value POSTed via DELETE-style intent is handled by the
// dedicated unvote handler below.
func (s *Server) vote(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	aid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := parseForm(r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	rawValue := strings.ToLower(strings.TrimSpace(postFormValue(r, "value")))
	var voteValue store.FaqVoteValue
	switch rawValue {
	case "up":
		voteValue = store.FaqVoteValueUp
	case "down":
		voteValue = store.FaqVoteValueDown
	default:
		http.Error(w, "vote value must be up or down", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}

	if _, err := s.deps.Store.UpsertVote(ctx, store.UpsertVoteParams{
		AnswerID: aid, UserID: local.ID, Value: voteValue,
	}); err != nil {
		if mapped, ok := mapVoteTriggerError(err); ok {
			http.Error(w, mapped, http.StatusForbidden)
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "upsert vote", err)
		return
	}

	s.renderVoteFragment(w, r, aid, local.ID)
}

// unvote handles DELETE /a/{id}/vote. Removes the (user, answer) row;
// the M1 trigger reverses the score delta.
func (s *Server) unvote(w http.ResponseWriter, r *http.Request) {
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
	if err := s.deps.Store.DeleteVote(ctx, store.DeleteVoteParams{
		AnswerID: aid, UserID: local.ID,
	}); err != nil {
		s.internalError(w, r, "delete vote", err)
		return
	}
	s.renderVoteFragment(w, r, aid, local.ID)
}

// renderVoteFragment writes the updated vote-button cluster (a tiny
// partial) so htmx swaps the score and the active state in place.
func (s *Server) renderVoteFragment(w http.ResponseWriter, r *http.Request, aid, userID int64) {
	ctx := r.Context()
	a, err := s.deps.Store.GetAnswer(ctx, aid)
	if err != nil {
		s.internalError(w, r, "get answer after vote", err)
		return
	}
	curr, err := s.deps.Store.GetUserVote(ctx, store.GetUserVoteParams{
		AnswerID: aid, UserID: userID,
	})
	var have string
	if err == nil {
		have = string(curr.Value)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		s.internalError(w, r, "get user vote", err)
		return
	}

	view := VoteCellView{
		AnswerID: a.ID,
		Score:    int(a.LocalScore),
		UserVote: have,
	}
	if a.ImportedScore != nil {
		view.Score = int(*a.ImportedScore) + int(a.LocalScore)
	}
	s.deps.Templates.Render(w, "partials/vote_cell.gohtml", view)
}

// mapVoteTriggerError maps the M1 vote-eligibility trigger's RAISE
// messages to a human-friendly response string. Returns (msg, true)
// when the error is a recognized trigger raise; otherwise (_, false).
func mapVoteTriggerError(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return "", false
	}
	// PL/pgSQL RAISE EXCEPTION emits SQLSTATE P0001 (raise_exception).
	if pgErr.Code != "P0001" {
		return "", false
	}
	msg := pgErr.Message
	switch {
	case strings.Contains(msg, "self-vote"):
		return "you cannot vote on your own answer", true
	case strings.Contains(msg, "non-open question"):
		return "this question is locked; votes are disabled", true
	default:
		return fmt.Sprintf("vote rejected: %s", msg), true
	}
}
