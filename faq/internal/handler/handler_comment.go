package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/infodancer/web/faq/faquser"
	"github.com/infodancer/web/faq/internal/render"
	"github.com/infodancer/web/faq/store"
)

// postQuestionComment handles POST /q/{id}/comments. Creates a comment
// on the question; the M4 canComment gate rejects anon and rejects
// non-admin commenters on locked questions.
func (s *Server) postQuestionComment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	qid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	body, ok := s.parseCommentBody(w, r)
	if !ok {
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
	if !s.canComment(u, q.Status == store.FaqQuestionStatusLocked) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}
	bodyHTML := render.RenderMarkdownString(body)
	if _, err := s.deps.Store.CreateQuestionComment(ctx, store.CreateQuestionCommentParams{
		QuestionID: &qid, BodyMd: body, BodyHtml: bodyHTML, AuthorUserID: &local.ID,
	}); err != nil {
		s.internalError(w, r, "create question comment", err)
		return
	}

	s.renderAfterComment(w, r, qid, u, local.ID)
}

// postAnswerComment handles POST /a/{id}/comments. Same shape as the
// question variant; the gate checks the parent question's status.
func (s *Server) postAnswerComment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	aid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	body, ok := s.parseCommentBody(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	a, err := s.deps.Store.GetAnswer(ctx, aid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get answer", err)
		return
	}
	parent, err := s.deps.Store.GetQuestion(ctx, a.QuestionID)
	if err != nil {
		s.internalError(w, r, "get parent question", err)
		return
	}
	if !s.canComment(u, parent.Status == store.FaqQuestionStatusLocked) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}
	bodyHTML := render.RenderMarkdownString(body)
	if _, err := s.deps.Store.CreateAnswerComment(ctx, store.CreateAnswerCommentParams{
		AnswerID: &aid, BodyMd: body, BodyHtml: bodyHTML, AuthorUserID: &local.ID,
	}); err != nil {
		s.internalError(w, r, "create answer comment", err)
		return
	}

	s.renderAfterComment(w, r, a.QuestionID, u, local.ID)
}

// editComment handles PATCH /comments/{id}. Honors the same edit gate
// as questions and answers (author within grace OR admin).
func (s *Server) editComment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	cid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	body, ok := s.parseCommentBody(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	c, err := s.deps.Store.GetComment(ctx, cid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get comment", err)
		return
	}

	// Resolve the parent question id (and its lock status) before the
	// canEditAs gate so a locked parent freezes comment edits to non-
	// admins. A comment lives under exactly one of question_id /
	// answer_id (CHECK constraint at the schema level).
	var qid int64
	switch {
	case c.QuestionID != nil:
		qid = *c.QuestionID
	case c.AnswerID != nil:
		a, err := s.deps.Store.GetAnswer(ctx, *c.AnswerID)
		if err != nil {
			s.internalError(w, r, "answer for comment", err)
			return
		}
		qid = a.QuestionID
	default:
		s.internalError(w, r, "comment orphan",
			fmt.Errorf("comment %d has neither question_id nor answer_id", cid))
		return
	}
	parent, err := s.deps.Store.GetQuestion(ctx, qid)
	if err != nil {
		s.internalError(w, r, "get parent question", err)
		return
	}

	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}
	if !s.canEditAs(u, local.ID, c.AuthorUserID, c.CreatedAt.Time, parent.Status == store.FaqQuestionStatusLocked) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	bodyHTML := render.RenderMarkdownString(body)
	if err := s.deps.Store.EditComment(ctx, store.EditCommentParams{
		ID: cid, BodyMd: body, BodyHtml: bodyHTML, LastEditorUserID: &local.ID,
	}); err != nil {
		s.internalError(w, r, "edit comment", err)
		return
	}
	s.renderAfterComment(w, r, qid, u, local.ID)
}

// parseCommentBody parses + validates the comment body field. Returns
// (body, true) on success; on failure writes a 422 and returns (_, false).
func (s *Server) parseCommentBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	if err := parseForm(r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return "", false
	}
	body := strings.TrimSpace(postFormValue(r, "body"))
	if body == "" {
		http.Error(w, "body is required", http.StatusUnprocessableEntity)
		return "", false
	}
	if utf8Len(body) > maxCommentLen {
		http.Error(w, fmt.Sprintf("comment must be %d characters or fewer", maxCommentLen),
			http.StatusUnprocessableEntity)
		return "", false
	}
	return body, true
}

// renderAfterComment re-renders the question detail body so htmx swaps
// in the new comment list. The hx-target on every comment form is the
// outer .faq-q-detail, so this fragment replaces the whole body.
func (s *Server) renderAfterComment(w http.ResponseWriter, r *http.Request, qid int64, u *faquser.User, localID int64) {
	view, err := s.loadDetail(r.Context(), qid, u, localID)
	if err != nil {
		s.internalError(w, r, "load after comment", err)
		return
	}
	s.deps.Templates.Render(w, "partials/q_detail_body.gohtml", view)
}
