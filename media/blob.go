package media

import (
	"context"
	"errors"
	"sync"
)

// ErrBlobNotFound is returned by [BlobStore.Get] when no blob has the key.
var ErrBlobNotFound = errors.New("media: blob not found")

// maxCategoryLen bounds a category name.
const maxCategoryLen = 64

// ValidCategory reports whether s is a usable category bucket: lowercase
// ASCII alphanumeric plus dash and underscore, up to 64 chars. The empty string
// is valid and means "no category prefix" — the original flat layout — for
// direct library use; the HTTP handler requires a non-empty category. The
// restriction is what makes a category safe as a filesystem path segment and a
// database key (no separators, dots, or traversal).
func ValidCategory(s string) bool {
	if len(s) > maxCategoryLen {
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

// BlobStore is the byte-storage seam, partitioned by category so one configured
// store serves every category a host registers. The default backend keeps bytes
// in Postgres (see [PostgresBlobStore]); [FilesystemBlobStore] keeps them on
// disk; a future object-store backend fits the same interface without touching
// the pipeline, metadata, or handler. Keys are (category, content-address), so
// Put is idempotent and a stored blob never changes for a given key.
//
// Delete removes bytes unconditionally. Content addressing means one blob can
// back several objects or variants, so callers normally go through [Delete],
// which removes a blob only once no metadata row refers to it.
type BlobStore interface {
	// Put stores data under (category, hash) with its content type. Storing an
	// existing key is a no-op (idempotent).
	Put(ctx context.Context, category, hash, contentType string, data []byte) error
	// Get returns the bytes and content type for (category, hash), or
	// [ErrBlobNotFound].
	Get(ctx context.Context, category, hash string) (data []byte, contentType string, err error)
	// Has reports whether a blob with (category, hash) is stored.
	Has(ctx context.Context, category, hash string) (bool, error)
	// Delete removes the blob for (category, hash). Deleting a key that is not
	// stored is not an error (idempotent).
	Delete(ctx context.Context, category, hash string) error
}

// MemoryBlobStore is an in-memory [BlobStore] for tests and a no-database dev
// mount. It is safe for concurrent use. It is not persistent — do not use it in
// production.
type MemoryBlobStore struct {
	mu   sync.RWMutex
	data map[string]memBlob
}

type memBlob struct {
	bytes       []byte
	contentType string
}

// NewMemoryBlobStore returns an empty in-memory blob store.
func NewMemoryBlobStore() *MemoryBlobStore {
	return &MemoryBlobStore{data: make(map[string]memBlob)}
}

// memKey combines category and hash into one map key. A NUL separator can't
// appear in either (hash is hex, category is the ValidCategory set), so the
// join is unambiguous.
func memKey(category, hash string) string { return category + "\x00" + hash }

// Put stores data under (category, hash); storing an existing key is a no-op.
func (m *MemoryBlobStore) Put(_ context.Context, category, hash, contentType string, data []byte) error {
	key := memKey(category, hash)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[key]; ok {
		return nil // content-addressed: already stored
	}
	// Defensive copy so a caller reusing the slice cannot mutate stored bytes.
	cp := make([]byte, len(data))
	copy(cp, data)
	m.data[key] = memBlob{bytes: cp, contentType: contentType}
	return nil
}

// Get returns the bytes and content type for (category, hash), or [ErrBlobNotFound].
func (m *MemoryBlobStore) Get(_ context.Context, category, hash string) ([]byte, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.data[memKey(category, hash)]
	if !ok {
		return nil, "", ErrBlobNotFound
	}
	return b.bytes, b.contentType, nil
}

// Has reports whether a blob with (category, hash) is stored.
func (m *MemoryBlobStore) Has(_ context.Context, category, hash string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[memKey(category, hash)]
	return ok, nil
}

// Delete removes the blob for (category, hash); a missing key is not an error.
func (m *MemoryBlobStore) Delete(_ context.Context, category, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, memKey(category, hash))
	return nil
}
