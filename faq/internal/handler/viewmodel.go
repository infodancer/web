package handler

import (
	"net/http"
	"time"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/faquser"
)

// BaseView is the common envelope every full page template receives. The
// embedded ui.DocumentData carries the page chrome (nav, footer, meta, CSS,
// analytics, theme, sidebars) — host-supplied via Deps.Chrome or the module's
// default — which base.gohtml renders through ui/document. The remaining fields
// are the per-page context the content/title/head blocks reference.
type BaseView struct {
	uiassets.DocumentData

	// BasePath is the module's mount point (e.g. "/faq"). Templates
	// prefix internal links with it.
	BasePath string

	// User is the resolved subject for the request, or nil for
	// anonymous traffic.
	User *faquser.User

	// PageTitle is the human-readable page title rendered in <title> by the
	// faq chrome partial's title block. Page handlers set this on the view.
	PageTitle string

	// CanonicalURL, when non-empty, renders as <link rel="canonical"> by the
	// faq chrome partial's head block. Subsite-scoped detail pages set this
	// per DESIGN §18.4 D8.3 so search engines deduplicate imported-question
	// URLs onto the global canonical.
	CanonicalURL string

	// Page-specific payload — the concrete view-model the page template
	// expects.
	Page any
}

// baseView constructs a [BaseView] from the request, the server's deps, and a
// page-specific payload. The chrome comes from the host (Deps.Chrome) when set,
// else a complete module default; either way the module overlays its own
// section nav (Tags / Search / Ask) as the left sidebar, and the current user
// (set by [Server.resolveUser]) is resolved from the request context.
func (s *Server) baseView(r *http.Request, page any) BaseView {
	user := faquser.UserFromContext(r.Context())

	var doc uiassets.DocumentData
	if s.deps.Chrome != nil {
		doc = s.deps.Chrome(r)
	} else {
		doc = s.defaultChrome()
	}
	if doc.Meta.SiteName == "" {
		doc.Meta.SiteName = orFAQ(s.deps.SiteName)
	}
	// Place the FAQ section nav (Tags / Search / Ask) per the host's
	// configuration. Left is the default and survives whatever chrome the host
	// injects; right merges it atop the host's discovery rail so a host with
	// its own right sidebar keeps one column; none lets the host own it.
	switch s.deps.SectionNavPlacement {
	case "none":
		// Host renders the section nav itself; add nothing.
	case "right":
		doc.SidebarRight = mergeSidebarTop(doc.SidebarRight, s.faqSectionSidebar(user))
	default: // "left" and any unrecognized value
		doc.SidebarLeft = s.faqSectionSidebar(user)
	}

	return BaseView{
		DocumentData: doc,
		BasePath:     s.deps.BasePath,
		User:         user,
		Page:         page,
	}
}

// defaultChrome is the standalone chrome used when no host Chrome hook is set:
// the ui CSS + htmx served from the module's own /static/ui mount, and a brand
// nav/footer from SiteName/HomeURL.
func (s *Server) defaultChrome() uiassets.DocumentData {
	d := s.deps
	assetBase := d.BasePath + "/static/ui"
	name := orFAQ(d.SiteName)
	home := d.HomeURL
	if home == "" {
		home = "/"
	}
	return uiassets.DocumentData{
		AssetBase: assetBase,
		HeadTags:  uiassets.HeadTags(assetBase),
		Meta:      uiassets.Meta{SiteName: name},
		Nav:       uiassets.NavData{BrandText: name, BrandURL: home},
		Footer:    uiassets.FooterData{BrandText: name, BrandURL: home},
	}
}

// faqSectionSidebar builds the FAQ section navigation (Tags / Search, plus Ask
// for a signed-in user) as a left-sidebar panel, so it renders consistently
// whatever top-nav chrome the host supplies.
func (s *Server) faqSectionSidebar(user *faquser.User) *uiassets.SidebarData {
	base := s.deps.BasePath
	items := []uiassets.SidebarItem{
		{Label: "Tags", URL: base + "/tags"},
		{Label: "Search", URL: base + "/search"},
	}
	if user != nil {
		items = append(items, uiassets.SidebarItem{Label: "Ask a question", URL: base + "/q/new"})
	}
	return &uiassets.SidebarData{
		Sections: []uiassets.SidebarSection{
			{Key: "faq", Title: "FAQ", Open: true, Items: items},
		},
	}
}

