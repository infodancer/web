package media

import "net/http"

// Config tunes the upload pipeline, serving, and the category a handler stores
// under. The pipeline ([Process]) reads only MaxUploadBytes and MaxPixels; the
// rest is used by the HTTP handler.
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
}
