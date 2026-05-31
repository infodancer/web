package media

import "time"

// ObjectMeta is the metadata record for one stored image, keyed by the
// composite (Category, Hash) content address. Bytes live in a [BlobStore]; this
// is everything else, kept in Postgres so the metadata is queryable and
// swapping the byte backend touches no rows.
type ObjectMeta struct {
	Category    string // server-side bucket (e.g. "campaign"); part of the key
	Hash        string // sha256 hex of the canonical bytes
	ContentType string // "image/png" or "image/jpeg"
	Width       int
	Height      int
	ByteSize    int64
	Subject     string // id of the owning record this image is tied to (from Config.Authorize)
	CreatedAt   time.Time
}
