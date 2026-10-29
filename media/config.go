package media

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
)

// FitMode selects how a [ThumbnailSpec]'s box is applied when the source aspect
// ratio differs from the box. Neither mode ever upscales.
type FitMode int

const (
	// FitInside scales the source down to fit entirely within MaxWidth×MaxHeight
	// with the aspect ratio preserved, so the output dimensions vary by source.
	// A source already within the box is left at its own size. This is the zero
	// value, so an unset Fit means FitInside.
	FitInside FitMode = iota
	// FitCover scales the source to cover the box, then center-crops to exactly
	// MaxWidth×MaxHeight — every thumbnail is identical dimensions (a clean grid)
	// at the cost of cropping the long edge. A source smaller than the box is not
	// upscaled: it is cropped to the box's aspect ratio at native scale.
	FitCover
)

// String renders a FitMode for errors and debugging.
func (f FitMode) String() string {
	switch f {
	case FitInside:
		return "inside"
	case FitCover:
		return "cover"
	default:
		return fmt.Sprintf("FitMode(%d)", int(f))
	}
}

// maxThumbnailNameLen bounds a thumbnail spec name (matches maxCategoryLen).
const maxThumbnailNameLen = 64

// ThumbnailSpec describes one derived size to pregenerate at upload time. Each
// spec produces a downscaled, re-encoded variant stored alongside the original
// under the same category and content-addressed by its own bytes (see
// [Processed.Thumbnails]).
type ThumbnailSpec struct {
	// Name labels the variant (e.g. "thumb", "medium"). It appears in the upload
	// response and in media_variants, so it must be a stable identifier:
	// non-empty, unique within a [Config], and limited to the same character set
	// as a category (lowercase ASCII alphanumeric plus dash and underscore).
	Name string
	// MaxWidth and MaxHeight bound the variant. Both must be positive. For
	// FitInside they are an upper bound the output fits within; for FitCover they
	// are the exact output dimensions (modulo the no-upscale rule).
	MaxWidth  int
	MaxHeight int
	// Fit selects the scaling/cropping strategy. The zero value is FitInside.
	Fit FitMode
}

