package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// FilesystemBlobStore stores blob bytes on the local filesystem — plain image
// files under a root directory, in media's content-addressed,
// category-partitioned layout. One file holds the bytes and a small sidecar
// records the content type; paths are <root>/<category>/ab/cd/<hash> (the
// category folder gives the logical separation; the first two bytes of the hash
// shard so no directory holds the whole corpus). An empty category drops the
// category folder (<root>/ab/cd/...).
//
// It satisfies [BlobStore], so a host swaps it for [PostgresBlobStore] with no
// other change. Like the Postgres backend it is content-addressed and
// idempotent. Bytes are still served through the media handler (Content-Type +
// nosniff + immutable caching), never via a bare directory listing.
type FilesystemBlobStore struct {
	root string
}

// NewFilesystemBlobStore returns a store rooted at dir, creating it (0700) if
// it does not exist.
func NewFilesystemBlobStore(dir string) (*FilesystemBlobStore, error) {
	if dir == "" {
		return nil, errors.New("media: filesystem blob store: empty root directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("media: filesystem blob store: %w", err)
	}
	return &FilesystemBlobStore{root: dir}, nil
}

// paths returns the blob and sidecar paths for (category, hash). ok is false
// when the category or hash is not clean — the guard that makes path traversal
// impossible, since a category is the ValidCategory set (no separator or dot)
// and a key is only ever a sha256 hex string, both joined under the fixed root.
func (f *FilesystemBlobStore) paths(category, hash string) (blobPath, typePath string, ok bool) {
	if !ValidCategory(category) || !looksLikeHash(hash) {
		return "", "", false
	}
	parts := []string{f.root}
	if category != "" {
		parts = append(parts, category)
	}
	parts = append(parts, hash[:2], hash[2:4], hash)
	blobPath = filepath.Join(parts...)
	return blobPath, blobPath + ".type", true
}

// Put writes the bytes and a content-type sidecar; storing an existing key is a
// no-op.
func (f *FilesystemBlobStore) Put(_ context.Context, category, hash, contentType string, data []byte) error {
	blobPath, typePath, ok := f.paths(category, hash)
	if !ok {
		return fmt.Errorf("media: filesystem put: invalid category/hash %q/%q", category, hash)
	}
	if _, err := os.Stat(blobPath); err == nil { //nolint:gosec // G703: blobPath is <root>/<validated-category>/<validated-hash shards>; no traversal
		return nil // content-addressed: already stored
	}
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o700); err != nil { //nolint:gosec // G703: see above
		return fmt.Errorf("media: filesystem put: %w", err)
	}
	// Sidecar first, bytes last: once the blob file exists its type is already
	// recorded, so Has (which checks the blob) never reports a typeless blob.
	if err := writeFileAtomic(typePath, []byte(contentType)); err != nil {
		return fmt.Errorf("media: filesystem put type: %w", err)
	}
	if err := writeFileAtomic(blobPath, data); err != nil {
		return fmt.Errorf("media: filesystem put blob: %w", err)
	}
	return nil
}

// Get returns the bytes and content type for (category, hash), or [ErrBlobNotFound].
func (f *FilesystemBlobStore) Get(_ context.Context, category, hash string) ([]byte, string, error) {
	blobPath, typePath, ok := f.paths(category, hash)
	if !ok {
		return nil, "", ErrBlobNotFound
	}
	data, err := os.ReadFile(blobPath) //nolint:gosec // G304: path is <root>/<validated-category>/<validated-hash shards>; no traversal
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", ErrBlobNotFound
		}
		return nil, "", fmt.Errorf("media: filesystem get: %w", err)
	}
	contentType := ""
	if ct, err := os.ReadFile(typePath); err == nil { //nolint:gosec // G304: see above
		contentType = string(ct)
	}
	if contentType == "" {
		// Sidecar missing/empty: sniff so serving still works.
		contentType = http.DetectContentType(data)
	}
	return data, contentType, nil
}

// Has reports whether a blob with (category, hash) is stored.
func (f *FilesystemBlobStore) Has(_ context.Context, category, hash string) (bool, error) {
	blobPath, _, ok := f.paths(category, hash)
	if !ok {
		return false, nil
	}
	switch _, err := os.Stat(blobPath); { //nolint:gosec // G703: validated category/hash path
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("media: filesystem has: %w", err)
	}
}

// writeFileAtomic writes data to a temp file in the destination directory, then
// renames it into place — so a reader never sees a half-written blob and a
// crash mid-write leaves the old file (or none), never a corrupt one.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*") // CreateTemp makes the file 0600
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() //nolint:gosec // G703: tmpName from os.CreateTemp under the destination dir

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path) //nolint:gosec // G703: both paths under <root>/<validated-category>/<validated-hash shards>
}

// Compile-time proof the filesystem store satisfies the seam.
var _ BlobStore = (*FilesystemBlobStore)(nil)
