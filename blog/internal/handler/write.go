package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/infodancer/ui/mdedit"

	"github.com/infodancer/web/blog/bloguser"
	"github.com/infodancer/web/blog/internal/render"
	"github.com/infodancer/web/blog/internal/slug"
	"github.com/infodancer/web/blog/store"
)

// maxTagsPerPost caps the tag list an author can attach.
const maxTagsPerPost = 10

// editView is the page payload for the new/edit post form.
type editView struct {
	IsNew           bool
	Post            *store.Post // nil on the new form
	Field           mdedit.Field
	Blogs           []store.Blog
	SelectedBlogID  int64 // 0 = no named blog
	TagsValue       string
	ExcerptValue    string
	TitleValue      string
	SlugValue       string
	CanOverrideSlug bool
	Errors          []string
}

// handleMine lists the signed-in author's own posts in every status.
func (s *Server) handleMine(w http.ResponseWriter, r *http.Request) {
	user := bloguser.UserFromContext(r.Context())
	posts, err := s.store.ListByAuthorKeyAnyStatus(r.Context(), user.Key, 200, 0)
	if err != nil {
		s.serverError(w, "mine", err)
		return
	}
	view := s.baseView(r, mineView{Posts: s.cards(posts)})
	view.Meta.Robots = "noindex"
	s.renderPage(w, r, "mine.gohtml", http.StatusOK, view)
}

// handleNewForm renders the empty post form.
func (s *Server) handleNewForm(w http.ResponseWriter, r *http.Request) {
	s.renderEditForm(w, r, editView{IsNew: true}, http.StatusOK)
}

// handleEditForm renders the form pre-filled from an existing post.
func (s *Server) handleEditForm(w http.ResponseWriter, r *http.Request) {
	post, ok := s.loadEditablePost(w, r)
	if !ok {
		return
	}
	tagNames := make([]string, 0, len(post.Tags))
	for _, t := range post.Tags {
		tagNames = append(tagNames, t.Name)
	}
	view := editView{
		Post:         post,
		TitleValue:   post.Title,
		ExcerptValue: post.Excerpt,
		TagsValue:    strings.Join(tagNames, ", "),
		SlugValue:    post.Slug,
	}
	if post.BlogID != nil {
		view.SelectedBlogID = *post.BlogID
	}
	view.Field.Markdown = post.BodyMD
	s.renderEditForm(w, r, view, http.StatusOK)
}

// renderEditForm fills the form-shared pieces (named-blog options, the
// mdedit field defaults, editor head tags) and renders.
func (s *Server) renderEditForm(w http.ResponseWriter, r *http.Request, view editView, status int) {
	blogs, err := s.store.ListBlogs(r.Context())
	if err != nil {
		s.serverError(w, "edit form blogs", err)
		return
	}
	view.Blogs = blogs

	user := bloguser.UserFromContext(r.Context())
	view.CanOverrideSlug = user != nil && user.Admin

	f := view.Field
	f.ID = "post-body"
	f.Name = "body_md"
	f.Label = "Body (markdown)"
	f.Rows = 20
	f.Required = true
	f.AllowFileLoad = true
	if s.ingestImage != nil {
		f.UploadURL = s.basePath + "/upload"
	}
	view.Field = f.WithDefaults()

	base := s.baseView(r, view)
	base.EditorHead = mdedit.HeadTags(s.basePath + "/static/mdedit")
	base.Meta.Robots = "noindex"
	s.renderPage(w, r, "post_edit.gohtml", status, base)
}

// postForm is the parsed, validated post form.
type postForm struct {
	Title    string
	BodyMD   string
	Excerpt  string
	BlogID   *int64
	Slug     string // admin override; "" keeps/derives
	Tags     []store.TagInput
	TagsRaw  string
	Publish  bool
	Errors   []string
	BlogID64 int64
}

func (s *Server) parsePostForm(r *http.Request, user *bloguser.User) postForm {
	var f postForm
	r.Body = http.MaxBytesReader(nil, r.Body, s.maxBodyBytes)
	if err := r.ParseForm(); err != nil {
		f.Errors = append(f.Errors, "form too large or malformed")
		return f
	}
	f.Title = strings.TrimSpace(r.PostFormValue("title"))
	f.BodyMD = strings.TrimSpace(r.PostFormValue("body_md"))
	f.Excerpt = strings.TrimSpace(r.PostFormValue("excerpt"))
	f.TagsRaw = r.PostFormValue("tags")
	f.Publish = r.PostFormValue("action") == "publish"

	if f.Title == "" {
		f.Errors = append(f.Errors, "a title is required")
	}
	if f.BodyMD == "" {
		f.Errors = append(f.Errors, "the body is empty")
	}

	if v := r.PostFormValue("blog_id"); v != "" && v != "0" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 0 {
			f.Errors = append(f.Errors, "invalid blog selection")
		} else {
			f.BlogID = &id
			f.BlogID64 = id
		}
	}

	if user.Admin {
		if v := strings.TrimSpace(r.PostFormValue("slug")); v != "" {
			f.Slug = slug.Normalize(v)
			if f.Slug == "" {
				f.Errors = append(f.Errors, "slug must contain letters or digits")
			}
		}
	}

	f.Tags = parseTags(f.TagsRaw)
	if len(f.Tags) > maxTagsPerPost {
		f.Errors = append(f.Errors, "too many tags (max "+strconv.Itoa(maxTagsPerPost)+")")
	}
	return f
}

