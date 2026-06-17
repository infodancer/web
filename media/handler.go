package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// objectStore is the small metadata interface the handler needs. The Postgres
// [Store] satisfies it; tests use an in-memory fake. (Conventions: handlers
// depend on an interface, not the concrete store.)
type objectStore interface {
	Put(ctx context.Context, m ObjectMeta) error
	Lookup(ctx context.Context, category, hash string) (ObjectMeta, bool, error)
	PutVariant(ctx context.Context, v Variant) error
}

// Handler serves the media HTTP contract: POST an image to upload, GET its
// content address to retrieve it. Build one with [NewHandler] and mount
// [Handler.Routes] under a host-chosen path.
type Handler struct {
	store     objectStore
	blobs     BlobStore
	cfg       Config
	log       *slog.Logger
	requestID func(ctx context.Context) string
}

// NewHandler wires a metadata store, a blob store, and config into a handler.
// cfg.Category is the server-side bucket this handler stores under; it must be
// a non-empty valid category (see [ValidCategory]). A host registers one
// handler per category over a single shared store.
func NewHandler(store objectStore, blobs BlobStore, cfg Config) (*Handler, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("media: NewHandler: %w", err)
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	requestID := cfg.RequestID
	if requestID == nil {
		requestID = func(context.Context) string { return "" }
	}
	return &Handler{store: store, blobs: blobs, cfg: cfg, log: log, requestID: requestID}, nil
}

// logFailure records a storage failure at the HTTP boundary with correlation
// context. It is the single place media logs an error: the blob and metadata
// stores stay silent and return wrapped errors, and the caller writes a
// sanitized response after logging. hash is the object's content address (the
// original or a variant), or "" when none is known yet.
func (h *Handler) logFailure(ctx context.Context, op, hash string, err error) {
	h.log.Error("media: "+op+" failed",
		"err", err,
		"request_id", h.requestID(ctx),
		"category", h.cfg.Category,
		"hash", hash,
	)
}

// validateConfig checks the parts of Config that every storage entry point
// (the HTTP handler and [Ingest]) depends on: a server-side category and
// well-formed thumbnail specs. Callers wrap the error with their own prefix.
func validateConfig(cfg Config) error {
	if cfg.Category == "" || !ValidCategory(cfg.Category) {
		return fmt.Errorf("invalid category %q", cfg.Category)
	}
	if err := validateThumbnailSpecs(cfg.Thumbnails); err != nil {
		return err
	}
	return nil
}

// Routes returns the handler's mux. Mount it under a path with
// [http.StripPrefix]; the patterns are root-relative:
//
//	POST /            upload (multipart field "image")
//	GET  /{file}      serve bytes; {file} is "<hash>" or "<hash>.<ext>"
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /{$}", h.upload)
	mux.HandleFunc("GET /{file}", h.serve)
	return mux
}

// uploadResponse is the success body. mdedit's adapter inserts ![](url).
type uploadResponse struct {
	URL        string         `json:"url"`
	Width      int            `json:"width"`
	Height     int            `json:"height"`
	Thumbnails []thumbVariant `json:"thumbnails,omitempty"`
}

