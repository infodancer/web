package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/infodancer/web/faq/internal/render"
	"github.com/infodancer/web/faq/store"
)

// postAnswer handles POST /q/{id}/answers. Creates a new answer on an
// open question; locked questions are rejected so imported content
// stays read-only by default.
func (s *Server) postAnswer(w http.ResponseWriter, r *http.Request) {
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
	body := strings.TrimSpace(postFormValue(r, "body"))
	verrs := validateBody(body)
	if len(verrs) > 0 {
		s.renderAnswerFormErrors(w, qid, body, verrs)
		return
	}

	ctx := r.Context()
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}

	q, err := s.deps.Store.GetQuestion(ctx, qid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get question", err)
		return
	}
	if q.Status != store.FaqQuestionStatusOpen {
		http.Error(w, "this question is locked", http.StatusForbidden)
		return
	}

	bodyHTML := render.RenderMarkdownString(body)
	_, err = s.deps.Store.CreateAnswer(ctx, store.CreateAnswerParams{
		QuestionID: qid, BodyMd: body, BodyHtml: bodyHTML, AuthorUserID: &local.ID,
	})
	if err != nil {
		s.internalError(w, r, "create answer", err)
		return
	}

	// Re-render the whole detail body so htmx swaps in the new answer
	// list. The caller's hx-target is the question's <article> wrapper.
	view, err := s.loadDetail(ctx, qid, u, local.ID)
	if err != nil {
		s.internalError(w, r, "load after answer", err)
		return
	}
	s.deps.Templates.Render(w, "partials/q_detail_body.gohtml", view)
}

// editAnswerForm renders GET /a/{id}/edit as an htmx fragment that
// replaces the answer's <li>. Same edit gate as questions.
func (s *Server) editAnswerForm(w http.ResponseWriter, r *http.Request) {
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
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}
	if !s.canEditAs(u, local.ID, a.AuthorUserID, a.CreatedAt.Time, parent.Status == store.FaqQuestionStatusLocked) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	s.deps.Templates.Render(w, "partials/answer_edit_form.gohtml", EditAnswerView{
		ID: a.ID, QuestionID: a.QuestionID, Body: a.BodyMd,
	})
}

// editAnswerSubmit handles PATCH /a/{id}. Same gate as the form, plus
// always re-renders the full question detail body so the htmx swap
// picks up the new answer markup (including any accepted-answer or
// score reflow).
func (s *Server) editAnswerSubmit(w http.ResponseWriter, r *http.Request) {
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
	body := strings.TrimSpace(postFormValue(r, "body"))
	verrs := validateBody(body)

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
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}
	if !s.canEditAs(u, local.ID, a.AuthorUserID, a.CreatedAt.Time, parent.Status == store.FaqQuestionStatusLocked) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if len(verrs) > 0 {
		s.deps.Templates.Render(w, "partials/answer_edit_form.gohtml", EditAnswerView{
			ID: a.ID, QuestionID: a.QuestionID, Body: body, Errors: verrs,
		})
		return
	}

	bodyHTML := render.RenderMarkdownString(body)
	if err := s.deps.Store.EditAnswer(ctx, store.EditAnswerParams{
		ID: aid, BodyMd: body, BodyHtml: bodyHTML, LastEditorUserID: &local.ID,
	}); err != nil {
		s.internalError(w, r, "edit answer", err)
		return
	}

	// Re-render the parent question's full detail so the answer list
	// reflects the edit (and admin edits flip the imported flag in the
	// rendered footer).
	view, err := s.loadDetail(ctx, a.QuestionID, u, local.ID)
	if err != nil {
		s.internalError(w, r, "load after edit", err)
		return
	}
	s.deps.Templates.Render(w, "partials/q_detail_body.gohtml", view)
}

// validateBody returns errors for an answer/comment body. Mirrors
// validateQuestionInput shape but covers a single field.
func validateBody(body string) []string {
	var verrs []string
	if body == "" {
		verrs = append(verrs, "body is required")
	} else if utf8Len(body) > maxBodyLen {
		verrs = append(verrs, fmt.Sprintf("body must be %d characters or fewer", maxBodyLen))
	}
	return verrs
}

func (s *Server) renderAnswerFormErrors(w http.ResponseWriter, qid int64, body string, errs []string) {
	w.WriteHeader(http.StatusUnprocessableEntity)
	s.deps.Templates.Render(w, "partials/answer_form.gohtml", AnswerFormView{
		QuestionID: qid, Body: body, Errors: errs,
	})
}
