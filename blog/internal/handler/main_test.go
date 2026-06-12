package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/infodancer/web/blog/bloguser"
	"github.com/infodancer/web/blog/store"
)

// fakeStore is an in-memory BlogStore for handler tests.
type fakeStore struct {
	mu      sync.Mutex
	nextID  int64
	blogs   map[int64]*store.Blog
	authors map[string]*store.Author // by user key
	posts   map[int64]*store.Post
	tags    map[int64][]store.Tag // by post id
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		nextID:  1,
		blogs:   map[int64]*store.Blog{},
		authors: map[string]*store.Author{},
		posts:   map[int64]*store.Post{},
		tags:    map[int64][]store.Tag{},
	}
}

func (f *fakeStore) id() int64 { v := f.nextID; f.nextID++; return v }

func (f *fakeStore) CreateBlog(_ context.Context, slug, name, description string) (*store.Blog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.blogs {
		if b.Slug == slug {
			return nil, store.ErrSlugTaken
		}
	}
	b := &store.Blog{ID: f.id(), Slug: slug, Name: name, Description: description, CreatedAt: time.Now()}
	f.blogs[b.ID] = b
	return b, nil
}

func (f *fakeStore) UpdateBlog(_ context.Context, id int64, slug, name, description string) (*store.Blog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.blogs[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	for _, other := range f.blogs {
		if other.ID != id && other.Slug == slug {
			return nil, store.ErrSlugTaken
		}
	}
	b.Slug, b.Name, b.Description = slug, name, description
	return b, nil
}

func (f *fakeStore) DeleteBlog(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.blogs[id]; !ok {
		return store.ErrNotFound
	}
	for _, p := range f.posts {
		if p.BlogID != nil && *p.BlogID == id && p.Status != "deleted" {
			return store.ErrBlogInUse
		}
	}
	delete(f.blogs, id)
	return nil
}

func (f *fakeStore) GetBlogBySlug(_ context.Context, slug string) (*store.Blog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.blogs {
		if b.Slug == slug {
			return b, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) ListBlogs(_ context.Context) ([]store.Blog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []store.Blog{}
	for _, b := range f.blogs {
		c := *b
		for _, p := range f.posts {
			if p.BlogID != nil && *p.BlogID == b.ID {
				c.PostCount++
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeStore) EnsureAuthor(_ context.Context, userKey, wantSlug, displayName string) (*store.Author, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.authors[userKey]; ok {
		a.DisplayName = displayName
		return a, nil
	}
	a := &store.Author{UserKey: userKey, Slug: wantSlug, DisplayName: displayName, CreatedAt: time.Now()}
	f.authors[userKey] = a
	return a, nil
}

func (f *fakeStore) GetAuthorBySlug(_ context.Context, slug string) (*store.Author, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.authors {
		if a.Slug == slug {
			return a, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) ListAuthors(_ context.Context) ([]store.Author, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []store.Author{}
	for _, a := range f.authors {
		c := *a
		for _, p := range f.posts {
			if p.AuthorKey == a.UserKey && p.Status == store.StatusPublished {
				c.PostCount++
			}
		}
		if c.PostCount > 0 {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStore) fill(p *store.Post) {
	if a, ok := f.authors[p.AuthorKey]; ok {
		p.AuthorSlug, p.AuthorName = a.Slug, a.DisplayName
	}
	p.BlogSlug, p.BlogName = "", ""
	if p.BlogID != nil {
		if b, ok := f.blogs[*p.BlogID]; ok {
			p.BlogSlug, p.BlogName = b.Slug, b.Name
		}
	}
	p.Tags = f.tags[p.ID]
}

func (f *fakeStore) CreatePost(_ context.Context, baseSlug, status string, params store.PostParams) (*store.Post, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	slug := baseSlug
	for i := 2; ; i++ {
		taken := false
		for _, p := range f.posts {
			if p.Slug == slug {
				taken = true
				break
			}
		}
		if !taken {
			break
		}
		slug = baseSlug + "-" + strconv.Itoa(i)
	}
	now := time.Now()
	p := &store.Post{
		ID: f.id(), Slug: slug, Title: params.Title, BodyMD: params.BodyMD,
		BodyHTML: params.BodyHTML, Excerpt: params.Excerpt, AuthorKey: params.AuthorKey,
		BlogID: params.BlogID, Status: status, CreatedAt: now, UpdatedAt: now,
	}
	if status == store.StatusPublished {
		t := now
		p.PublishedAt = &t
	}
	f.posts[p.ID] = p
	c := *p
	f.fill(&c)
	return &c, nil
}

func (f *fakeStore) UpdatePost(_ context.Context, id int64, newSlug string, params store.PostParams) (*store.Post, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.posts[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	if newSlug != "" {
		for _, other := range f.posts {
			if other.ID != id && other.Slug == newSlug {
				return nil, store.ErrSlugTaken
			}
		}
		p.Slug = newSlug
	}
	p.Title, p.BodyMD, p.BodyHTML, p.Excerpt, p.BlogID = params.Title, params.BodyMD, params.BodyHTML, params.Excerpt, params.BlogID
	p.UpdatedAt = time.Now()
	c := *p
	f.fill(&c)
	return &c, nil
}

func (f *fakeStore) SetPostStatus(_ context.Context, id int64, status string) (*store.Post, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.posts[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	p.Status = status
	if status == store.StatusPublished && p.PublishedAt == nil {
		t := time.Now()
		p.PublishedAt = &t
	}
	p.UpdatedAt = time.Now()
	c := *p
	f.fill(&c)
	return &c, nil
}

func (f *fakeStore) SoftDeletePost(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.posts[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.posts, id)
	return nil
}

func (f *fakeStore) GetPostByID(_ context.Context, id int64) (*store.Post, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.posts[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	c := *p
	f.fill(&c)
	return &c, nil
}

func (f *fakeStore) GetPostBySlug(_ context.Context, slug string) (*store.Post, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.posts {
		if p.Slug == slug {
			c := *p
			f.fill(&c)
			return &c, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) published(filter func(*store.Post) bool) []store.Post {
	out := []store.Post{}
	for _, p := range f.posts {
		if p.Status != store.StatusPublished {
			continue
		}
		if filter != nil && !filter(p) {
			continue
		}
		c := *p
		f.fill(&c)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

func (f *fakeStore) ListPublished(_ context.Context, limit, offset int) ([]store.Post, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.published(nil)
	return window(all, limit, offset), int64(len(all)), nil
}

func (f *fakeStore) ListPublishedByBlog(_ context.Context, blogSlug string, limit, offset int) ([]store.Post, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.published(func(p *store.Post) bool {
		if p.BlogID == nil {
			return false
		}
		b, ok := f.blogs[*p.BlogID]
		return ok && b.Slug == blogSlug
	})
	return window(all, limit, offset), int64(len(all)), nil
}

func (f *fakeStore) ListPublishedByAuthor(_ context.Context, authorSlug string, limit, offset int) ([]store.Post, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.published(func(p *store.Post) bool {
		a, ok := f.authors[p.AuthorKey]
		return ok && a.Slug == authorSlug
	})
	return window(all, limit, offset), int64(len(all)), nil
}

func (f *fakeStore) ListPublishedByTag(_ context.Context, tagSlug string, limit, offset int) ([]store.Post, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.published(func(p *store.Post) bool {
		for _, t := range f.tags[p.ID] {
			if t.Slug == tagSlug {
				return true
			}
		}
		return false
	})
	return window(all, limit, offset), int64(len(all)), nil
}

func (f *fakeStore) ListByAuthorKeyAnyStatus(_ context.Context, authorKey string, limit, offset int) ([]store.Post, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []store.Post{}
	for _, p := range f.posts {
		if p.AuthorKey == authorKey {
			c := *p
			f.fill(&c)
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return window(out, limit, offset), nil
}

func (f *fakeStore) SetPostTags(_ context.Context, postID int64, tags []store.TagInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Tag
	for i, t := range tags {
		out = append(out, store.Tag{ID: int64(i + 1), Slug: t.Slug, Name: t.Name})
	}
	f.tags[postID] = out
	return nil
}

func (f *fakeStore) GetTagBySlug(_ context.Context, slug string) (*store.Tag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, tags := range f.tags {
		for _, t := range tags {
			if t.Slug == slug {
				c := t
				return &c, nil
			}
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) ListTags(_ context.Context) ([]store.Tag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[string]*store.Tag{}
	for postID, tags := range f.tags {
		p, ok := f.posts[postID]
		if !ok || p.Status != store.StatusPublished {
			continue
		}
		for _, t := range tags {
			if existing, ok := seen[t.Slug]; ok {
				existing.PostCount++
			} else {
				c := t
				c.PostCount = 1
				seen[t.Slug] = &c
			}
		}
	}
	out := []store.Tag{}
	for _, t := range seen {
		out = append(out, *t)
	}
	return out, nil
}

func (f *fakeStore) TagsForPosts(_ context.Context, postIDs []int64) (map[int64][]store.Tag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[int64][]store.Tag{}
	for _, id := range postIDs {
		if tags := f.tags[id]; len(tags) > 0 {
			out[id] = tags
		}
	}
	return out, nil
}

func window(posts []store.Post, limit, offset int) []store.Post {
	if offset >= len(posts) {
		return []store.Post{}
	}
	end := offset + limit
	if end > len(posts) {
		end = len(posts)
	}
	return posts[offset:end]
}

// stubResolver returns a fixed user (nil = anonymous).
type stubResolver struct{ user *bloguser.User }

func (s stubResolver) Resolve(context.Context, *http.Request) (*bloguser.User, error) {
	return s.user, nil
}

// fakeIndexer records search hook calls.
type fakeIndexer struct {
	mu      sync.Mutex
	indexed map[string]SearchDocument
	deleted []string
}

func newFakeIndexer() *fakeIndexer {
	return &fakeIndexer{indexed: map[string]SearchDocument{}}
}

func (f *fakeIndexer) IndexPost(_ context.Context, doc SearchDocument) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.indexed[doc.ExternalID] = doc
	return nil
}

func (f *fakeIndexer) DeletePost(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.indexed, id)
	f.deleted = append(f.deleted, id)
	return nil
}

type testEnv struct {
	store   *fakeStore
	indexer *fakeIndexer
	server  *Server
}

// newTestServer builds a Server over the fake store for the given viewer.
func newTestServer(t *testing.T, user *bloguser.User) testEnv {
	t.Helper()
	fs := newFakeStore()
	idx := newFakeIndexer()
	srv, err := New(Deps{
		Store:           fs,
		Resolver:        stubResolver{user: user},
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		BaseURL:         "https://example.org",
		SiteName:        "Test Site",
		SiteDescription: "A test blog",
		Indexer:         idx,
		IngestImage: func(_ context.Context, subject string, _ io.Reader) (string, error) {
			return "/media/blog/abc123.webp", nil
		},
	})
	if err != nil {
		t.Fatalf("handler.New: %v", err)
	}
	return testEnv{store: fs, indexer: idx, server: srv}
}

// seedAuthorAndPost creates an author registry row plus one post directly
// in the fake store.
func (e testEnv) seedPost(t *testing.T, authorKey, status, title string, blogID *int64, tags ...string) *store.Post {
	t.Helper()
	ctx := context.Background()
	if _, err := e.store.EnsureAuthor(ctx, authorKey, "author-"+authorKey, "Author "+strings.ToUpper(authorKey)); err != nil {
		t.Fatalf("seed author: %v", err)
	}
	p, err := e.store.CreatePost(ctx, "post", status, store.PostParams{
		Title: title, BodyMD: "Hello **world**", BodyHTML: "<p>Hello <strong>world</strong></p>",
		AuthorKey: authorKey, BlogID: blogID,
	})
	if err != nil {
		t.Fatalf("seed post: %v", err)
	}
	var ins []store.TagInput
	for _, tg := range tags {
		ins = append(ins, store.TagInput{Slug: tg, Name: tg})
	}
	if len(ins) > 0 {
		if err := e.store.SetPostTags(ctx, p.ID, ins); err != nil {
			t.Fatalf("seed tags: %v", err)
		}
	}
	return p
}
