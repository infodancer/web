package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/faquser"
	"github.com/infodancer/web/faq/store"
)

// detail is the M3.5 implementation of GET /q/{id}[/{slug}]. It composes
// 5+ small queries (question, answers, comments-on-question, tags, source,
// imported-author lookups) inside a single read transaction so the
// snapshot is consistent. If the URL slug doesn't match the canonical
// slug, we 301 to the canonical URL so search engines and clients land on
// a stable address.
func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.resolveUser(w, r); ok {
		r = withUser(r, u)
	} else {
		return
	}

	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	// Resolve the local user row when the request is authenticated so the
	// view-model can carry CanEdit/IsAdmin for hiding or showing edit /
	// vote / accept controls in the template. Anonymous requests skip
	// this; their view-model defaults to read-only affordances.
	curr := faquser.UserFromContext(ctx)
	var currLocalID int64
	if curr != nil {
		localRow, err := s.resolveLocalUser(ctx, curr)
		if err != nil {
			s.internalError(w, r, "resolve user", err)
			return
		}
		currLocalID = localRow.ID
	}

	view, err := s.loadDetail(ctx, id, curr, currLocalID)
	if err != nil {
		if errors.Is(err, errQuestionNotFound) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "load detail", err)
		return
	}

	// Canonical-slug redirect. If the request supplied a slug segment
	// that doesn't match the canonical one, send a 301 so the bookmark
	// converges on the right URL.
	urlSlug := r.PathValue("slug")
	if urlSlug != "" && urlSlug != view.Question.Slug && !uiassets.IsRequest(r) {
		target := fmt.Sprintf("%s/q/%d/%s", s.deps.BasePath, view.Question.ID, view.Question.Slug)
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}

	base := s.baseView(r, view)
	base.PageTitle = view.Question.Title
	base.CanonicalURL = canonicalURLFor(s.deps.BasePath, view)

	if uiassets.IsRequest(r) {
		s.deps.Templates.Render(w, "partials/q_detail_body.gohtml", view)
		return
	}
	s.deps.Templates.Render(w, "pages/q_detail.gohtml", base)
}

// canonicalURLFor returns the canonical detail URL for a question per
// D8.3 — the subsite-prefixed URL when the question has a canonical
// subsite, otherwise the global /q URL.
func canonicalURLFor(basePath string, v QuestionDetailView) string {
	if v.CanonicalSubsiteSlug != "" {
		return fmt.Sprintf("%s/%s/q/%d/%s", basePath, v.CanonicalSubsiteSlug, v.Question.ID, v.Question.Slug)
	}
	return fmt.Sprintf("%s/q/%d/%s", basePath, v.Question.ID, v.Question.Slug)
}

// errQuestionNotFound is the loadDetail sentinel for a missing or
// already-deleted question. The handler maps it to a 404.
var errQuestionNotFound = errors.New("handler: question not found")