// parseTags splits the comma-separated tag input into slugged, deduplicated
// tag inputs.
func parseTags(raw string) []store.TagInput {
	seen := map[string]bool{}
	var out []store.TagInput
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		sl := slug.Normalize(name)
		if sl == "" || seen[sl] {
			continue
		}
		seen[sl] = true
		out = append(out, store.TagInput{Slug: sl, Name: name})
	}
	return out
}

// handleCreate persists a new post as draft or published.
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	user := bloguser.UserFromContext(r.Context())
	f := s.parsePostForm(r, user)
	if len(f.Errors) > 0 {
		s.rerenderForm(w, r, f, nil)
		return
	}

	author, err := s.ensureAuthorFor(r.Context(), user)
	if err != nil {
		s.serverError(w, "ensure author", err)
		return
	}

	status := store.StatusDraft
	if f.Publish {
		status = store.StatusPublished
	}
	baseSlug := f.Slug
	if baseSlug == "" {
		baseSlug = slug.Normalize(f.Title)
	}
	post, err := s.store.CreatePost(r.Context(), baseSlug, status, store.PostParams{
		Title:     f.Title,
		BodyMD:    f.BodyMD,
		BodyHTML:  string(render.Body(f.BodyMD)),
		Excerpt:   f.Excerpt,
		AuthorKey: author.UserKey,
		BlogID:    f.BlogID,
	})
	if err != nil {
		s.serverError(w, "create post", err)
		return
	}
	if err := s.store.SetPostTags(r.Context(), post.ID, f.Tags); err != nil {
		s.serverError(w, "set tags", err)
		return
	}
	s.syncSearch(r.Context(), post.ID)
	http.Redirect(w, r, s.basePath+"/p/"+post.Slug, http.StatusSeeOther)
}

// handleUpdate rewrites an existing post's editable fields.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	post, ok := s.loadEditablePost(w, r)
	if !ok {
		return
	}
	user := bloguser.UserFromContext(r.Context())
	f := s.parsePostForm(r, user)
	if len(f.Errors) > 0 {
		s.rerenderForm(w, r, f, post)
		return
	}

	// Keep the registry's display identity fresh on every save.
	if _, err := s.ensureAuthorFor(r.Context(), user); err != nil {
		s.log.Warn("refresh author registry", "err", err)
	}

	updated, err := s.store.UpdatePost(r.Context(), post.ID, f.Slug, store.PostParams{
		Title:    f.Title,
		BodyMD:   f.BodyMD,
		BodyHTML: string(render.Body(f.BodyMD)),
		Excerpt:  f.Excerpt,
		BlogID:   f.BlogID,
	})
	if err != nil {
		if errors.Is(err, store.ErrSlugTaken) {
			f.Errors = append(f.Errors, "that slug is already in use")
			s.rerenderForm(w, r, f, post)
			return
		}
		s.serverError(w, "update post", err)
		return
	}
	if err := s.store.SetPostTags(r.Context(), updated.ID, f.Tags); err != nil {
		s.serverError(w, "set tags", err)
		return
	}
	s.syncSearch(r.Context(), updated.ID)
	http.Redirect(w, r, s.basePath+"/p/"+updated.Slug, http.StatusSeeOther)
}

// rerenderForm shows the form again with validation errors, preserving the
// author's input.
func (s *Server) rerenderForm(w http.ResponseWriter, r *http.Request, f postForm, post *store.Post) {
	view := editView{
		IsNew:          post == nil,
		Post:           post,
		TitleValue:     f.Title,
		ExcerptValue:   f.Excerpt,
		TagsValue:      f.TagsRaw,
		SlugValue:      f.Slug,
		SelectedBlogID: f.BlogID64,
		Errors:         f.Errors,
	}
	view.Field.Markdown = f.BodyMD
	view.Field.Errors = f.Errors
	s.renderEditForm(w, r, view, http.StatusUnprocessableEntity)
}

// statusHandler builds the publish/unpublish/archive POST handlers.
func (s *Server) statusHandler(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		post, ok := s.loadEditablePost(w, r)
		if !ok {
			return
		}
		updated, err := s.store.SetPostStatus(r.Context(), post.ID, target)
		if err != nil {
			s.serverError(w, "set status", err)
			return
		}
		s.syncSearch(r.Context(), updated.ID)
		http.Redirect(w, r, s.basePath+"/p/"+updated.Slug, http.StatusSeeOther)
	}
}

