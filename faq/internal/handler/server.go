package handler

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/infodancer/ui"

	"github.com/infodancer/web/faq/faquser"
	"github.com/infodancer/web/faq/internal/render"
	"github.com/infodancer/web/faq/store"
)

// Deps captures everything the handler needs to do its job. The faq
// package constructs this from the public [faq.Config] when it builds
// the module.
type Deps struct {
	// Store is the data-access layer. Required.
	Store *store.Store

	// Templates is the parsed template set. Required.
	Templates *render.Templates

	// Resolver pulls a [*faquser.User] out of the incoming request.
	// Required. Returning (nil, nil) is fine — handlers treat that as
	// anonymous access and continue.
	Resolver faquser.UserResolver

	// Logger is the per-server slog handle. Required.
	Logger *slog.Logger

	// BasePath is the URL prefix the module is mounted at (e.g. "/faq").
	// Used for link generation in templates and for sitemap output.
	BasePath string

	// SiteName, HomeURL, LogoURL drive the default nav/footer chrome (used
	// when Chrome is nil). See [faq.Config] for semantics. All optional.
	SiteName string
	HomeURL  string
	LogoURL  string

	// SignInURL is the host's login URL, used for the anonymous "sign in to
	// post an answer" prompt on open questions. Empty renders plain text.
	SignInURL string

	// Chrome, when set, supplies the full host page chrome per request as a
	// ui.DocumentData rendered through ui/document. nil falls back to the
	// SiteName/HomeURL-derived default (see [Server.baseView]).
	Chrome func(*http.Request) ui.DocumentData

	// PageSize is the default page size for list endpoints.
	PageSize int

	// EditGracePeriod is how long after creation the original author may
	// edit their own post. Admins are unrestricted. See [Server.canEdit].
	EditGracePeriod time.Duration

	// Static is the filesystem for the module's bundled static assets
	// (faq.css and friends). Optional in tests; required for the
	// faq.New construction path because base.gohtml links to
	// {{.BasePath}}/static/faq.css.
	Static fs.FS

	// CSRFDisabled, when true, short-circuits the same-origin CSRF
	// guard. Use only when the host application already enforces an
	// equivalent guard at a higher layer.
	CSRFDisabled bool
}

// Server is the HTTP layer of the faq module. Construct one with [New]
// and mount its [http.Handler] via [Server.Routes].
type Server struct {
	deps         Deps
	mux          *http.ServeMux
	csrfDisabled bool
}