// loadDetail performs the multi-query fan-out for the detail view. All
// queries run inside a single read transaction so the rendered view is
// a consistent snapshot. Returns errQuestionNotFound if the question
// row is missing or deleted.
//
// curr/currLocalID populate the CanEdit/IsAdmin fields on the view so
// templates can hide write affordances for anonymous and unauthorized
// viewers. Pass nil/0 for anonymous requests.
func (s *Server) loadDetail(ctx context.Context, id int64, curr *faquser.User, currLocalID int64) (QuestionDetailView, error) {
	var view QuestionDetailView

	err := s.deps.Store.WithTx(ctx, func(q *store.Queries) error {
		row, err := q.GetQuestion(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errQuestionNotFound
			}
			return fmt.Errorf("question: %w", err)
		}

		// Tags.
		tagRows, err := q.ListTagsForQuestion(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("tags: %w", err)
		}
		tagNames := make([]string, len(tagRows))
		for i, t := range tagRows {
			tagNames[i] = t.Name
		}

		// Viewer state drives the interactive controls (answer form, vote
		// cells, accept/edit/delete). The gates reuse the same checks the write
		// handlers enforce, so the UI never offers an action the POST rejects.
		authed := curr != nil
		open := row.Status == store.FaqQuestionStatusOpen
		canAccept := s.canAccept(curr, currLocalID, row)
		isAdmin := curr != nil && curr.Admin

		// Answers + per-answer comments + per-answer score. Soft-deleted answers
		// are hidden from everyone but admins, who see them inline with Restore.
		ansRows, err := q.ListAnswersForQuestion(ctx, store.ListAnswersForQuestionParams{
			QuestionID:     row.ID,
			IncludeDeleted: isAdmin,
		})
		if err != nil {
			return fmt.Errorf("answers: %w", err)
		}

		answers, err := s.buildAnswers(ctx, q, ansRows, row.AcceptedAnswerID, answerRenderCtx{
			questionID: row.ID,
			localID:    currLocalID,
			curr:       curr,
			authed:     authed,
			open:       open,
			canAccept:  canAccept,
		})
		if err != nil {
			return err
		}

		// Top-level comments on the question.
		qid := row.ID
		qComments, err := q.ListCommentsForQuestion(ctx, &qid)
		if err != nil {
			return fmt.Errorf("question comments: %w", err)
		}
		comments, err := buildComments(ctx, q, qComments)
		if err != nil {
			return err
		}

		// Author resolution: imported vs local.
		authorName, err := resolveAuthor(ctx, q, row.AuthorUserID, row.ImportedAuthorID)
		if err != nil {
			return err
		}

		// Source attribution for imported questions.
		var attr *SourceAttribution
		if row.SourceID != nil {
			src, err := q.GetSourceByID(ctx, *row.SourceID)
			if err != nil {
				return fmt.Errorf("source: %w", err)
			}
			a := SourceAttribution{
				Domain:      src.SourceDomain,
				DisplayName: src.DisplayName,
				License:     src.License,
			}
			if row.SourceUrl != nil {
				a.URL = *row.SourceUrl
			}
			attr = &a
		}

		// Best-effort score: question score = sum of answer scores' best.
		bestScore := 0
		for _, a := range answers {
			if a.Score > bestScore {
				bestScore = a.Score
			}
		}

		question := Question{
			ID:               row.ID,
			Slug:             slugFor(row.ID, row.Title),
			Title:            row.Title,
			BodyHTML:         row.BodyHtml,
			Score:            bestScore,
			AnswerCount:      len(answers),
			Status:           string(row.Status),
			Author:           authorName,
			AcceptedAnswerID: row.AcceptedAnswerID,
			Imported:         row.SourceID != nil,
			LocallyModified:  row.LocallyModified,
			NeedsReview:      row.NeedsReview,
		}
		if row.CreatedAt.Valid {
			question.CreatedAt = row.CreatedAt.Time
		}
		if row.EditedAt.Valid {
			t := row.EditedAt.Time
			question.EditedAt = &t
		}

		var createdAt time.Time
		if row.CreatedAt.Valid {
			createdAt = row.CreatedAt.Time
		}
		// Canonical subsite per D8.3 — empty when canonical_subsite_id is
		// NULL (multi- or zero-match imported question with no native
		// owner). pgx.ErrNoRows is the documented happy-path "no canonical
		// subsite"; anything else is a real failure.
		canonicalSubsiteSlug, err := q.GetCanonicalSubsiteSlugForQuestion(ctx, row.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("canonical subsite: %w", err)
		}

		view = QuestionDetailView{
			Question:             question,
			Tags:                 tagNames,
			Answers:              answers,
			Comments:             comments,
			Source:               attr,
			CanEdit:              s.canEditAs(curr, currLocalID, row.AuthorUserID, createdAt, row.Status == store.FaqQuestionStatusLocked),
			IsAdmin:              curr != nil && curr.Admin,
			Authed:               authed,
			CanAnswer:            authed && open,
			CanAccept:            canAccept,
			SignInURL:            s.deps.SignInURL,
			CanonicalSubsiteSlug: canonicalSubsiteSlug,
		}
		if view.CanAnswer {
			view.AnswerForm = &AnswerFormView{QuestionID: row.ID}
		}
		return nil
	})
	if err == nil && view.IsAdmin {
		// Load the M6c curation block out-of-transaction — the per-page
		// admin affordance doesn't need snapshot consistency with the
		// detail body, and these reads only happen on admin views.
		block, blockErr := s.loadAdminCurateBlock(ctx, id)
		if blockErr != nil {
			return QuestionDetailView{}, blockErr
		}
		view.AdminCurate = block
	}
	if err != nil {
		return QuestionDetailView{}, err
	}
	return view, nil
}

// renderDetailBody reloads the question and renders the q_detail_body fragment —
// the response the answer / vote / accept / delete handlers swap back into the
// page via htmx.
func (s *Server) renderDetailBody(w http.ResponseWriter, r *http.Request, qid int64, u *faquser.User, localID int64) {
	view, err := s.loadDetail(r.Context(), qid, u, localID)
	if err != nil {
		s.internalError(w, r, "load detail", err)
		return
	}
	s.deps.Templates.Render(w, "partials/q_detail_body.gohtml", view)
}