// mergeSidebarTop returns a sidebar with extra's sections placed above base's,
// without mutating either argument. It is used to fold the FAQ section nav into
// the top of a host-supplied right sidebar (SectionNavRight). Either side may
// be nil: a nil base yields extra as-is, a nil extra yields base unchanged.
func mergeSidebarTop(base, extra *uiassets.SidebarData) *uiassets.SidebarData {
	if extra == nil {
		return base
	}
	if base == nil {
		return extra
	}
	sections := make([]uiassets.SidebarSection, 0, len(extra.Sections)+len(base.Sections))
	sections = append(sections, extra.Sections...)
	sections = append(sections, base.Sections...)
	return &uiassets.SidebarData{Sections: sections}
}

// orFAQ returns name or "FAQ" when empty, matching the standalone chrome's
// brand fallback.
func orFAQ(name string) string {
	if name == "" {
		return "FAQ"
	}
	return name
}

// QuestionListView is the view-model for pages/q_list.gohtml. It is also
// the data passed to partials/q_list_body.gohtml on htmx re-renders.
type QuestionListView struct {
	Title     string
	Sort      string // "newest" | "votes" | "unanswered"
	Questions []QuestionCard
	Page      int
	PageSize  int
	Total     int
	HasMore   bool
}

// QuestionCard is the row data for partials/q_card.gohtml. Used by list,
// search, and tag-detail pages.
//
// URL and TagLinks are absolute mount-relative URLs (start with `/`)
// built by [Server.buildCards]. The handler injects the right subsite
// or global prefix at build time, so the partial never has to do URL
// math — relative paths broke when the same card was rendered at
// different depths (global /q vs subsite /{slug}/q vs detail /q/{id}).
type QuestionCard struct {
	ID          int64
	Slug        string
	Title       string
	Excerpt     string
	Tags        []string  // raw tag names (kept for callers that just want the list)
	TagLinks    []TagLink // rendered link targets, parallel to Tags
	URL         string    // absolute URL to the question detail page
	AnswerCount int
	Score       int
	CreatedAt   time.Time
	Imported    bool
	Locked      bool
	Source      *SourceAttribution
}

// TagLink pairs a tag name with the URL it should link to. Built by
// [Server.buildCards] so subsite-scoped lists and global lists can
// share one card partial.
type TagLink struct {
	Name string
	URL  string
}

// QuestionDetailView is the view-model for pages/q_detail.gohtml.
type QuestionDetailView struct {
	Question Question
	Tags     []string
	Answers  []Answer
	Comments []Comment
	Source   *SourceAttribution
	CanEdit  bool
	IsAdmin  bool

	// Authed is true when a signed-in user is viewing. CanAnswer is true when
	// that user may post an answer (signed in and the question is open);
	// CanAccept is true when they may accept an answer (owner or admin, open).
	Authed    bool
	CanAnswer bool
	CanAccept bool

	// AnswerForm is the post-an-answer form model, non-nil exactly when
	// CanAnswer. The detail body renders it under the answer list; when nil on
	// an open question the body shows the sign-in prompt instead.
	AnswerForm *AnswerFormView

	// SignInURL is the host login URL for the anonymous sign-in prompt; empty
	// renders the prompt as plain text.
	SignInURL string

	// CanonicalSubsiteSlug is the slug of the question's canonical subsite
	// per D8.3, or empty when the global /q URL is canonical (the question
	// matches zero or multiple subsites and has no native subsite_id).
	// Handlers build the <link rel="canonical"> target from this.
	CanonicalSubsiteSlug string

	// AdminCurate carries the M6c curation surface — the subsites this
	// question is explicitly pinned into and the ones available to add.
	// Populated only when IsAdmin; nil for everyone else so the template
	// can skip the block.
	AdminCurate *AdminCurateBlock
}

// AdminCurateBlock is the per-question M6c curation surface rendered on
// the detail page when an admin is viewing. Both slices are always
// non-nil (possibly empty) so the template doesn't need nil checks.
type AdminCurateBlock struct {
	Pinned    []SubsiteLink // subsites this question is explicitly pinned into
	Available []SubsiteLink // subsites that could be pinned into (excludes Pinned)
}

// Question is the rendered representation of one faq_questions row.
type Question struct {
	ID               int64
	Slug             string
	Title            string
	BodyHTML         string // already sanitized
	Score            int
	AnswerCount      int
	CreatedAt        time.Time
	EditedAt         *time.Time
	Status           string // "open" | "locked" | "deleted"
	Author           string
	AcceptedAnswerID *int64
	Imported         bool
	LocallyModified  bool
	NeedsReview      bool
}