// New validates deps and returns a wired Server. Returns an error if a
// required dep is missing.
func New(d Deps) (*Server, error) {
	if d.Store == nil {
		return nil, errors.New("handler: Deps.Store is required")
	}
	if d.Templates == nil {
		return nil, errors.New("handler: Deps.Templates is required")
	}
	if d.Resolver == nil {
		return nil, errors.New("handler: Deps.Resolver is required")
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.BasePath == "" {
		d.BasePath = "/faq"
	}
	if d.HomeURL == "" {
		d.HomeURL = "/"
	}
	if d.PageSize <= 0 {
		d.PageSize = 25
	}
	if d.EditGracePeriod == 0 {
		d.EditGracePeriod = 10 * time.Minute
	}

	s := &Server{deps: d, mux: http.NewServeMux(), csrfDisabled: d.CSRFDisabled}
	s.registerRoutes()
	return s, nil
}

// Routes returns the underlying [http.Handler] wrapped in the body-
// size limit and same-origin CSRF guard. Mount it at [Deps.BasePath]
// in the host application's router using http.StripPrefix so the
// module sees paths relative to its mount point.
func (s *Server) Routes() http.Handler {
	return s.limitBody(s.csrfGuard(s.subsiteDispatch(s.mux)))
}

// maxBodyBytes is the per-request body cap. Sized to comfortably hold
// the largest legitimate POST: a 30k-char body, a 150-char title, a
// short tags field, plus form-encoding overhead. Anything larger is
// either malicious or a bug; reject with 413 before ParseForm reads
// the whole thing into memory.
const maxBodyBytes = 64 * 1024

// limitBody wraps r.Body in [http.MaxBytesReader] so subsequent
// ParseForm calls can't be used to exhaust memory. Read-side handlers
// don't care about a body cap, but applying it uniformly is simpler
// than threading per-route limits and costs nothing on GET requests.
func (s *Server) limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// parseForm calls r.ParseForm() with the assumption that limitBody (in
// [Server.Routes]) has already wrapped r.Body in a [http.MaxBytesReader].
// Centralizing the call here lets gosec G120 be suppressed in one
// place rather than at every handler call site, and documents the
// "no, the body really is bounded" invariant.
//
//nolint:gosec // G120 — body bounded by Server.limitBody middleware
func parseForm(r *http.Request) error {
	return r.ParseForm()
}

// postFormValue is the matching wrapper for r.PostFormValue. Same
// rationale: the call is safe because limitBody bounded the body
// before any handler ran.
//
//nolint:gosec // G120 — body bounded by Server.limitBody middleware
func postFormValue(r *http.Request, name string) string {
	return r.PostFormValue(name)
}

// registerRoutes wires the M3 read-path endpoints. Each handler maps to
// the matching task in DESIGN.md §17. Write-path endpoints (M4) and
// admin (M5) are appended here as they land.
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /q", s.questionsList)
	s.mux.HandleFunc("GET /q/new", s.askForm)
	s.mux.HandleFunc("POST /q", s.askSubmit)
	s.mux.HandleFunc("GET /q/{id}", s.questionDetail)
	s.mux.HandleFunc("GET /q/{id}/{slug}", s.questionDetail)
	s.mux.HandleFunc("GET /q/{id}/edit", s.editQuestionForm)
	s.mux.HandleFunc("PATCH /q/{id}", s.editQuestionSubmit)
	s.mux.HandleFunc("POST /q/{id}/answers", s.postAnswer)
	s.mux.HandleFunc("GET /a/{id}/edit", s.editAnswerForm)
	s.mux.HandleFunc("PATCH /a/{id}", s.editAnswerSubmit)
	s.mux.HandleFunc("POST /a/{id}/vote", s.vote)
	s.mux.HandleFunc("DELETE /a/{id}/vote", s.unvote)
	s.mux.HandleFunc("POST /a/{id}/delete", s.deleteAnswer)
	s.mux.HandleFunc("POST /a/{id}/restore", s.restoreAnswer)
	s.mux.HandleFunc("POST /q/{id}/comments", s.postQuestionComment)
	s.mux.HandleFunc("POST /a/{id}/comments", s.postAnswerComment)
	s.mux.HandleFunc("PATCH /comments/{id}", s.editComment)
	s.mux.HandleFunc("POST /q/{id}/accept", s.accept)
	s.mux.HandleFunc("POST /q/{id}/lock", s.lockQuestion)
	s.mux.HandleFunc("POST /q/{id}/unlock", s.unlockQuestion)
	s.mux.HandleFunc("POST /q/{id}/delete", s.deleteQuestion)
	s.mux.HandleFunc("POST /q/{id}/restore", s.restoreQuestion)
	s.mux.HandleFunc("GET /admin/review", s.adminReviewQueue)
	s.mux.HandleFunc("POST /admin/review/{id}/reviewed", s.adminMarkReviewed)
	s.mux.HandleFunc("GET /admin/deleted", s.adminDeletedQueue)
	s.mux.HandleFunc("GET /admin/subsites", s.adminSubsitesIndex)
	s.mux.HandleFunc("GET /admin/subsites/new", s.adminSubsiteNew)
	s.mux.HandleFunc("POST /admin/subsites", s.adminSubsiteCreate)
	s.mux.HandleFunc("GET /admin/subsites/{id}", s.adminSubsiteEdit)
	s.mux.HandleFunc("POST /admin/subsites/{id}", s.adminSubsiteUpdate)
	s.mux.HandleFunc("POST /admin/subsites/{id}/delete", s.adminSubsiteDelete)
	s.mux.HandleFunc("POST /admin/subsites/{id}/tags", s.adminSubsiteAttachTag)
	s.mux.HandleFunc("POST /admin/subsites/{id}/tags/{tag_id}/delete", s.adminSubsiteDetachTag)
	s.mux.HandleFunc("POST /admin/q/{id}/move", s.adminMoveQuestion)
	s.mux.HandleFunc("POST /admin/q/{id}/subsites", s.adminQuestionIncludeSubsite)
	s.mux.HandleFunc("POST /admin/q/{id}/subsites/{subsite_id}/delete", s.adminQuestionRemoveSubsite)
	s.mux.HandleFunc("POST /admin/subsites/{id}/questions", s.adminSubsiteIncludeBulk)
	s.mux.HandleFunc("POST /admin/subsites/{id}/questions/{question_id}/delete", s.adminSubsiteRemoveQuestion)
	s.mux.HandleFunc("GET /tags", s.tagsIndex)
	s.mux.HandleFunc("GET /tags/{name}", s.tagDetail)
	s.mux.HandleFunc("GET /search", s.search)
	s.mux.HandleFunc("GET /sitemap.xml", s.sitemapIndex)
	// Go 1.22+ ServeMux wildcards must be a whole segment, so the
	// sub-sitemap URLs live one level deeper rather than as
	// /sitemap-N.xml. The on-the-wire URL is /sitemap/N.xml; the
	// handler validates N.
	s.mux.HandleFunc("GET /sitemap/{name}", s.sitemapPart)

	if s.deps.Static != nil {
		s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(s.deps.Static)))
	}

	// M6a — global directory at the exact root path. Subsite-prefixed
	// routes (everything that starts with a non-reserved first segment)
	// are dispatched by [Server.subsiteDispatch] in front of the mux —
	// see Routes(). Registering /{subsite}/q here would conflict with
	// /q/{id} on the literal path /q/q, which ServeMux 1.22 rejects as
	// ambiguous even though our reserved-slug check makes the
	// ambiguity unreachable in practice.
	s.mux.HandleFunc("GET /{$}", s.directoryLanding)

	// Catch-all 404 for everything else under the base path.
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.notFound(w, r)
	})
}