// answerRenderCtx carries the per-viewer state buildAnswers needs to decide
// which interactive controls each answer renders.
type answerRenderCtx struct {
	questionID int64
	localID    int64         // viewer's faq_users.id, 0 when anonymous
	curr       *faquser.User // viewer, nil when anonymous
	authed     bool
	open       bool // question is open (not locked/deleted)
	canAccept  bool // viewer may accept answers on this question
}

func (s *Server) buildAnswers(ctx context.Context, q *store.Queries, rows []store.ListAnswersForQuestionRow, accepted *int64, rc answerRenderCtx) ([]Answer, error) {
	out := make([]Answer, len(rows))
	for i, a := range rows {
		authorName, err := resolveAuthor(ctx, q, a.AuthorUserID, a.ImportedAuthorID)
		if err != nil {
			return nil, err
		}
		aid := a.ID
		ansComments, err := q.ListCommentsForAnswer(ctx, &aid)
		if err != nil {
			return nil, fmt.Errorf("answer comments: %w", err)
		}
		comments, err := buildComments(ctx, q, ansComments)
		if err != nil {
			return nil, err
		}

		ans := Answer{
			ID:              a.ID,
			QuestionID:      rc.questionID,
			BodyHTML:        a.BodyHtml,
			Score:           int(a.TotalScore),
			Accepted:        accepted != nil && *accepted == a.ID,
			Author:          authorName,
			Imported:        a.SourceID != nil,
			LocallyModified: a.LocallyModified,
			NeedsReview:     a.NeedsReview,
			Comments:        comments,
			Deleted:         a.DeletedAt.Valid,
		}
		if a.CreatedAt.Valid {
			ans.CreatedAt = a.CreatedAt.Time
		}
		if a.EditedAt.Valid {
			t := a.EditedAt.Time
			ans.EditedAt = &t
		}

		// A soft-deleted answer (only admins ever see one) carries no
		// interactive controls — just the muted body and a Restore button the
		// template adds. Live answers get the full set, gated to match the
		// write handlers so the UI never offers an action the POST rejects.
		if !ans.Deleted {
			ans.CanAccept = rc.canAccept
			ans.CanEdit = s.canEditAs(rc.curr, rc.localID, a.AuthorUserID, ans.CreatedAt, !rc.open)
			ans.CanDelete = s.canDeleteAnswer(rc.curr, rc.localID, a.AuthorUserID, rc.open)

			// Interactive vote cell for a signed-in viewer on an open question
			// — but never on their own answer, which the score trigger forbids
			// them from voting. Anonymous viewers and self-answers fall through
			// to the read-only score.
			mine := a.AuthorUserID != nil && *a.AuthorUserID == rc.localID
			if rc.authed && rc.open && !mine {
				userVote := ""
				v, verr := q.GetUserVote(ctx, store.GetUserVoteParams{AnswerID: a.ID, UserID: rc.localID})
				switch {
				case verr == nil:
					userVote = string(v.Value)
				case errors.Is(verr, pgx.ErrNoRows):
					// no vote cast yet — leave userVote empty
				default:
					return nil, fmt.Errorf("user vote: %w", verr)
				}
				ans.Vote = &VoteCellView{AnswerID: a.ID, Score: int(a.TotalScore), UserVote: userVote}
			}
		}

		out[i] = ans
	}
	return out, nil
}

func buildComments(ctx context.Context, q *store.Queries, rows []store.FaqComments) ([]Comment, error) {
	out := make([]Comment, len(rows))
	for i, c := range rows {
		authorName, err := resolveAuthor(ctx, q, c.AuthorUserID, c.ImportedAuthorID)
		if err != nil {
			return nil, err
		}
		comment := Comment{
			ID:       c.ID,
			BodyHTML: c.BodyHtml,
			Author:   authorName,
			Imported: c.SourceID != nil,
		}
		if c.CreatedAt.Valid {
			comment.CreatedAt = c.CreatedAt.Time
		}
		out[i] = comment
	}
	return out, nil
}

// resolveAuthor returns a display name from either the local user id or
// the imported-author id, preferring the imported author when both are
// nil for native rows authored anonymously (which v0.1 doesn't permit
// but the schema allows).
func resolveAuthor(ctx context.Context, q *store.Queries, userID, importedID *int64) (string, error) {
	if userID != nil {
		u, err := q.GetUserByID(ctx, *userID)
		if err == nil {
			return u.DisplayName, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("user: %w", err)
		}
	}
	if importedID != nil {
		a, err := q.GetImportedAuthor(ctx, *importedID)
		if err == nil {
			return a.DisplayName, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("imported author: %w", err)
		}
	}
	return "anonymous", nil
}
