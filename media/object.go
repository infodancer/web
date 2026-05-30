package media

import "time"

// ObjectMeta is the metadata record for one stored image, keyed by its content
// address (Hash). Bytes live in a [BlobStore]; this is everything else, kept in
// Postgres so the metadata is queryable and swapping the byte backend touches
// no rows.
type ObjectMeta struct {
	Hash        string // sha256 hex of the canonical bytes; primary key + URL slug
	ContentType string // "image/png" or "image/jpeg"
	Width       int
	Height      int
	ByteSize    int64
	Owner       string // host-defined owner id from Config.Authorize; "" if open
	CreatedAt   time.Time
}