// ----- helpers -----

// resolveUser fetches the current user from the resolver and stashes it
// in the request context. A resolver error means an invalid token and
// produces a 401; (nil, nil) is fine and continues anonymously.
func (s *Server) resolveUser(w http.ResponseWriter, r *http.Request) (*faquser.User, bool) {
	u, err := s.deps.Resolver.Resolve(r.Context(), r)
	if err != nil {
		s.deps.Logger.Info("handler: resolver error", "err", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	return u, true
}

// withUser returns a request whose context carries u (which may be nil).
func withUser(r *http.Request, u *faquser.User) *http.Request {
	return r.WithContext(faquser.ContextWithUser(r.Context(), u))
}

// notFound renders the 404 page. Falls back to plain text if the
// template is missing (e.g. tests skip 404 customization).
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	if s.deps.Templates.Has("pages/not_found.gohtml") {
		w.WriteHeader(http.StatusNotFound)
		s.deps.Templates.Render(w, "pages/not_found.gohtml", s.baseView(r, nil))
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

// internalError renders a 500 with the canonical template, logging the
// underlying error. Used by handlers when an unexpected DB failure
// occurs.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, where string, err error) {
	s.deps.Logger.Error("handler: internal error", "where", where, "err", err)
	if s.deps.Templates.Has("pages/error.gohtml") {
		w.WriteHeader(http.StatusInternalServerError)
		s.deps.Templates.Render(w, "pages/error.gohtml", s.baseView(r, nil))
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// pathID parses a {id} path value as int64 and emits 404 if it doesn't
// look like a positive integer. Returns (id, true) on success.
func (s *Server) pathID(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	raw := strings.TrimSpace(r.PathValue(key))
	if raw == "" {
		s.notFound(w, r)
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		s.notFound(w, r)
		return 0, false
	}
	return id, true
}

// queryInt returns a clamped integer parsed from a query parameter, with
// a fallback when the param is absent or malformed. Used for paging
// offsets and sizes.
func queryInt(r *http.Request, name string, fallback, minVal, maxVal int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if n < minVal {
		return minVal
	}
	if n > maxVal {
		return maxVal
	}
	return n
}

// pageBounds returns (limit, offset) for a list endpoint, derived from
// the request's `page` query parameter and the server's configured
// default page size.
func (s *Server) pageBounds(r *http.Request) (int32, int32, int) {
	page := queryInt(r, "page", 1, 1, 1_000_000)
	size := queryInt(r, "size", s.deps.PageSize, 1, 100)
	offset := (page - 1) * size
	return int32(size), int32(offset), page //nolint:gosec // bounds-checked above
}
