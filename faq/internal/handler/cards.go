package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/infodancer/web/faq/internal/slug"
	"github.com/infodancer/web/faq/store"
)

// buildCards is the global-scope variant of [Server.buildCardsForPrefix].
// Detail-page URLs point at the global /q tree.
func (s *Server) buildCards(ctx context.Context, rows []questionRow) ([]QuestionCard, error) {
	return s.buildCardsForPrefix(ctx, rows, s.deps.BasePath)
}

// buildCardsForPrefix converts a slice of faq_questions rows into
// [QuestionCard] view-models, attaching the per-question answer count,
// best-answer score, tag list, source attribution, and the absolute
// detail-page URL. All extras are fetched in a single batch of
// follow-up queries — this keeps every list view at O(1) queries
// regardless of page size.
//
// linkPrefix is the path under which detail and tag URLs are
// constructed: `/faq` for the global tree, `/faq/{subsite}` for a
// subsite-scoped tree. The handler picks the right one before calling.
//
// rows may be either store.FaqQuestions, ListQuestionsByVotesRow, or any
// other shape that exposes the question id and core fields; callers pass
// pre-extracted normalized data via [questionRow] to avoid duplicating
// the field-projection logic across sort variants.
func (s *Server) buildCardsForPrefix(ctx context.Context, rows []questionRow, linkPrefix string) ([]QuestionCard, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}

	counts, err := s.deps.Store.CountAnswersByQuestionIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("count answers: %w", err)
	}
	countByID := make(map[int64]int, len(counts))
	for _, c := range counts {
		countByID[c.QuestionID] = int(c.AnswerCount)
	}

	scores, err := s.deps.Store.BestScoreByQuestionIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("best scores: %w", err)
	}
	scoreByID := make(map[int64]int, len(scores))
	for _, sc := range scores {
		scoreByID[sc.QuestionID] = int(sc.BestScore)
	}

	tags, err := s.deps.Store.TagsByQuestionIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("tags by question: %w", err)
	}
	tagsByID := make(map[int64][]string, len(rows))
	for _, t := range tags {
		tagsByID[t.QuestionID] = append(tagsByID[t.QuestionID], t.TagName)
	}

	// Source attribution for imported questions. Collect the distinct
	// source IDs first, then a single SELECT covers them all.
	srcIDSet := map[int64]struct{}{}
	for _, r := range rows {
		if r.SourceID != nil {
			srcIDSet[*r.SourceID] = struct{}{}
		}
	}
	var sourceByID map[int64]SourceAttribution
	if len(srcIDSet) > 0 {
		srcIDs := make([]int64, 0, len(srcIDSet))
		for id := range srcIDSet {
			srcIDs = append(srcIDs, id)
		}
		srcs, err := s.deps.Store.SourcesByIDs(ctx, srcIDs)
		if err != nil {
			return nil, fmt.Errorf("sources by ids: %w", err)
		}
		sourceByID = make(map[int64]SourceAttribution, len(srcs))
		for _, sr := range srcs {
			sourceByID[sr.ID] = SourceAttribution{
				Domain:      sr.SourceDomain,
				DisplayName: sr.DisplayName,
				License:     sr.License,
			}
		}
	}

	cards := make([]QuestionCard, len(rows))
	for i, r := range rows {
		tagNames := tagsByID[r.ID]
		tagLinks := make([]TagLink, len(tagNames))
		for j, name := range tagNames {
			tagLinks[j] = TagLink{Name: name, URL: linkPrefix + "/tags/" + name}
		}
		slug := slugFor(r.ID, r.Title)
		c := QuestionCard{
			ID:          r.ID,
			Slug:        slug,
			Title:       r.Title,
			Excerpt:     excerpt(r.BodyMD, 200),
			Tags:        tagNames,
			TagLinks:    tagLinks,
			URL:         fmt.Sprintf("%s/q/%d/%s", linkPrefix, r.ID, slug),
			AnswerCount: countByID[r.ID],
			Score:       scoreByID[r.ID],
			Imported:    r.SourceID != nil,
			Locked:      r.Status == string(store.FaqQuestionStatusLocked),
		}
		c.CreatedAt = r.CreatedAt
		if r.SourceID != nil {
			if src, ok := sourceByID[*r.SourceID]; ok {
				if r.SourceURL != nil {
					src.URL = *r.SourceURL
				}
				c.Source = &src
			}
		}
		cards[i] = c
	}
	return cards, nil
}

// questionRow is the minimal projection [buildCards] needs from any list
// query's row shape. Callers convert from store.FaqQuestions or sibling
// row types before invoking buildCards.
type questionRow struct {
	ID        int64
	Title     string
	BodyMD    string
	Status    string
	SourceID  *int64
	SourceURL *string
	CreatedAt time.Time
}

// fromFaqQuestion converts a store.FaqQuestions row into a questionRow.
func fromFaqQuestion(q store.FaqQuestions) questionRow {
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
	return r
}

// excerpt returns the first n runes of body, stripping markdown link
// syntax to "label" form and collapsing whitespace. Cheap and good
// enough for v0.1 list cards; replace with a smarter extractor later
// if needed.
func excerpt(body string, n int) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	// Replace newlines with spaces; the excerpt is single-line.
	body = strings.ReplaceAll(body, "\n", " ")
	body = strings.Join(strings.Fields(body), " ")
	if len([]rune(body)) <= n {
		return body
	}
	r := []rune(body)
	cut := string(r[:n])
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return cut + "…"
}

// slugFor produces the canonical slug for a question. Falls back to
// "q-{id}" if the title has no [a-z0-9] content after normalization.
func slugFor(id int64, title string) string {
	if s := slug.Normalize(title); s != "" {
		return s
	}
	return fmt.Sprintf("q-%d", id)
}

// ErrUnknownSort signals that a sort= query parameter is not one the
// list handler accepts.
var ErrUnknownSort = errors.New("handler: unknown sort")
