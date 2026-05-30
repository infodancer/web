package handler

import (
	"fmt"
	"net/http"
	"strings"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/internal/render"
	"github.com/infodancer/web/faq/internal/slug"
	"github.com/infodancer/web/faq/store"
)

// subsiteAsk handles both GET (form) and POST (submit) on
// /{subsite}/ask. The dispatcher routes both methods here; we branch
// on r.Method.
//
// The submit path uses CreateNativeQuestionInSubsite so subsite_id is
// set at create time — making the new question reachable from this
// subsite via the native branch of the query shape (no tag-rule path
// required). The post-create redirect goes to the subsite-prefixed
// detail URL.
func (s *Server) subsiteAsk(w http.ResponseWriter, r *http.Request) {
	sub, ok := subsiteFromContext(r.Context())
	if !ok {
		s.notFound(w, r)
		return
	}
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	subPrefix := s.deps.BasePath + "/" + sub.Slug
	action := subPrefix + "/ask"
	heading := "Ask in " + sub.DisplayName

	switch r.Method {
	case http.MethodGet:
		view := AskQuestionView{
			ActionURL: action,
			Heading:   heading,
		}
		base := s.baseView(r, view)
		base.PageTitle = heading
		s.deps.Templates.Render(w, "pages/q_new.gohtml", base)
		return

	case http.MethodPost:
		if err := parseForm(r); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		title := strings.TrimSpace(postFormValue(r, "title"))
		body := strings.TrimSpace(postFormValue(r, "body"))
		tagsRaw := postFormValue(r, "tags")

		verrs := validateQuestionInput(title, body, tagsRaw)
		tags := normalizeTags(tagsRaw)
		if len(tags) > maxTags {
			verrs = append(verrs, fmt.Sprintf("at most %d tags", maxTags))
		}
		if len(verrs) > 0 {
			s.renderAskError(w, r, action, heading, title, body, tagsRaw, verrs)
			return
		}

		ctx := r.Context()
		local, err := s.resolveLocalUser(ctx, u)
		if err != nil {
			s.internalError(w, r, "subsite ask: resolve user", err)
			return
		}
		bodyHTML := render.RenderMarkdownString(body)

		var qID int64
		err = s.deps.Store.WithTx(ctx, func(q *store.Queries) error {
			qr, err := q.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
				Title:        title,
				BodyMd:       body,
				BodyHtml:     bodyHTML,
				AuthorUserID: &local.ID,
				SubsiteID:    &sub.ID,
			})
			if err != nil {
				return fmt.Errorf("create question: %w", err)
			}
			qID = qr.ID
			return attachTagsTx(ctx, q, qr.ID, tags)
		})
		if err != nil {
			s.internalError(w, r, "subsite ask: create", err)
			return
		}

		dest := fmt.Sprintf("%s/q/%d/%s", subPrefix, qID, slug.Normalize(title))
		if uiassets.IsRequest(r) {
			w.Header().Set("HX-Redirect", dest)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, dest, http.StatusSeeOther)
		return

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}
