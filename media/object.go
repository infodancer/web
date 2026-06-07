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

// Variant is the metadata record for one derived size (thumbnail) of an
// original object, keyed by (Category, ParentHash, Name). Its bytes live in the
// same [BlobStore] under (Category, Hash) like any other blob — Hash is the
// variant's own content address, distinct from ParentHash. This row is the
// original -> named-variant map; it does not duplicate the bytes.
type Variant struct {
	Category    string // same bucket as the parent object
	ParentHash  string // content address of the original this variant derives from
	Name        string // the ThumbnailSpec.Name that produced it (e.g. "thumb")
	Hash        string // sha256 hex of the variant's own canonical bytes
	ContentType string // "image/webp" or "image/jpeg"
	Width       int
	Height      int
	ByteSize    int64
	CreatedAt   time.Time
}