// Answer is the rendered representation of one faq_answers row.
type Answer struct {
	ID              int64
	QuestionID      int64
	BodyHTML        string
	Score           int
	Accepted        bool
	CreatedAt       time.Time
	EditedAt        *time.Time
	Author          string
	Imported        bool
	LocallyModified bool
	NeedsReview     bool
	Comments        []Comment

	// Vote, when non-nil, renders the interactive up/down vote cell for the
	// current viewer; nil shows the read-only score (anonymous viewers, or the
	// viewer's own answer, which the DB trigger forbids them from voting on).
	Vote *VoteCellView

	// CanAccept is true when the viewer may accept this answer (question owner
	// or admin, on an open question); it renders the accept/unaccept toggle.
	CanAccept bool

	// CanEdit / CanDelete gate the author/admin controls in the answer footer:
	// CanEdit per canEditAs (author within the grace window, or admin),
	// CanDelete for the author on an open question or an admin.
	CanEdit   bool
	CanDelete bool

	// Deleted marks a soft-deleted answer. Only admins ever see one (rendered
	// muted with a Restore control); the list hides it from everyone else.
	Deleted bool
}

// Comment is the rendered representation of one faq_comments row.
type Comment struct {
	ID        int64
	BodyHTML  string
	CreatedAt time.Time
	Author    string
	Imported  bool
}

// SourceAttribution holds the rendered attribution block for an imported
// question/answer/comment.
type SourceAttribution struct {
	Domain      string
	DisplayName string
	License     string
	URL         string // canonical link back to the source post
}

// TagIndexView is the view-model for pages/tag_index.gohtml.
type TagIndexView struct {
	Tags []TagSummary
}

// TagSummary is the row data for partials/tag_list_body.gohtml.
// URL is the absolute link to the tag's detail page (global or
// subsite-scoped depending on the caller).
type TagSummary struct {
	Name          string
	URL           string
	Description   string
	QuestionCount int
}

// TagDetailView is the view-model for pages/tag_questions.gohtml.
type TagDetailView struct {
	Name      string
	Questions []QuestionCard
	Page      int
	PageSize  int
	Total     int
	HasMore   bool
}

// AskQuestionView is the view-model for pages/q_new.gohtml. ActionURL
// is where the form POSTs to (`{BasePath}/q` for global asks,
// `{BasePath}/{subsite}/ask` for subsite-scoped asks). Heading is the
// rendered <h1> text.
type AskQuestionView struct {
	ActionURL string
	Heading   string
	Title     string
	Body      string
	Tags      string
	Errors    []string
}

// EditQuestionView is the view-model for partials/q_edit_form.gohtml.
type EditQuestionView struct {
	ID     int64
	Title  string
	Body   string
	Tags   string
	Errors []string
}

// AnswerFormView is the view-model for the inline answer form on a
// question detail page (M4.5).
type AnswerFormView struct {
	QuestionID int64
	Body       string
	Errors     []string
}

// EditAnswerView is the view-model for partials/answer_edit_form.gohtml
// (M4.5).
type EditAnswerView struct {
	ID         int64
	QuestionID int64
	Body       string
	Errors     []string
}

// CommentFormView is the view-model for the inline comment form
// (M4.7). ParentType is "q" or "a".
type CommentFormView struct {
	ParentType string
	ParentID   int64
	Body       string
	Errors     []string
}

// VoteCellView is the view-model for partials/vote_cell.gohtml — the
// score + up/down arrow cluster on a single answer. UserVote is "up",
// "down", or "" (no vote).
type VoteCellView struct {
	AnswerID int64
	Score    int
	UserVote string
}

// AdminReviewView is the view-model for pages/admin_review.gohtml and
// partials/admin_review_body.gohtml (M5).
type AdminReviewView struct {
	Questions      []QuestionCard
	Sources        []AdminSourceOption
	SelectedSource int64 // 0 = no filter
	Page           int
	PageSize       int
	Total          int
	HasMore        bool
}

// AdminSourceOption is one entry in the review-queue source filter
// dropdown.
type AdminSourceOption struct {
	ID          int64
	DisplayName string
	Selected    bool
}

// AdminDeletedView is the view-model for pages/admin_deleted.gohtml
// and partials/admin_deleted_body.gohtml (M5).
type AdminDeletedView struct {
	Questions []QuestionCard
	Page      int
	PageSize  int
	Total     int
	HasMore   bool
}

// AdminSubsitesView is the view-model for pages/admin_subsites.gohtml.
// One row per subsite plus a count, plus a link to the edit page.
type AdminSubsitesView struct {
	Subsites []AdminSubsiteRow
}

