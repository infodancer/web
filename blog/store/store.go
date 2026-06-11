// Package store is blog's data layer: a thin domain wrapper over the
// sqlc-generated queries. It translates pgtype values into plain Go types,
// maps database errors to sentinel errors, and owns the slug-uniqueness
// retry loops so handlers never see a raw unique-violation.
package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/blog/db"
)

// Post status values. The schema CHECK constraint is the enforcement; these
// are the only strings the store writes.
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusArchived  = "archived"
)

var (
	// ErrNotFound is returned when the requested row does not exist (or is
	// soft-deleted).
	ErrNotFound = errors.New("blog: not found")
	// ErrSlugTaken is returned when a caller-chosen slug collides and the
	// store was asked not to disambiguate (named-blog admin edits).
	ErrSlugTaken = errors.New("blog: slug already in use")
	// ErrBlogInUse is returned when deleting a named blog that posts still
	// reference.
	ErrBlogInUse = errors.New("blog: named blog still has posts")
)

// Blog is a named blog — one editorial stream within the instance.
type Blog struct {
	ID          int64
	Slug        string
	Name        string
	Description string
	CreatedAt   time.Time
	PostCount   int64
}

// Author is a row of the author registry: cached display identity for a
// user who has authored posts. Identity and roles live with the host.
type Author struct {
	UserKey     string
	Slug        string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	PostCount   int64
}

// Tag is a flat content tag.
type Tag struct {
	ID        int64
	Slug      string
	Name      string
	PostCount int64
}

// Post is a blog post joined with its author registry row and named blog.
type Post struct {
	ID       int64
	Slug     string
	Title    string
	BodyMD   string
	BodyHTML string
	Excerpt  string

	AuthorKey  string
	AuthorSlug string
	AuthorName string

	BlogID   *int64
	BlogSlug string // "" when the post belongs to no named blog
	BlogName string

	Status      string
	ExternalID  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	PublishedAt *time.Time

	Tags []Tag // populated by GetPost*/WithTags helpers, not list queries
}

// PostParams carries the caller-controlled fields for creating or updating
// a post. Body HTML must already be sanitized by the markdown pipeline.
type PostParams struct {
	Title      string
	BodyMD     string
	BodyHTML   string
	Excerpt    string
	AuthorKey  string
	BlogID     *int64
	ExternalID string
}

// TagInput names one tag as the author typed it; the store slugifies
// display names at the handler layer, so Slug arrives normalized.
type TagInput struct {
	Slug string
	Name string
}

// Store wraps the generated queries with domain types.
type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// New builds a Store over the host-owned pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: db.New(pool)}
}

// --- named blogs ---

// CreateBlog inserts a named blog. The slug is caller-chosen (admin form);
// a collision returns ErrSlugTaken.
func (s *Store) CreateBlog(ctx context.Context, slug, name, description string) (*Blog, error) {
	row, err := s.q.CreateBlog(ctx, db.CreateBlogParams{Slug: slug, Name: name, Description: description})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrSlugTaken
		}
		return nil, fmt.Errorf("blog store: create blog: %w", err)
	}
	b := blogFromModel(row)
	return &b, nil
}

// UpdateBlog updates a named blog's slug, name, and description.
func (s *Store) UpdateBlog(ctx context.Context, id int64, slug, name, description string) (*Blog, error) {
	row, err := s.q.UpdateBlog(ctx, db.UpdateBlogParams{ID: id, Slug: slug, Name: name, Description: description})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if isUniqueViolation(err) {
			return nil, ErrSlugTaken
		}
		return nil, fmt.Errorf("blog store: update blog: %w", err)
	}
	b := blogFromModel(row)
	return &b, nil
}

