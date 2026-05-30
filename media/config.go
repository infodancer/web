package media

import "net/http"

// Config tunes the upload pipeline and serving. The pipeline ([Process]) reads
// only MaxUploadBytes and MaxPixels; the remaining fields are used by the HTTP
// handler. A zero Config is valid — the pipeline applies its defaults.
type Config struct {
	// MaxUploadBytes caps the accepted upload size. Zero uses the default
	// (10 MiB).
	MaxUploadBytes int64

	// MaxPixels caps width*height to refuse decompression bombs before a full
	// decode. Zero uses the default (25 MP).
	MaxPixels int

	// PublicBaseURL is the externally-visible prefix the handler builds object
	// URLs from (e.g. "/media" for same-origin, or
	// "https://cdn.example.com/media" to front the bytes with a CDN or a
	// separate origin for defense in depth). Trailing slash optional.
	PublicBaseURL string

	// Authorize gates uploads. It runs before any bytes are processed and
	// returns the owner id to record plus whether the request may upload.
	// Authentication and authorization policy belong to the host; when nil,
	// uploads are open (suitable only for a trusted/dev mount).
	Authorize func(r *http.Request) (owner string, ok bool)
}
