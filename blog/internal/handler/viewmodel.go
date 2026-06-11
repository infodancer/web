package handler

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	ui "github.com/infodancer/ui"

	"github.com/infodancer/web/blog/bloguser"
	"github.com/infodancer/web/blog/store"
)

// feedFormats maps each feed format to its MIME type and URL filename.
var feedFormats = []struct {
	label, mime, file string
}{
	{"RSS", "application/rss+xml", "feed.xml"},
	{"Atom", "application/atom+xml", "feed.atom"},
	{"JSON", "application/feed+json", "feed.json"},
}

// BaseView is the envelope every full page template receives. The embedded
// ui.DocumentData carries the chrome that base.gohtml renders through
// ui/document — host-supplied via Deps.Chrome or the module default. The
// remaining fields are per-page context the content/title/head blocks
// reference.
type BaseView struct {
	ui.DocumentData

	// BasePath is the module's mount point; templates prefix internal links
	// with it.
	BasePath string

	// User is the resolved visitor, or nil for anonymous.
	User *bloguser.User

	// SignInURL is the host login URL, surfaced to anonymous visitors.
	SignInURL string

	// FeedLinks is the head block's feed autodiscovery markup for the
	// current scope, pre-built (and escaped) by feedLinks.
	FeedLinks template.HTML

	// EditorHead carries mdedit's <head> tags on editor pages only.
	EditorHead template.HTML

	// Page is the page-specific payload.
	Page any
}

// baseView builds a [BaseView] from the request, deps, and a page payload.
func (s *Server) baseView(r *http.Request, page any) BaseView {
	user := bloguser.UserFromContext(r.Context())

	var doc ui.DocumentData
	if s.chrome != nil {
		doc = s.chrome(r)
	} else {
		doc = s.defaultChrome()
	}
	if doc.Meta.SiteName == "" {
		doc.Meta.SiteName = s.siteName
	}
	if doc.Meta.Description == "" {
		doc.Meta.Description = s.siteDesc
	}
	// blog's own layout CSS rides in ExtraCSS after ui's base sheets.
	doc.ExtraCSS = append(doc.ExtraCSS, s.basePath+"/static/css/blog.css")

	return BaseView{
		DocumentData: doc,
		BasePath:     s.basePath,
		User:         user,
		SignInURL:    s.signInURL,
		FeedLinks:    s.feedLinks("", "", ""),
		Page:         page,
	}
}

// defaultChrome is the standalone chrome used when no host Chrome hook is
// set: ui CSS served from the module's own /static/ui mount, and a brand
// nav/footer from SiteName/HomeURL.
func (s *Server) defaultChrome() ui.DocumentData {
	assetBase := s.basePath + "/static/ui"
	home := s.homeURL
	if home == "" {
		home = "/"
	}
	return ui.DocumentData{
		AssetBase: assetBase,
		HeadTags:  ui.HeadTags(assetBase),
		Meta:      ui.Meta{SiteName: s.siteName},
		Nav:       ui.NavData{BrandText: s.siteName, BrandURL: home, SignInURL: s.signInURL},
		Footer:    ui.FooterData{BrandText: s.siteName, BrandURL: home},
	}
}

// feedLinks builds the autodiscovery <link rel="alternate"> markup for a
// scope. The combined feeds are always advertised; authorSlug and blogSlug
// add their scoped feeds, labeled with scopeName. Built in Go (titles
// HTML-escaped, every other part module-controlled) so the MIME types
// don't pick up entity escaping from the template attribute context.
func (s *Server) feedLinks(scopeName, authorSlug, blogSlug string) template.HTML {
	var b strings.Builder
	add := func(title, rel string) {
		for _, f := range feedFormats {
			fmt.Fprintf(&b, "  <link rel=\"alternate\" type=%q title=\"%s (%s)\" href=%q>\n",
				f.mime, template.HTMLEscapeString(title), f.label, s.absURL(rel+"/"+f.file))
		}
	}
	add(s.siteName, "")
	if authorSlug != "" {
		add(s.siteName+" — "+scopeName, "/authors/"+authorSlug)
	}
	if blogSlug != "" {
		add(s.siteName+" — "+scopeName, "/b/"+blogSlug)
	}
	return template.HTML(b.String()) //nolint:gosec // fixed markup; titles escaped above
}