// thumbVariant is one derived size in the upload response, keyed by spec name so
// a caller can pick "thumb" vs "medium". Omitted entirely when no thumbnails are
// configured (back-compat with consumers that only read url).
type thumbVariant struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	// Same-origin guard: reject a cross-site form post outright. CSRF policy
	// otherwise belongs to the host, but this costs nothing and blocks the
	// obvious cross-origin upload.
	if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
		http.Error(w, "cross-origin upload rejected", http.StatusForbidden)
		return
	}

	// Authorize gates the upload and names the owning record (subject). An
	// upload is always tied to a record the user may write to; the host derives
	// it from the request. A nil Authorize is a trusted/dev mount (empty subject).
	subject := ""
	if h.cfg.Authorize != nil {
		var ok bool
		subject, ok = h.cfg.Authorize(r)
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	maxBytes := h.cfg.MaxUploadBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxUploadBytes
	}
	// Bound the whole request body; +1 KiB headroom for multipart framing.
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024)

	file, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, `missing "image" file field`, http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	processed, err := Process(file, h.cfg)
	if err != nil {
		writeProcessError(w, err)
		return
	}

	ctx := r.Context()
	if err := h.blobs.Put(ctx, h.cfg.Category, processed.SHA256, processed.ContentType, processed.Bytes); err != nil {
		h.logFailure(ctx, "blob put", processed.SHA256, err)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	meta := ObjectMeta{
		Category:    h.cfg.Category,
		Hash:        processed.SHA256,
		ContentType: processed.ContentType,
		Width:       processed.Width,
		Height:      processed.Height,
		ByteSize:    int64(len(processed.Bytes)),
		Subject:     subject,
		CreatedAt:   time.Now().UTC(),
	}
	if err := h.store.Put(ctx, meta); err != nil {
		h.logFailure(ctx, "metadata put", processed.SHA256, err)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	// Store each pregenerated thumbnail: its bytes go in the same blob store
	// under their own content address (so the GET path serves them unchanged),
	// and a media_variants row maps the original to the named size.
	var thumbs []thumbVariant
	now := time.Now().UTC()
	for _, t := range processed.Thumbnails {
		if err := h.blobs.Put(ctx, h.cfg.Category, t.SHA256, t.ContentType, t.Bytes); err != nil {
			h.logFailure(ctx, "variant blob put", t.SHA256, err)
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}
		variant := Variant{
			Category:    h.cfg.Category,
			ParentHash:  processed.SHA256,
			Name:        t.Name,
			Hash:        t.SHA256,
			ContentType: t.ContentType,
			Width:       t.Width,
			Height:      t.Height,
			ByteSize:    int64(len(t.Bytes)),
			CreatedAt:   now,
		}
		if err := h.store.PutVariant(ctx, variant); err != nil {
			h.logFailure(ctx, "variant metadata put", t.SHA256, err)
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}
		thumbs = append(thumbs, thumbVariant{
			Name:   t.Name,
			URL:    h.objectURL(t.SHA256, t.ContentType),
			Width:  t.Width,
			Height: t.Height,
		})
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(uploadResponse{
		URL:        h.objectURL(processed.SHA256, processed.ContentType),
		Width:      processed.Width,
		Height:     processed.Height,
		Thumbnails: thumbs,
	})
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	hash := hashFromFile(r.PathValue("file"))
	if !looksLikeHash(hash) {
		http.NotFound(w, r)
		return
	}

	data, contentType, err := h.blobs.Get(r.Context(), h.cfg.Category, hash)
	if err != nil {
		if errors.Is(err, ErrBlobNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logFailure(r.Context(), "blob get", hash, err)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	etag := `"` + hash + `"`
	// Content-addressed: the bytes for a hash never change, so the client may
	// cache forever and a matching ETag is always fresh.
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Disposition", "inline")
	// data is a re-encoded image (Process guarantees it), served with a fixed
	// image Content-Type and X-Content-Type-Options: nosniff — not HTML, so the
	// XSS taint warning does not apply.
	_, _ = w.Write(data) //nolint:gosec // G705: re-encoded image bytes, nosniff + image content-type
}

// objectURL builds the public URL for a stored object from this handler's
// configured base. It delegates to the package-level [objectURL] so the serve
// path and [Ingest] produce byte-identical URLs from one source of truth.
func (h *Handler) objectURL(hash, contentType string) string {
	return objectURL(h.cfg.PublicBaseURL, hash, contentType)
}

// objectURL builds the canonical public URL for a stored object: the base with
// any trailing slash trimmed, then "/<hash><ext>". This is media's URL scheme
// and stays internal — both [Handler] (serving) and [Ingest] (returning) call
// it, so the storage layout and the URL shape cannot drift. Consumers take the
// returned URL; they never construct it.
func objectURL(base, hash, contentType string) string {
	base = strings.TrimRight(base, "/")
	return base + "/" + hash + extFor(contentType)
}

// writeProcessError maps a pipeline error to an HTTP status.
func writeProcessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTooLarge), errors.Is(err, ErrTooManyPixels):
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
	case errors.Is(err, ErrUnsupportedFormat), errors.Is(err, ErrAnimated):
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
	case errors.Is(err, ErrDecode):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	default:
		http.Error(w, "upload failed", http.StatusBadRequest)
	}
}

func extFor(contentType string) string {
	switch contentType {
	case "image/webp":
		return ".webp"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		// No longer produced (PNG re-encodes to WebP), but objects stored
		// before that change still serve under their .png URL.
		return ".png"
	default:
		return ""
	}
}

// hashFromFile strips an optional extension from a "{file}" path segment. A
// sha256 hex string contains no dot, so splitting on the first dot is safe.
func hashFromFile(file string) string {
	if i := strings.IndexByte(file, '.'); i >= 0 {
		return file[:i]
	}
	return file
}

// looksLikeHash reports whether s is a 64-char lowercase hex string.
func looksLikeHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// sameOrigin reports whether the Origin header's host matches the request Host.
func sameOrigin(origin, host string) bool {
	if i := strings.Index(origin, "://"); i >= 0 {
		origin = origin[i+3:]
	}
	return origin == host
}
