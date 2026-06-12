package integration

import (
	"context"
	"errors"
	"net/http"
	"testing"

	blog "github.com/infodancer/web/blog"
	"github.com/infodancer/web/blog/bloguser"
	blogstore "github.com/infodancer/web/blog/store"
	"github.com/infodancer/web/contact/submissions"
)

// blogStubResolver is an always-anonymous resolver -- enough to construct
// the blog module for migration and store checks.
type blogStubResolver struct{}

func (blogStubResolver) Resolve(context.Context, *http.Request) (*bloguser.User, error) {
	return nil, nil
}

func newBlogModule(t *testing.T) (*blog.Module, context.Context) {
	t.Helper()
	ctx := context.Background()
	pool := newTestPool(t)
	m, err := blog.New(blog.Config{DB: pool, Resolver: blogStubResolver{}, AutoMigrate: true})
	if err != nil {
		t.Fatalf("blog new: %v", err)
	}
	for _, table := range []string{"blog_post_tags", "blog_tags", "blog_posts", "blog_authors", "blog_blogs"} {
		if _, err := pool.Exec(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("reset %s: %v", table, err)
		}
	}
	return m, ctx
}

// TestBlogCoexistsInSharedDatabase verifies the blog component migrates into
// the same database as the others without colliding: its prefixed tables and
// its dedicated goose version table sit alongside contact's.
func TestBlogCoexistsInSharedDatabase(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	if err := submissions.Migrate(ctx, pool); err != nil {
		t.Fatalf("contact migrate: %v", err)
	}
	m, err := blog.New(blog.Config{DB: pool, Resolver: blogStubResolver{}})
	if err != nil {
		t.Fatalf("blog new: %v", err)
	}
	if err := m.Migrate(ctx); err != nil {
		t.Fatalf("blog migrate: %v", err)
	}

	for _, table := range []string{
		"contact_submissions",
		"blog_blogs", "blog_authors", "blog_posts", "blog_tags", "blog_post_tags",
		"contact_goose_db_version", "blog_goose_db_version",
	} {
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = $1)", table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %q: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %q to exist", table)
		}
	}
}

// TestBlogAuthorRegistryPostgres exercises EnsureAuthor's create/refresh and
// slug-disambiguation behavior against the real unique constraints.
func TestBlogAuthorRegistryPostgres(t *testing.T) {
	m, ctx := newBlogModule(t)
	s := m.Store()

	a1, err := s.EnsureAuthor(ctx, "u1", "matthew", "Matthew")
	if err != nil {
		t.Fatalf("ensure u1: %v", err)
	}
	if a1.Slug != "matthew" {
		t.Errorf("u1 slug = %q", a1.Slug)
	}

	// A second user wanting the same slug gets a suffixed one.
	a2, err := s.EnsureAuthor(ctx, "u2", "matthew", "Other Matthew")
	if err != nil {
		t.Fatalf("ensure u2: %v", err)
	}
	if a2.Slug != "matthew-2" {
		t.Errorf("u2 slug = %q, want matthew-2", a2.Slug)
	}

	// Refreshing the first user's display name keeps the slug stable.
	a1b, err := s.EnsureAuthor(ctx, "u1", "matthew", "Matthew J. Hunter")
	if err != nil {
		t.Fatalf("refresh u1: %v", err)
	}
	if a1b.Slug != "matthew" || a1b.DisplayName != "Matthew J. Hunter" {
		t.Errorf("refresh = %q/%q", a1b.Slug, a1b.DisplayName)
	}
}