// Config tunes the upload pipeline, serving, and the category a handler stores
// under. The pipeline ([Process]) reads MaxUploadBytes, MaxPixels, MaxWidth,
// MaxHeight, FlattenAnimation, and Thumbnails; the rest is used by the HTTP
// handler.
type Config struct {
	// Category is the server-side bucket this handler stores under (e.g.
	// "campaign", "creature", "character"). It is a property of the mount, NOT
	// of the request — the client never sets it. A host registers one handler
	// per category over a single shared store, so images are physically
	// separated on disk (<root>/<category>/ab/cd/<hash>) and by composite key
	// in the database, not by metadata alone. Required and validated by
	// [NewHandler]; must match [ValidCategory].
	Category string

	// MaxUploadBytes caps the accepted upload size. Zero uses the default
	// (10 MiB).
	MaxUploadBytes int64

	// MaxPixels caps width*height to refuse decompression bombs before a full
	// decode. Zero uses the default (25 MP).
	MaxPixels int

	// MaxWidth and MaxHeight bound the canonical output. An image larger than
	// either is scaled down to fit, aspect ratio preserved, before re-encode;
	// smaller images are never upscaled. Zero leaves that side unbounded (the
	// default), and negative values are rejected. Thumbnails derive from the
	// bounded output, so no variant exceeds these limits either. They do not
	// relax the pre-decode guards: MaxPixels and the WebP side limit still
	// apply to the source.
	MaxWidth  int
	MaxHeight int

	// FlattenAnimation accepts an animated GIF and keeps only its first frame,
	// placed on the GIF's logical screen, instead of rejecting it with
	// [ErrAnimated]. Only the first frame is decoded, so a GIF with many frames
	// costs no more than a static one. The output is a static image like any
	// other; animation is never stored. Animated WebP is still rejected, since
	// the WebP decoder cannot read the animated container. Off by default:
	// callers taking deliberate uploads should refuse animation outright rather
	// than silently store a still the uploader did not choose.
	FlattenAnimation bool

	// PublicBaseURL is the externally-visible prefix the handler builds object
	// URLs from — point it at where this handler's GET route is mounted, which
	// is category-specific (e.g. "/media/campaign", or
	// "https://cdn.example.com/media/campaign"). Trailing slash optional.
	PublicBaseURL string

	// Authorize gates every upload and identifies the owning record. media does
	// not allow free-floating uploads: an image is always tied to some other
	// record the user may write to (e.g. a campaign), and that record's kind is
	// what the handler's Category names. Authorize returns the subject — the id
	// of that owning record, derived server-side from the request's route or
	// session — and whether the request may write to it. ok=false rejects the
	// upload; the subject is recorded in the image's metadata.
	//
	// A nil Authorize allows uploads with an empty subject. That is for trusted
	// or development mounts only — a production mount must set it.
	Authorize func(r *http.Request) (subject string, ok bool)

	// Thumbnails lists the derived sizes to pregenerate for every upload. Each
	// spec yields one downscaled, re-encoded variant (same safety pipeline and
	// format policy as the original), stored alongside it under the same category
	// and content-addressed by its own bytes. Empty (the default) generates no
	// thumbnails — the original is the only stored object. Validated by
	// [NewHandler] and [Process]; see [ThumbnailSpec].
	Thumbnails []ThumbnailSpec

	// Logger receives structured logs for storage failures at the HTTP
	// boundary. Defaults to [slog.Default]. Library code below the handler
	// (the blob and metadata stores) stays silent and returns wrapped errors;
	// the handler logs each failure once, here, then serves a sanitized 500.
	Logger *slog.Logger

	// RequestID extracts a correlation id from the request context for log
	// lines, so a media failure ties to the same request across components. A
	// host injects it (e.g. obs.RequestID); a nil extractor logs an empty id.
	// media never requires a specific middleware.
	RequestID func(ctx context.Context) string
}

// validThumbName reports whether s is a usable thumbnail spec name: the same
// character set as a category (lowercase ASCII alphanumeric, dash, underscore),
// but non-empty (a variant must be addressable by name).
func validThumbName(s string) bool {
	if s == "" || len(s) > maxThumbnailNameLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}

// validateProcessConfig checks the Config fields [Process] reads.
func validateProcessConfig(cfg Config) error {
	if cfg.MaxWidth < 0 || cfg.MaxHeight < 0 {
		return fmt.Errorf("media: negative output limit %dx%d", cfg.MaxWidth, cfg.MaxHeight)
	}
	return validateThumbnailSpecs(cfg.Thumbnails)
}

// validateThumbnailSpecs checks a thumbnail spec list: every name valid and
// unique, both dimensions positive, and a known Fit. An empty list is valid (no
// thumbnails). It is called by both [NewHandler] and [Process] so the same rules
// apply to HTTP mounts and direct library use.
func validateThumbnailSpecs(specs []ThumbnailSpec) error {
	seen := make(map[string]struct{}, len(specs))
	for _, s := range specs {
		if !validThumbName(s.Name) {
			return fmt.Errorf("media: invalid thumbnail name %q", s.Name)
		}
		if _, dup := seen[s.Name]; dup {
			return fmt.Errorf("media: duplicate thumbnail name %q", s.Name)
		}
		seen[s.Name] = struct{}{}
		if s.MaxWidth <= 0 || s.MaxHeight <= 0 {
			return fmt.Errorf("media: thumbnail %q has non-positive dimensions %dx%d", s.Name, s.MaxWidth, s.MaxHeight)
		}
		if s.Fit != FitInside && s.Fit != FitCover {
			return fmt.Errorf("media: thumbnail %q has unknown fit mode %s", s.Name, s.Fit)
		}
	}
	return nil
}
