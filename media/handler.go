package media

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// objectStore is the small metadata interface the handler needs. The Postgres
// [Store] satisfies it; tests use an in-memory fake. (Conventions: handlers
// depend on an interface, not the concrete store.)
type objectStore interface {
	Put(ctx context.Context, m ObjectMeta) error
	Lookup(ctx context.Context, hash string) (ObjectMeta, bool, error)
}

// Handler serves the media HTTP contract: POST an image to upload, GET its
// content address to retrieve it. Build one with [NewHandler] and mount
// [Handler.Routes] under a host-chosen path.
type Handler struct {
	store objectStore
	blobs BlobStore
	cfg   Config
}

// NewHandler wires a metadata store, a blob store, and config into a handler.
func NewHandler(store objectStore, blobs BlobStore, cfg Config) *Handler {
	return &Handler{store: store, blobs: blobs, cfg: cfg}
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

	owner := ""
	if h.cfg.Authorize != nil {
		var ok bool
		owner, ok = h.cfg.Authorize(r)
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
	if err := h.blobs.Put(ctx, processed.SHA256, processed.ContentType, processed.Bytes); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	meta := ObjectMeta{
		Hash:        processed.SHA256,
		ContentType: processed.ContentType,
		Width:       processed.Width,
		Height:      processed.Height,
		ByteSize:    int64(len(processed.Bytes)),
		Owner:       owner,
		CreatedAt:   time.Now().UTC(),
	}
	if err := h.store.Put(ctx, meta); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(uploadResponse{
		URL:    h.objectURL(processed.SHA256, processed.ContentType),
		Width:  processed.Width,
		Height: processed.Height,
	})
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	hash := hashFromFile(r.PathValue("file"))
	if !looksLikeHash(hash) {
		http.NotFound(w, r)
		return
	}

	data, contentType, err := h.blobs.Get(r.Context(), hash)
	if err != nil {
		if errors.Is(err, ErrBlobNotFound) {
			http.NotFound(w, r)
			return
		}
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
	_, _ = w.Write(data)
}

// objectURL builds the public URL for a stored object.
func (h *Handler) objectURL(hash, contentType string) string {
	base := strings.TrimRight(h.cfg.PublicBaseURL, "/")
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
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
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