// TestBlogPostLifecyclePostgres walks a post through create -> publish ->
// unpublish against the real schema: slug dedupe, published_at stamping and
// preservation, listing filters, and tag round-trips.
func TestBlogPostLifecyclePostgres(t *testing.T) {
	m, ctx := newBlogModule(t)
	s := m.Store()

	if _, err := s.EnsureAuthor(ctx, "u1", "alice", "Alice"); err != nil {
		t.Fatalf("ensure author: %v", err)
	}
	params := blogstore.PostParams{
		Title: "Hello World", BodyMD: "hi", BodyHTML: "<p>hi</p>", AuthorKey: "u1",
	}

	p1, err := s.CreatePost(ctx, "hello-world", blogstore.StatusDraft, params)
	if err != nil {
		t.Fatalf("create p1: %v", err)
	}
	if p1.Slug != "hello-world" || p1.PublishedAt != nil {
		t.Errorf("p1 = %q published %v", p1.Slug, p1.PublishedAt)
	}

	// Same base slug dedupes with a numeric suffix.
	p2, err := s.CreatePost(ctx, "hello-world", blogstore.StatusPublished, params)
	if err != nil {
		t.Fatalf("create p2: %v", err)
	}
	if p2.Slug != "hello-world-2" {
		t.Errorf("p2 slug = %q, want hello-world-2", p2.Slug)
	}
	if p2.PublishedAt == nil {
		t.Error("published create did not stamp published_at")
	}

	// Tags round-trip and gate the tag listing on published posts.
	if err := s.SetPostTags(ctx, p2.ID, []blogstore.TagInput{{Slug: "osr", Name: "OSR"}, {Slug: "meta", Name: "meta"}}); err != nil {
		t.Fatalf("set tags: %v", err)
	}
	got, err := s.GetPostByID(ctx, p2.ID)
	if err != nil {
		t.Fatalf("get p2: %v", err)
	}
	if len(got.Tags) != 2 {
		t.Errorf("tags = %+v", got.Tags)
	}
	byTag, total, err := s.ListPublishedByTag(ctx, "osr", 10, 0)
	if err != nil || total != 1 || len(byTag) != 1 {
		t.Errorf("by tag: %v total=%d n=%d", err, total, len(byTag))
	}

	// Only the published post lists publicly; the author page agrees.
	listed, total, err := s.ListPublished(ctx, 10, 0)
	if err != nil || total != 1 || len(listed) != 1 || listed[0].ID != p2.ID {
		t.Fatalf("list published: %v total=%d", err, total)
	}
	if listed[0].AuthorSlug != "alice" {
		t.Errorf("author join: %q", listed[0].AuthorSlug)
	}

	// Publish p1, then unpublish: published_at must survive the round trip.
	pub, err := s.SetPostStatus(ctx, p1.ID, blogstore.StatusPublished)
	if err != nil || pub.PublishedAt == nil {
		t.Fatalf("publish p1: %v %v", err, pub)
	}
	firstPub := *pub.PublishedAt
	unpub, err := s.SetPostStatus(ctx, p1.ID, blogstore.StatusDraft)
	if err != nil {
		t.Fatalf("unpublish p1: %v", err)
	}
	if unpub.PublishedAt == nil || !unpub.PublishedAt.Equal(firstPub) {
		t.Errorf("published_at not preserved: %v vs %v", unpub.PublishedAt, firstPub)
	}

	// Soft delete hides the post from every read.
	if err := s.SoftDeletePost(ctx, p2.ID); err != nil {
		t.Fatalf("delete p2: %v", err)
	}
	if _, err := s.GetPostByID(ctx, p2.ID); !errors.Is(err, blogstore.ErrNotFound) {
		t.Errorf("deleted post readable: %v", err)
	}
	_, total, err = s.ListPublished(ctx, 10, 0)
	if err != nil || total != 0 {
		t.Errorf("deleted post still counted: %v total=%d", err, total)
	}
}

// TestBlogNamedBlogsPostgres covers named-blog CRUD and the in-use delete
// guard against the real foreign keys.
func TestBlogNamedBlogsPostgres(t *testing.T) {
	m, ctx := newBlogModule(t)
	s := m.Store()

	b, err := s.CreateBlog(ctx, "admin-notes", "Admin Notes", "news")
	if err != nil {
		t.Fatalf("create blog: %v", err)
	}
	if _, err := s.CreateBlog(ctx, "admin-notes", "Dup", ""); !errors.Is(err, blogstore.ErrSlugTaken) {
		t.Errorf("dup slug err = %v", err)
	}

	if _, err := s.EnsureAuthor(ctx, "u1", "alice", "Alice"); err != nil {
		t.Fatalf("ensure author: %v", err)
	}
	p, err := s.CreatePost(ctx, "in-stream", blogstore.StatusPublished, blogstore.PostParams{
		Title: "In Stream", BodyMD: "x", BodyHTML: "<p>x</p>", AuthorKey: "u1", BlogID: &b.ID,
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	byBlog, total, err := s.ListPublishedByBlog(ctx, "admin-notes", 10, 0)
	if err != nil || total != 1 || len(byBlog) != 1 || byBlog[0].BlogSlug != "admin-notes" {
		t.Errorf("by blog: %v total=%d", err, total)
	}

	if err := s.DeleteBlog(ctx, b.ID); !errors.Is(err, blogstore.ErrBlogInUse) {
		t.Errorf("in-use delete err = %v", err)
	}
	if err := s.SoftDeletePost(ctx, p.ID); err != nil {
		t.Fatalf("delete post: %v", err)
	}
	if err := s.DeleteBlog(ctx, b.ID); err != nil {
		t.Errorf("delete empty blog: %v", err)
	}
}