// AdminSubsiteRow is a single row on the admin subsite index.
type AdminSubsiteRow struct {
	ID            int64
	Slug          string
	DisplayName   string
	Description   string
	QuestionCount int
	EditURL       string
	PublicURL     string
}

// AdminSubsiteFormView backs both the create form
// (pages/admin_subsite_new.gohtml) and the edit form
// (pages/admin_subsite_edit.gohtml). ID == 0 means create; non-zero
// means edit.
type AdminSubsiteFormView struct {
	ID                int64
	ActionURL         string
	Heading           string
	Slug              string
	DisplayName       string
	Description       string
	TagRules          []AdminSubsiteTagRow      // empty on create
	ExplicitQuestions []AdminSubsiteQuestionRow // M6c — currently pinned questions; empty on create
	BulkBadTokens     []string                  // M6c — non-numeric tokens from the last bulk submission
	BulkMissingIDs    []string                  // M6c — numeric IDs from the last bulk submission that didn't match a row
	Errors            []string
}

// AdminSubsiteTagRow is one attached tag rule on the edit page.
type AdminSubsiteTagRow struct {
	TagID         int64
	Name          string
	QuestionCount int64 // global tag count
}

// AdminSubsiteQuestionRow is one pinned-question row on the subsite
// admin edit page (M6c).
type AdminSubsiteQuestionRow struct {
	ID    int64
	Title string
	Slug  string
	URL   string
}

// SearchView is the view-model for pages/search.gohtml.
type SearchView struct {
	Query    string
	Results  []QuestionCard
	Page     int
	PageSize int
	Total    int
	HasMore  bool
}

// DirectoryView is the view-model for pages/directory.gohtml — the
// module's `/` landing page. Per DESIGN.md §18.4 D8.5 it is browse-
// driven: a card per subsite plus a recent-questions strip across the
// global corpus.
type DirectoryView struct {
	Subsites []SubsiteSummary
	Recent   []DirectoryRecent
}

// SubsiteSummary is one card on the directory landing.
type SubsiteSummary struct {
	Slug          string
	DisplayName   string
	Description   string // markdown source; template renders it
	QuestionCount int
}

// DirectoryRecent is one row of the recent-questions strip. Lighter
// than [QuestionCard]: the directory strip is a discovery aid, not a
// full list, so we skip tags, score, and answer counts.
type DirectoryRecent struct {
	ID        int64
	Slug      string
	Title     string
	URL       string
	CreatedAt time.Time
	Imported  bool
	Locked    bool
}

// SubsiteLandingView is the view-model for pages/subsite_landing.gohtml.
// Per DESIGN.md §18.4 D8.5 each subsite landing renders the subsite's
// description as Markdown plus a recent-questions strip plus its top
// tags. Empty description still gets the strip and tag chips — we do
// not redirect to /{subsite}/q on empty description.
type SubsiteLandingView struct {
	Slug            string
	DisplayName     string
	Description     string // markdown source; template renders it
	QuestionCount   int
	Recent          []DirectoryRecent
	TopTags         []TagLink
	AskURL          string
	AllQuestionsURL string
}

// SubsiteQuestionListView is the view-model for the subsite-scoped /q
// list. Differs from [QuestionListView] only in that the sort-link
// URLs already include the subsite prefix and the page title names the
// subsite.
type SubsiteQuestionListView struct {
	Subsite   SubsiteSummary // for breadcrumbs + chrome
	Title     string
	Sort      string
	BaseURL   string // {basePath}/{subsite} — prefix for sort links and pagination
	Questions []QuestionCard
	Page      int
	PageSize  int
	Total     int
	HasMore   bool
}

// SubsiteDetailView wraps a [QuestionDetailView] with the subsite
// chrome and the cross-subsite "see also" block (DESIGN.md §18.4 D8.6).
// CanonicalURL is set per D8.3: imports canonicalize to the global URL,
// natives to their own subsite URL.
type SubsiteDetailView struct {
	Subsite      SubsiteSummary
	Detail       QuestionDetailView
	SeeAlso      []SubsiteLink // other subsites this question also belongs to
	CanonicalURL string
}

// SubsiteLink is a small link tuple used by the see-also footer and
// the M6c admin curation block. ID is populated by callers that need
// it for form actions (e.g. POST to a subsite ID); see-also rendering
// can ignore it.
type SubsiteLink struct {
	ID          int64
	Slug        string
	DisplayName string
	URL         string
}