// handleDelete soft-deletes a post.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	post, ok := s.loadEditablePost(w, r)
	if !ok {
		return
	}
	if err := s.store.SoftDeletePost(r.Context(), post.ID); err != nil {
		s.serverError(w, "delete post", err)
		return
	}
	s.deindex(r.Context(), post.ID)
	http.Redirect(w, r, s.basePath+"/mine", http.StatusSeeOther)
}

// handleUpload accepts one inline image (multipart field "image") and
// returns mdedit's expected JSON {"url": "..."}.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if s.ingestImage == nil {
		http.NotFound(w, r)
		return
	}
	user := bloguser.UserFromContext(r.Context())

	// media.Process enforces the real byte/pixel caps; this is the outer
	// transport bound.
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	file, _, err := r.FormFile("image")
	if err != nil {
		uploadError(w, http.StatusBadRequest, "missing image field")
		return
	}
	defer func() { _ = file.Close() }()

	url, err := s.ingestImage(r.Context(), "user:"+user.Key, file)
	if err != nil {
		s.log.Warn("image upload", "user", user.Key, "err", err)
		uploadError(w, http.StatusUnprocessableEntity, "image rejected")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"url": url})
}

func uploadError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// loadEditablePost fetches the {id} post and enforces edit authorization,
// writing the error response itself when it returns ok=false.
func (s *Server) loadEditablePost(w http.ResponseWriter, r *http.Request) (*store.Post, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, false
	}
	post, err := s.store.GetPostByID(r.Context(), id)
	if err != nil {
		s.notFoundOr(w, "load post", err)
		return nil, false
	}
	user := bloguser.UserFromContext(r.Context())
	if !mayEditPost(user, post.AuthorKey) {
		http.Error(w, "forbidden: not your post", http.StatusForbidden)
		return nil, false
	}
	return post, true
}

// ensureAuthorFor creates/refreshes the registry row for user, preferring
// the host profile directory's identity when wired.
func (s *Server) ensureAuthorFor(ctx context.Context, user *bloguser.User) (*store.Author, error) {
	display := user.DisplayName
	want := ""
	if s.authors != nil {
		if info, err := s.authors.Lookup(ctx, user.Key); err == nil && info != nil {
			if info.DisplayName != "" {
				display = info.DisplayName
			}
			want = info.Slug
		} else if err != nil {
			s.log.Warn("author directory lookup", "key", user.Key, "err", err)
		}
	}
	if display == "" {
		display = "author"
	}
	if want == "" {
		want = slug.Normalize(display)
	}
	return s.store.EnsureAuthor(ctx, user.Key, want, display)
}

// syncSearch reflects a post's current state into the host's search index:
// published posts are (re)indexed, everything else is removed. Failures are
// logged, never user-facing — search lags rather than blocking a save.
func (s *Server) syncSearch(ctx context.Context, postID int64) {
	if s.indexer == nil {
		return
	}
	post, err := s.store.GetPostByID(ctx, postID)
	if err != nil {
		s.log.Warn("search sync: load", "post", postID, "err", err)
		return
	}
	if post.Status != store.StatusPublished {
		s.deindex(ctx, postID)
		return
	}
	doc := SearchDocument{
		ExternalID: strconv.FormatInt(post.ID, 10),
		URL:        s.absURL("/p/" + post.Slug),
		Title:      post.Title,
		Summary:    displayExcerpt(*post),
		BodyMD:     post.BodyMD,
	}
	for _, t := range post.Tags {
		doc.Tags = append(doc.Tags, t.Name)
	}
	if post.PublishedAt != nil {
		doc.PublishedAt = *post.PublishedAt
	}
	if err := s.indexer.IndexPost(ctx, doc); err != nil {
		s.log.Warn("search sync: index", "post", postID, "err", err)
	}
}

func (s *Server) deindex(ctx context.Context, postID int64) {
	if s.indexer == nil {
		return
	}
	if err := s.indexer.DeletePost(ctx, strconv.FormatInt(postID, 10)); err != nil {
		s.log.Warn("search sync: delete", "post", postID, "err", err)
	}
}

// ReindexAll walks every published post into the search index, for
// boot-time reconciliation by the host.
func (s *Server) ReindexAll(ctx context.Context) error {
	if s.indexer == nil {
		return nil
	}
	const batch = 200
	for offset := 0; ; offset += batch {
		posts, _, err := s.store.ListPublished(ctx, batch, offset)
		if err != nil {
			return err
		}
		if len(posts) == 0 {
			return nil
		}
		for _, p := range posts {
			s.syncSearch(ctx, p.ID)
		}
		if len(posts) < batch {
			return nil
		}
	}
}
