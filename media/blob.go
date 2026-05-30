package media

import (
	"context"
	"errors"
	"sync"
)

// ErrBlobNotFound is returned by [BlobStore.Get] when no blob has the hash.
var ErrBlobNotFound = errors.New("media: blob not found")

// BlobStore is the byte-storage seam. The default backend keeps bytes in
// Postgres (see [PostgresBlobStore]); a filesystem or object-store backend can
// be added behind this interface without touching the pipeline, metadata, or
// handler. Keys are content addresses (sha256 hex), so Put is idempotent and a
// stored blob never changes for a given key.
type BlobStore interface {
	// Put stores data under hash with its content type. Storing an existing
	// hash is a no-op (idempotent), since the key addresses the content.
	Put(ctx context.Context, hash, contentType string, data []byte) error
	// Get returns the bytes and content type for hash, or [ErrBlobNotFound].
	Get(ctx context.Context, hash string) (data []byte, contentType string, err error)
	// Has reports whether a blob with hash is stored.
	Has(ctx context.Context, hash string) (bool, error)
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

func (m *MemoryBlobStore) Put(_ context.Context, hash, contentType string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[hash]; ok {
		return nil // content-addressed: already stored
	}
	// Defensive copy so a caller reusing the slice cannot mutate stored bytes.
	cp := make([]byte, len(data))
	copy(cp, data)
	m.data[hash] = memBlob{bytes: cp, contentType: contentType}
	return nil
}

func (m *MemoryBlobStore) Get(_ context.Context, hash string) ([]byte, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.data[hash]
	if !ok {
		return nil, "", ErrBlobNotFound
	}
	return b.bytes, b.contentType, nil
}

func (m *MemoryBlobStore) Has(_ context.Context, hash string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[hash]
	return ok, nil
}