// postCard is one post in a listing context.
type postCard struct {
	BasePath    string
	Slug        string
	Title       string
	Excerpt     string
	AuthorName  string
	AuthorSlug  string
	BlogName    string
	BlogSlug    string
	PublishedAt *time.Time
	Status      string // shown on the authoring list only
}

func (s *Server) cardFromPost(p store.Post) postCard {
	return postCard{
		BasePath:   s.basePath,
		Slug:       p.Slug,
		Title:      p.Title,
		Excerpt:    displayExcerpt(p),
		AuthorName: p.AuthorName,
		AuthorSlug: p.AuthorSlug,
		BlogName:   p.BlogName,
		BlogSlug:   p.BlogSlug,
		PublishedAt: func() *time.Time {
			if p.PublishedAt != nil {
				t := *p.PublishedAt
				return &t
			}
			return nil
		}(),
		Status: p.Status,
	}
}

func (s *Server) cards(posts []store.Post) []postCard {
	out := make([]postCard, 0, len(posts))
	for _, p := range posts {
		out = append(out, s.cardFromPost(p))
	}
	return out
}

// pagination is the prev/next control state for a listing page.
type pagination struct {
	Page    int
	HasPrev bool
	HasNext bool
	PrevURL string
	NextURL string
}

func paginate(basePath string, page, pageSize int, total int64) pagination {
	p := pagination{Page: page}
	if page > 1 {
		p.HasPrev = true
		p.PrevURL = basePath + "?page=" + strconv.Itoa(page-1)
	}
	if int64(page*pageSize) < total {
		p.HasNext = true
		p.NextURL = basePath + "?page=" + strconv.Itoa(page+1)
	}
	return p
}

// pageParam reads the 1-based ?page= query parameter.
func pageParam(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// indexView is the payload for the combined and named-blog index pages.
type indexView struct {
	Heading     string
	Description string
	Blog        *store.Blog // non-nil on named-blog pages
	Posts       []postCard
	Pagination  pagination
	Blogs       []store.Blog // named blogs, for the index sidebar list
}

// postView is the payload for the post detail page.
type postView struct {
	Post       *store.Post
	BodyHTML   template.HTML
	AuthorCard template.HTML // profile-backed byline; empty falls back to text
	CanEdit    bool
	IsDraft    bool
}

// authorsView lists authors with published posts.
type authorsView struct {
	Authors []store.Author
}

// authorView is one author's page.
type authorView struct {
	Author     *store.Author
	Info       *AuthorInfo // profile enrichment; may be nil
	Posts      []postCard
	Pagination pagination
}

// tagsView lists tags in use.
type tagsView struct {
	Tags []store.Tag
}

// tagView is one tag's page.
type tagView struct {
	Tag        *store.Tag
	Posts      []postCard
	Pagination pagination
}

// mineView is the authoring "my posts" list.
type mineView struct {
	Posts []postCard
}

// adminBlogsView is the named-blog management page.
type adminBlogsView struct {
	Blogs  []store.Blog
	Errors []string
}

// displayExcerpt returns the post's excerpt, deriving one from the body
// when the override is empty.
func displayExcerpt(p store.Post) string {
	if p.Excerpt != "" {
		return p.Excerpt
	}
	return deriveExcerpt(p.BodyMD)
}

// deriveExcerpt produces a plain-text summary from markdown: syntax
// characters stripped crudely, whitespace collapsed, cut at a word boundary
// near 200 characters.
func deriveExcerpt(md string) string {
	const maxLen = 200
	repl := strings.NewReplacer(
		"#", "", "*", "", "_", "", "`", "", ">", "", "|", " ",
		"[", "", "]", " ", // "[text](url)" degrades to "text (url)"; the target is dropped below
	)
	s := repl.Replace(md)
	// Drop link targets left as "(http...)" fragments.
	fields := strings.Fields(s)
	kept := fields[:0]
	for _, f := range fields {
		if strings.HasPrefix(f, "(http") || strings.HasPrefix(f, "(/") {
			continue
		}
		kept = append(kept, f)
	}
	s = strings.Join(kept, " ")
	if len(s) <= maxLen {
		return s
	}
	cut := s[:maxLen]
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return cut + "..."
}
