package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/store"
)

// listTags is the M3.6 implementation of GET /tags. Returns the full
// merged tag list sorted by descending question count, then by name. The
// list is small enough (a few hundred entries even after multi-dump
// imports) that we don't paginate in v0.1.
func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.resolveUser(w, r); ok {
		r = withUser(r, u)
	} else {
		return
	}
	ctx := r.Context()

	rows, err := s.deps.Store.ListTags(ctx)
	if err != nil {
		s.internalError(w, r, "list tags", err)
		return
	}
	tags := make([]TagSummary, len(rows))
	for i, t := range rows {
		ts := TagSummary{
			Name:          t.Name,
			URL:           s.deps.BasePath + "/tags/" + t.Name,
			QuestionCount: int(t.QuestionCount),
		}
		if t.Description != nil {
			ts.Description = *t.Description
		}
		tags[i] = ts
	}

	view := TagIndexView{Tags: tags}
	base := s.baseView(r, view)
	base.PageTitle = "Tags"

	if uiassets.IsRequest(r) {
		s.deps.Templates.Render(w, "partials/tag_list_body.gohtml", view)
		return
	}
	s.deps.Templates.Render(w, "pages/tag_index.gohtml", base)
}

// tagQuestions is the M3.6 implementation of GET /tags/{name}. Same
// pager + htmx pattern as /q, scoped to a single tag.
func (s *Server) tagQuestions(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.resolveUser(w, r); ok {
		r = withUser(r, u)
	} else {
		return
	}

	name := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	if name == "" {
		s.notFound(w, r)
		return
	}
	ctx := r.Context()

	// Confirm the tag exists; otherwise 404 (avoids a misleading empty
	// page for typo'd tag names).
	if _, err := s.deps.Store.GetTagByName(ctx, name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "tag lookup", err)
		return
	}

	limit, offset, page := s.pageBounds(r)

	qs, err := s.deps.Store.ListQuestionsByTag(ctx, store.ListQuestionsByTagParams{
		TagName: name, LimitN: limit, OffsetN: offset,
	})
	if err != nil {
		s.internalError(w, r, "list by tag", err)
		return
	}
	total, err := s.deps.Store.CountQuestionsByTag(ctx, name)
	if err != nil {
		s.internalError(w, r, "count by tag", err)
		return
	}

	rows := make([]questionRow, len(qs))
	for i, q := range qs {
		rows[i] = fromFaqQuestion(q)
	}
	cards, err := s.buildCards(ctx, rows)
	if err != nil {
		s.internalError(w, r, "build cards", err)
		return
	}

	view := TagDetailView{
		Name:      name,
		Questions: cards,
		Page:      page,
		PageSize:  int(limit),
		Total:     int(total),
		HasMore:   int64(offset)+int64(len(cards)) < total,
	}
	base := s.baseView(r, view)
	base.PageTitle = "Tag: " + name

	if uiassets.IsRequest(r) {
		s.deps.Templates.Render(w, "partials/tag_questions_body.gohtml", view)
		return
	}
	s.deps.Templates.Render(w, "pages/tag_questions.gohtml", base)
}
