package media

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPostgresBlobStore_Delete(t *testing.T) {
	testBlobStoreDelete(t, func(t *testing.T) BlobStore {
		_, blobs := fresh(t)
		return blobs
	})
}

// putObject records an object row with the given hash in category "cover".
func putObject(t *testing.T, store *Store, hash string) {
	t.Helper()
	require.NoError(t, store.Put(context.Background(), ObjectMeta{
		Category: "cover", Hash: hash, ContentType: "image/webp",
		Width: 16, Height: 16, ByteSize: 10, CreatedAt: time.Now().UTC(),
	}))
}

// putVariant records a variant row mapping parent to hash under name.
func putVariant(t *testing.T, store *Store, parent, name, hash string) {
	t.Helper()
	require.NoError(t, store.PutVariant(context.Background(), Variant{
		Category: "cover", ParentHash: parent, Name: name, Hash: hash,
		ContentType: "image/webp", Width: 8, Height: 8, ByteSize: 5, CreatedAt: time.Now().UTC(),
	}))
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func TestStore_DeleteObject_RemovesRowsAndReportsBlobs(t *testing.T) {
	store, _ := fresh(t)
	ctx := context.Background()
	parent, thumb, medium := hashOf([]byte("p")), hashOf([]byte("t")), hashOf([]byte("m"))
	putObject(t, store, parent)
	putVariant(t, store, parent, "thumb", thumb)
	putVariant(t, store, parent, "medium", medium)

	unref, err := store.DeleteObject(ctx, "cover", parent)
	require.NoError(t, err)
	require.Equal(t, sorted([]string{parent, thumb, medium}), sorted(unref))

	_, ok, err := store.Lookup(ctx, "cover", parent)
	require.NoError(t, err)
	require.False(t, ok, "object row survived DeleteObject")
	vs, err := store.ListVariants(ctx, "cover", parent)
	require.NoError(t, err)
	require.Empty(t, vs, "variant rows survived DeleteObject")
}

func TestStore_DeleteObject_KeepsBlobsStillReferenced(t *testing.T) {
	store, _ := fresh(t)
	ctx := context.Background()
	a, b, c, shared := hashOf([]byte("a")), hashOf([]byte("b")), hashOf([]byte("c")), hashOf([]byte("s"))

	// a's thumbnail is byte-identical to object b, and a and c share a variant.
	putObject(t, store, a)
	putObject(t, store, b)
	putObject(t, store, c)
	putVariant(t, store, a, "thumb", b)
	putVariant(t, store, a, "medium", shared)
	putVariant(t, store, c, "medium", shared)

	unref, err := store.DeleteObject(ctx, "cover", a)
	require.NoError(t, err)
	require.Equal(t, []string{a}, unref, "blobs still referenced by b or c were reported")
}

func TestStore_DeleteObject_KeepsBlobReferencedAsAnotherVariant(t *testing.T) {
	store, _ := fresh(t)
	ctx := context.Background()
	small, big := hashOf([]byte("small")), hashOf([]byte("big"))

	// big's thumbnail is the same bytes as the original small object.
	putObject(t, store, small)
	putObject(t, store, big)
	putVariant(t, store, big, "thumb", small)

	unref, err := store.DeleteObject(ctx, "cover", small)
	require.NoError(t, err)
	require.Empty(t, unref, "blob still used as big's thumbnail was reported")
}

func TestStore_DeleteObject_IsCategoryScoped(t *testing.T) {
	store, _ := fresh(t)
	ctx := context.Background()
	h := hashOf([]byte("x"))
	putObject(t, store, h)
	require.NoError(t, store.Put(ctx, ObjectMeta{
		Category: "avatar", Hash: h, ContentType: "image/webp", Width: 1, Height: 1, ByteSize: 1, CreatedAt: time.Now().UTC(),
	}))

	unref, err := store.DeleteObject(ctx, "cover", h)
	require.NoError(t, err)
	require.Equal(t, []string{h}, unref, "a row in another category must not count as a reference")

	_, ok, err := store.Lookup(ctx, "avatar", h)
	require.NoError(t, err)
	require.True(t, ok, "DeleteObject removed a row from another category")
}

func TestStore_DeleteObject_MissingObjectReportsItsHash(t *testing.T) {
	// A crash between Delete's metadata and blob steps leaves bytes with no row;
	// reporting the hash lets a repeated Delete remove them.
	store, _ := fresh(t)
	h := hashOf([]byte("orphan"))
	unref, err := store.DeleteObject(context.Background(), "cover", h)
	require.NoError(t, err)
	require.Equal(t, []string{h}, unref)
}

func TestDelete_EndToEnd(t *testing.T) {
	store, _ := fresh(t)
	blobs, err := NewFilesystemBlobStore(t.TempDir())
	require.NoError(t, err)
	ctx := context.Background()
	cfg := Config{
		Category:      "cover",
		PublicBaseURL: "/media/cover",
		Thumbnails:    []ThumbnailSpec{{Name: "thumb", MaxWidth: 8, MaxHeight: 8}},
	}

	res, err := Ingest(ctx, store, blobs, cfg, "book-1", bytes.NewReader(pngBytes(t, 32, 32)))
	require.NoError(t, err)
	vs, err := store.ListVariants(ctx, "cover", res.Meta.Hash)
	require.NoError(t, err)
	require.Len(t, vs, 1)

	require.NoError(t, Delete(ctx, store, blobs, "cover", res.Meta.Hash))

	for _, h := range []string{res.Meta.Hash, vs[0].Hash} {
		ok, err := blobs.Has(ctx, "cover", h)
		require.NoError(t, err)
		require.False(t, ok, "blob %s survived Delete", h)
	}
	_, ok, err := store.Lookup(ctx, "cover", res.Meta.Hash)
	require.NoError(t, err)
	require.False(t, ok)

	// The serve path now 404s, and Delete is idempotent.
	h, err := NewHandler(store, blobs, cfg)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+res.Meta.Hash+".webp", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.NoError(t, Delete(ctx, store, blobs, "cover", res.Meta.Hash))
}

func TestDelete_RemovesOrphanBlob(t *testing.T) {
	store, blobs := fresh(t)
	ctx := context.Background()
	data := []byte("left behind")
	h := hashOf(data)
	require.NoError(t, blobs.Put(ctx, "cover", h, "image/png", data))

	require.NoError(t, Delete(ctx, store, blobs, "cover", h))

	ok, err := blobs.Has(ctx, "cover", h)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDelete_RejectsBadKeys(t *testing.T) {
	store, blobs := fresh(t)
	ctx := context.Background()
	good := hashOf([]byte("x"))
	for _, tc := range []struct{ category, hash string }{
		{"../etc", good},
		{"cover", "nothex"},
		{"cover", ""},
	} {
		require.Error(t, Delete(ctx, store, blobs, tc.category, tc.hash), "Delete(%q, %q)", tc.category, tc.hash)
	}
}