// DeleteBlog removes a named blog, refusing with ErrBlogInUse while any
// non-deleted post references it.
func (s *Store) DeleteBlog(ctx context.Context, id int64) error {
	n, err := s.q.CountPostsReferencingBlog(ctx, pgtype.Int8{Int64: id, Valid: true})
	if err != nil {
		return fmt.Errorf("blog store: delete blog: count: %w", err)
	}
	if n > 0 {
		return ErrBlogInUse
	}
	rows, err := s.q.DeleteBlog(ctx, id)
	if err != nil {
		return fmt.Errorf("blog store: delete blog: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// GetBlogBySlug returns one named blog.
func (s *Store) GetBlogBySlug(ctx context.Context, slug string) (*Blog, error) {
	row, err := s.q.GetBlogBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("blog store: get blog: %w", err)
	}
	b := blogFromModel(row)
	return &b, nil
}

// ListBlogs returns all named blogs with their (non-deleted) post counts.
func (s *Store) ListBlogs(ctx context.Context) ([]Blog, error) {
	rows, err := s.q.ListBlogs(ctx)
	if err != nil {
		return nil, fmt.Errorf("blog store: list blogs: %w", err)
	}
	out := make([]Blog, 0, len(rows))
	for _, r := range rows {
		out = append(out, Blog{
			ID: r.ID, Slug: r.Slug, Name: r.Name, Description: r.Description,
			CreatedAt: r.CreatedAt.Time, PostCount: r.PostCount,
		})
	}
	return out, nil
}

// --- author registry ---

// EnsureAuthor creates or refreshes the registry row for userKey. wantSlug
// is the preferred slug (the profile handle when the host wired one, else a
// slugified display name); on collision with another author the store
// disambiguates with numeric suffixes. An existing row keeps its slug
// unless wantSlug names a different non-empty preference, so author URLs
// stay stable across display-name changes.
func (s *Store) EnsureAuthor(ctx context.Context, userKey, wantSlug, displayName string) (*Author, error) {
	if wantSlug == "" {
		wantSlug = "author"
	}
	existing, err := s.q.GetAuthorByKey(ctx, userKey)
	switch {
	case err == nil:
		if existing.DisplayName == displayName && (existing.Slug == wantSlug || wantSlug == "author") {
			a := authorFromModel(existing)
			return &a, nil
		}
		keep := existing.Slug
		if wantSlug != "author" && wantSlug != existing.Slug {
			keep = wantSlug
		}
		return s.updateAuthorWithSuffixes(ctx, userKey, keep, existing.Slug, displayName)
	case errors.Is(err, pgx.ErrNoRows):
		return s.insertAuthorWithSuffixes(ctx, userKey, wantSlug, displayName)
	default:
		return nil, fmt.Errorf("blog store: ensure author: %w", err)
	}
}

func (s *Store) insertAuthorWithSuffixes(ctx context.Context, userKey, wantSlug, displayName string) (*Author, error) {
	for i := 0; i < 20; i++ {
		try := suffixed(wantSlug, i)
		row, err := s.q.InsertAuthor(ctx, db.InsertAuthorParams{UserKey: userKey, Slug: try, DisplayName: displayName})
		if err == nil {
			a := authorFromModel(row)
			return &a, nil
		}
		if isUniqueViolation(err) {
			// A concurrent insert of the same user wins; adopt it.
			if got, gerr := s.q.GetAuthorByKey(ctx, userKey); gerr == nil {
				a := authorFromModel(got)
				return &a, nil
			}
			continue // slug collision: try the next suffix
		}
		return nil, fmt.Errorf("blog store: insert author: %w", err)
	}
	return nil, fmt.Errorf("blog store: insert author: %w", ErrSlugTaken)
}

func (s *Store) updateAuthorWithSuffixes(ctx context.Context, userKey, wantSlug, currentSlug, displayName string) (*Author, error) {
	for i := 0; i < 20; i++ {
		try := suffixed(wantSlug, i)
		row, err := s.q.UpdateAuthor(ctx, db.UpdateAuthorParams{UserKey: userKey, Slug: try, DisplayName: displayName})
		if err == nil {
			a := authorFromModel(row)
			return &a, nil
		}
		if isUniqueViolation(err) {
			if try == currentSlug {
				continue
			}
			// Keep the stable slug rather than fighting over the preferred one.
			row, err = s.q.UpdateAuthor(ctx, db.UpdateAuthorParams{UserKey: userKey, Slug: currentSlug, DisplayName: displayName})
			if err == nil {
				a := authorFromModel(row)
				return &a, nil
			}
			continue
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("blog store: update author: %w", err)
	}
	return nil, fmt.Errorf("blog store: update author: %w", ErrSlugTaken)
}

// GetAuthorBySlug returns one registry row.
func (s *Store) GetAuthorBySlug(ctx context.Context, slug string) (*Author, error) {
	row, err := s.q.GetAuthorBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("blog store: get author: %w", err)
	}
	a := authorFromModel(row)
	return &a, nil
}

// ListAuthors returns authors having at least one published post.
func (s *Store) ListAuthors(ctx context.Context) ([]Author, error) {
	rows, err := s.q.ListAuthorsWithPublished(ctx)
	if err != nil {
		return nil, fmt.Errorf("blog store: list authors: %w", err)
	}
	out := make([]Author, 0, len(rows))
	for _, r := range rows {
		out = append(out, Author{
			UserKey: r.UserKey, Slug: r.Slug, DisplayName: r.DisplayName,
			CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time, PostCount: r.PostCount,
		})
	}
	return out, nil
}

// --- posts ---

// CreatePost inserts a post with the given status, deriving a unique slug
// from baseSlug (numeric suffixes on collision). Publish status stamps
// published_at.
func (s *Store) CreatePost(ctx context.Context, baseSlug, status string, p PostParams) (*Post, error) {
	if baseSlug == "" {
		baseSlug = "post"
	}
	var publishedAt pgtype.Timestamptz
	if status == StatusPublished {
		publishedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}
	for i := 0; i < 50; i++ {
		try := suffixed(baseSlug, i)
		row, err := s.q.CreatePost(ctx, db.CreatePostParams{
			Slug: try, Title: p.Title, BodyMd: p.BodyMD, BodyHtml: p.BodyHTML,
			Excerpt: p.Excerpt, AuthorKey: p.AuthorKey, BlogID: pgInt8(p.BlogID),
			Status: status, ExternalID: pgText(p.ExternalID), PublishedAt: publishedAt,
		})
		if err == nil {
			return s.GetPostByID(ctx, row.ID)
		}
		if isUniqueViolation(err) {
			continue
		}
		return nil, fmt.Errorf("blog store: create post: %w", err)
	}
	return nil, fmt.Errorf("blog store: create post: %w", ErrSlugTaken)
}

// UpdatePost rewrites the caller-editable fields. The slug is preserved
// unless newSlug is non-empty (admin override), in which case a collision
// returns ErrSlugTaken.
func (s *Store) UpdatePost(ctx context.Context, id int64, newSlug string, p PostParams) (*Post, error) {
	cur, err := s.q.GetPostByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("blog store: update post: %w", err)
	}
	slug := cur.BlogPost.Slug
	if newSlug != "" {
		slug = newSlug
	}
	_, err = s.q.UpdatePost(ctx, db.UpdatePostParams{
		ID: id, Slug: slug, Title: p.Title, BodyMd: p.BodyMD, BodyHtml: p.BodyHTML,
		Excerpt: p.Excerpt, BlogID: pgInt8(p.BlogID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if isUniqueViolation(err) {
			return nil, ErrSlugTaken
		}
		return nil, fmt.Errorf("blog store: update post: %w", err)
	}
	return s.GetPostByID(ctx, id)
}

// SetPostStatus transitions a post's lifecycle status. Publishing stamps
// published_at on first publish and preserves it afterwards.
func (s *Store) SetPostStatus(ctx context.Context, id int64, status string) (*Post, error) {
	_, err := s.q.SetPostStatus(ctx, db.SetPostStatusParams{ID: id, Status: status})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("blog store: set status: %w", err)
	}
	return s.GetPostByID(ctx, id)
}

// SoftDeletePost marks a post deleted.
func (s *Store) SoftDeletePost(ctx context.Context, id int64) error {
	rows, err := s.q.SoftDeletePost(ctx, id)
	if err != nil {
		return fmt.Errorf("blog store: delete post: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// GetPostByID returns one non-deleted post (any status) with its tags.
func (s *Store) GetPostByID(ctx context.Context, id int64) (*Post, error) {
	row, err := s.q.GetPostByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("blog store: get post: %w", err)
	}
	p := postFrom(row.BlogPost, row.AuthorSlug, row.AuthorName, row.BlogSlug, row.BlogName)
	return s.withTags(ctx, &p)
}

// GetPostBySlug returns one non-deleted post (any status) with its tags.
// Visibility of drafts is the handler's decision.
func (s *Store) GetPostBySlug(ctx context.Context, slug string) (*Post, error) {
	row, err := s.q.GetPostBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("blog store: get post: %w", err)
	}
	p := postFrom(row.BlogPost, row.AuthorSlug, row.AuthorName, row.BlogSlug, row.BlogName)
	return s.withTags(ctx, &p)
}

func (s *Store) withTags(ctx context.Context, p *Post) (*Post, error) {
	tags, err := s.q.ListTagsForPost(ctx, p.ID)
	if err != nil {
		return nil, fmt.Errorf("blog store: post tags: %w", err)
	}
	p.Tags = make([]Tag, 0, len(tags))
	for _, t := range tags {
		p.Tags = append(p.Tags, Tag{ID: t.ID, Slug: t.Slug, Name: t.Name})
	}
	return p, nil
}

// ListPublished returns one page of published posts, newest first, with the
// total count of published posts.
func (s *Store) ListPublished(ctx context.Context, limit, offset int) ([]Post, int64, error) {
	rows, err := s.q.ListPublishedPosts(ctx, db.ListPublishedPostsParams{
		PageLimit: pg32(limit), PageOffset: pg32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("blog store: list published: %w", err)
	}
	total, err := s.q.CountPublishedPosts(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("blog store: count published: %w", err)
	}
	out := make([]Post, 0, len(rows))
	for _, r := range rows {
		out = append(out, postFrom(r.BlogPost, r.AuthorSlug, r.AuthorName, r.BlogSlug, r.BlogName))
	}
	return out, total, nil
}

// ListPublishedByBlog returns one page of a named blog's published posts.
func (s *Store) ListPublishedByBlog(ctx context.Context, blogSlug string, limit, offset int) ([]Post, int64, error) {
	rows, err := s.q.ListPublishedPostsByBlog(ctx, db.ListPublishedPostsByBlogParams{
		BlogSlug: blogSlug, PageLimit: pg32(limit), PageOffset: pg32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("blog store: list by blog: %w", err)
	}
	total, err := s.q.CountPublishedPostsByBlog(ctx, blogSlug)
	if err != nil {
		return nil, 0, fmt.Errorf("blog store: count by blog: %w", err)
	}
	out := make([]Post, 0, len(rows))
	for _, r := range rows {
		out = append(out, postFrom(r.BlogPost, r.AuthorSlug, r.AuthorName, r.BlogSlug, r.BlogName))
	}
	return out, total, nil
}

// ListPublishedByAuthor returns one page of an author's published posts.
func (s *Store) ListPublishedByAuthor(ctx context.Context, authorSlug string, limit, offset int) ([]Post, int64, error) {
	rows, err := s.q.ListPublishedPostsByAuthor(ctx, db.ListPublishedPostsByAuthorParams{
		AuthorSlug: authorSlug, PageLimit: pg32(limit), PageOffset: pg32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("blog store: list by author: %w", err)
	}
	total, err := s.q.CountPublishedPostsByAuthor(ctx, authorSlug)
	if err != nil {
		return nil, 0, fmt.Errorf("blog store: count by author: %w", err)
	}
	out := make([]Post, 0, len(rows))
	for _, r := range rows {
		out = append(out, postFrom(r.BlogPost, r.AuthorSlug, r.AuthorName, r.BlogSlug, r.BlogName))
	}
	return out, total, nil
}

// ListPublishedByTag returns one page of published posts carrying a tag.
func (s *Store) ListPublishedByTag(ctx context.Context, tagSlug string, limit, offset int) ([]Post, int64, error) {
	rows, err := s.q.ListPublishedPostsByTag(ctx, db.ListPublishedPostsByTagParams{
		TagSlug: tagSlug, PageLimit: pg32(limit), PageOffset: pg32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("blog store: list by tag: %w", err)
	}
	total, err := s.q.CountPublishedPostsByTag(ctx, tagSlug)
	if err != nil {
		return nil, 0, fmt.Errorf("blog store: count by tag: %w", err)
	}
	out := make([]Post, 0, len(rows))
	for _, r := range rows {
		out = append(out, postFrom(r.BlogPost, r.AuthorSlug, r.AuthorName, r.BlogSlug, r.BlogName))
	}
	return out, total, nil
}

// ListByAuthorKeyAnyStatus returns one page of a user's own posts in every
// status (the "my posts" authoring view), most recently updated first.
func (s *Store) ListByAuthorKeyAnyStatus(ctx context.Context, authorKey string, limit, offset int) ([]Post, error) {
	rows, err := s.q.ListPostsByAuthorKeyAnyStatus(ctx, db.ListPostsByAuthorKeyAnyStatusParams{
		AuthorKey: authorKey, PageLimit: pg32(limit), PageOffset: pg32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("blog store: list own posts: %w", err)
	}
	out := make([]Post, 0, len(rows))
	for _, r := range rows {
		out = append(out, postFrom(r.BlogPost, r.AuthorSlug, r.AuthorName, r.BlogSlug, r.BlogName))
	}
	return out, nil
}

// --- tags ---

// SetPostTags replaces a post's tag set, creating tags on first use.
func (s *Store) SetPostTags(ctx context.Context, postID int64, tags []TagInput) error {
	if err := s.q.ClearPostTags(ctx, postID); err != nil {
		return fmt.Errorf("blog store: clear tags: %w", err)
	}
	for _, t := range tags {
		if t.Slug == "" {
			continue
		}
		row, err := s.q.UpsertTag(ctx, db.UpsertTagParams{Slug: t.Slug, Name: t.Name})
		if err != nil {
			return fmt.Errorf("blog store: upsert tag %q: %w", t.Slug, err)
		}
		if err := s.q.AddPostTag(ctx, db.AddPostTagParams{PostID: postID, TagID: row.ID}); err != nil {
			return fmt.Errorf("blog store: add tag %q: %w", t.Slug, err)
		}
	}
	return nil
}

// TagsForPosts returns the tags of every listed post in one query, keyed
// by post id. Used by feed builders, which want categories without an N+1
// per item.
func (s *Store) TagsForPosts(ctx context.Context, postIDs []int64) (map[int64][]Tag, error) {
	rows, err := s.q.ListTagsForPostIDs(ctx, postIDs)
	if err != nil {
		return nil, fmt.Errorf("blog store: tags for posts: %w", err)
	}
	out := make(map[int64][]Tag)
	for _, r := range rows {
		out[r.PostID] = append(out[r.PostID], Tag{ID: r.ID, Slug: r.Slug, Name: r.Name})
	}
	return out, nil
}

// GetTagBySlug returns one tag.
func (s *Store) GetTagBySlug(ctx context.Context, slug string) (*Tag, error) {
	row, err := s.q.GetTagBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("blog store: get tag: %w", err)
	}
	return &Tag{ID: row.ID, Slug: row.Slug, Name: row.Name}, nil
}

// ListTags returns tags carried by at least one published post.
func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.q.ListTagsWithPublished(ctx)
	if err != nil {
		return nil, fmt.Errorf("blog store: list tags: %w", err)
	}
	out := make([]Tag, 0, len(rows))
	for _, r := range rows {
		out = append(out, Tag{ID: r.ID, Slug: r.Slug, Name: r.Name, PostCount: r.PostCount})
	}
	return out, nil
}

// --- conversions ---

func blogFromModel(m db.BlogBlog) Blog {
	return Blog{ID: m.ID, Slug: m.Slug, Name: m.Name, Description: m.Description, CreatedAt: m.CreatedAt.Time}
}

func authorFromModel(m db.BlogAuthor) Author {
	return Author{
		UserKey: m.UserKey, Slug: m.Slug, DisplayName: m.DisplayName,
		CreatedAt: m.CreatedAt.Time, UpdatedAt: m.UpdatedAt.Time,
	}
}

func postFrom(m db.BlogPost, authorSlug, authorName, blogSlug, blogName string) Post {
	p := Post{
		ID: m.ID, Slug: m.Slug, Title: m.Title, BodyMD: m.BodyMd, BodyHTML: m.BodyHtml,
		Excerpt: m.Excerpt, AuthorKey: m.AuthorKey, AuthorSlug: authorSlug, AuthorName: authorName,
		BlogSlug: blogSlug, BlogName: blogName, Status: m.Status,
		CreatedAt: m.CreatedAt.Time, UpdatedAt: m.UpdatedAt.Time,
	}
	if m.BlogID.Valid {
		v := m.BlogID.Int64
		p.BlogID = &v
	}
	if m.ExternalID.Valid {
		p.ExternalID = m.ExternalID.String
	}
	if m.PublishedAt.Valid {
		t := m.PublishedAt.Time
		p.PublishedAt = &t
	}
	return p
}

func pgInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func pgText(v string) pgtype.Text {
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}

// pg32 bounds a pagination value into int32 range for the generated query
// params. Callers pass small page-derived values; anything outside the
// range clamps rather than wrapping.
func pg32(v int) int32 {
	if v < 0 {
		return 0
	}
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v)
}

// suffixed returns base for attempt 0 and "base-2", "base-3", … after.
func suffixed(base string, attempt int) string {
	if attempt == 0 {
		return base
	}
	return base + "-" + strconv.Itoa(attempt+1)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
