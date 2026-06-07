//go:build integration

package media

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	pg, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("media_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Fatalf("postgres container: %v", err)
	}

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("connection string: %v", err)
	}

	testPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("pool: %v", err)
	}

	if err := Migrate(ctx, testPool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	code := m.Run()

	testPool.Close()
	_ = pg.Terminate(ctx)
	os.Exit(code)
}

// fresh truncates the media tables and returns clean stores over the pool.
func fresh(t *testing.T) (*Store, *PostgresBlobStore) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`TRUNCATE media_objects, media_blobs, media_variants`)
	require.NoError(t, err)
	return NewStore(testPool), NewPostgresBlobStore(testPool)
}

func sampleMeta() ObjectMeta {
	return ObjectMeta{
		Category:    "campaign",
		Hash:        "a1b2c3d4e5f600112233445566778899aabbccddeeff00112233445566778899",
		ContentType: "image/png",
		Width:       30,
		Height:      20,
		ByteSize:    1234,
		Subject:     "shadowmaze",
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestStore_PutLookupRoundTrip(t *testing.T) {
	store, _ := fresh(t)
	ctx := context.Background()
	in := sampleMeta()

	require.NoError(t, store.Put(ctx, in))

	got, ok, err := store.Lookup(ctx, in.Category, in.Hash)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, in.Category, got.Category)
	require.Equal(t, in.Hash, got.Hash)
	require.Equal(t, in.ContentType, got.ContentType)
	require.Equal(t, in.Width, got.Width)
	require.Equal(t, in.Height, got.Height)
	require.Equal(t, in.ByteSize, got.ByteSize)
	require.Equal(t, in.Subject, got.Subject)
	require.WithinDuration(t, in.CreatedAt, got.CreatedAt, time.Second)
}

func TestStore_LookupMissing(t *testing.T) {
	store, _ := fresh(t)
	in := sampleMeta()
	_, ok, err := store.Lookup(context.Background(), in.Category, in.Hash)
	require.NoError(t, err)
	require.False(t, ok)
}

// A different category is a different key — Lookup must not cross categories.
func TestStore_CategoryScopesLookup(t *testing.T) {
	store, _ := fresh(t)
	ctx := context.Background()
	in := sampleMeta()
	require.NoError(t, store.Put(ctx, in))

	_, ok, err := store.Lookup(ctx, "creature", in.Hash)
	require.NoError(t, err)
	require.False(t, ok, "same hash under a different category must not be found")
}

func TestStore_PutIdempotent(t *testing.T) {
	store, _ := fresh(t)
	ctx := context.Background()
	in := sampleMeta()
	require.NoError(t, store.Put(ctx, in))
	require.NoError(t, store.Put(ctx, in)) // second insert is a no-op, not an error

	var count int
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT count(*) FROM media_objects WHERE category = $1 AND hash = $2`,
		in.Category, in.Hash).Scan(&count))
	require.Equal(t, 1, count)
}

func TestStore_ListBySubject(t *testing.T) {
	store, _ := fresh(t)
	ctx := context.Background()

	mk := func(hash, subject string) ObjectMeta {
		m := sampleMeta()
		m.Hash = hash
		m.Subject = subject
		return m
	}
	hashes := []string{
		"1111111111111111111111111111111111111111111111111111111111111111",
		"2222222222222222222222222222222222222222222222222222222222222222",
		"3333333333333333333333333333333333333333333333333333333333333333",
	}
	require.NoError(t, store.Put(ctx, mk(hashes[0], "shadowmaze")))
	require.NoError(t, store.Put(ctx, mk(hashes[1], "shadowmaze")))
	require.NoError(t, store.Put(ctx, mk(hashes[2], "osrrealms")))

	got, err := store.ListBySubject(ctx, "campaign", "shadowmaze")
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, m := range got {
		require.Equal(t, "shadowmaze", m.Subject)
		require.Equal(t, "campaign", m.Category)
	}
}

func sampleVariant() Variant {
	return Variant{
		Category:    "campaign",
		ParentHash:  sampleMeta().Hash,
		Name:        "thumb",
		Hash:        "ffeeddccbbaa00998877665544332211ffeeddccbbaa00998877665544332211",
		ContentType: "image/webp",
		Width:       64,
		Height:      64,
		ByteSize:    512,
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestStore_VariantRoundTrip(t *testing.T) {
	store, _ := fresh(t)
	ctx := context.Background()
	in := sampleVariant()

	require.NoError(t, store.PutVariant(ctx, in))
	require.NoError(t, store.PutVariant(ctx, in)) // idempotent on (category, parent, name)

	got, err := store.ListVariants(ctx, in.Category, in.ParentHash)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, in.Name, got[0].Name)
	require.Equal(t, in.Hash, got[0].Hash)
	require.Equal(t, in.ContentType, got[0].ContentType)
	require.Equal(t, in.Width, got[0].Width)
	require.Equal(t, in.Height, got[0].Height)
	require.Equal(t, in.ByteSize, got[0].ByteSize)
}

func TestStore_ListObjectsAfterPaging(t *testing.T) {
	store, _ := fresh(t)
	ctx := context.Background()

	hashes := []string{
		"1111111111111111111111111111111111111111111111111111111111111111",
		"2222222222222222222222222222222222222222222222222222222222222222",
		"3333333333333333333333333333333333333333333333333333333333333333",
		"4444444444444444444444444444444444444444444444444444444444444444",
	}
	for _, hh := range hashes {
		m := sampleMeta()
		m.Hash = hh
		require.NoError(t, store.Put(ctx, m))
	}

	// Walk in pages of 2 via the keyset cursor; expect all four in hash order.
	var seen []string
	cursor := ""
	for {
		page, err := store.ListObjectsAfter(ctx, "campaign", cursor, 2)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		for _, m := range page {
			seen = append(seen, m.Hash)
		}
		cursor = page[len(page)-1].Hash
		if len(page) < 2 {
			break
		}
	}
	require.Equal(t, hashes, seen)
}

// BackfillThumbnails fills missing sizes for an already-stored original and is a
// no-op on a second run.
func TestBackfillThumbnails(t *testing.T) {
	store, blobs := fresh(t)
	ctx := context.Background()

	// Store an original with no thumbnails (as if uploaded before the feature).
	processed, err := Process(bytes.NewReader(pngBytes(t, 400, 300)), Config{Category: "campaign"})
	require.NoError(t, err)
	require.Empty(t, processed.Thumbnails)
	require.NoError(t, blobs.Put(ctx, "campaign", processed.SHA256, processed.ContentType, processed.Bytes))
	require.NoError(t, store.Put(ctx, ObjectMeta{
		Category:    "campaign",
		Hash:        processed.SHA256,
		ContentType: processed.ContentType,
		Width:       processed.Width,
		Height:      processed.Height,
		ByteSize:    int64(len(processed.Bytes)),
		Subject:     "shadowmaze",
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}))

	cfg := Config{Category: "campaign", Thumbnails: []ThumbnailSpec{
		{Name: "thumb", MaxWidth: 64, MaxHeight: 64, Fit: FitCover},
	}}

	res, err := BackfillThumbnails(ctx, store, blobs, cfg)
	require.NoError(t, err)
	require.Equal(t, 1, res.Scanned)
	require.Equal(t, 1, res.Created)

	variants, err := store.ListVariants(ctx, "campaign", processed.SHA256)
	require.NoError(t, err)
	require.Len(t, variants, 1)
	require.Equal(t, "thumb", variants[0].Name)
	require.Equal(t, 64, variants[0].Width)
	has, err := blobs.Has(ctx, "campaign", variants[0].Hash)
	require.NoError(t, err)
	require.True(t, has)

	// Re-run: nothing new to create.
	again, err := BackfillThumbnails(ctx, store, blobs, cfg)
	require.NoError(t, err)
	require.Equal(t, 1, again.Scanned)
	require.Equal(t, 0, again.Created)
}

func TestPostgresBlobStore_RoundTrip(t *testing.T) {
	_, blobs := fresh(t)
	ctx := context.Background()
	cat, hash := "campaign", sampleMeta().Hash
	payload := []byte("\x89PNG fake bytes for storage round trip")

	has, err := blobs.Has(ctx, cat, hash)
	require.NoError(t, err)
	require.False(t, has)

	require.NoError(t, blobs.Put(ctx, cat, hash, "image/png", payload))

	has, err = blobs.Has(ctx, cat, hash)
	require.NoError(t, err)
	require.True(t, has)

	got, ct, err := blobs.Get(ctx, cat, hash)
	require.NoError(t, err)
	require.Equal(t, payload, got)
	require.Equal(t, "image/png", ct)

	// A different category does not see this blob.
	has, err = blobs.Has(ctx, "creature", hash)
	require.NoError(t, err)
	require.False(t, has)

	_, _, err = blobs.Get(ctx, cat, "0000000000000000000000000000000000000000000000000000000000000000")
	require.ErrorIs(t, err, ErrBlobNotFound)
}

func TestMigrate_Idempotent(t *testing.T) {
	// TestMain already migrated; a second call must be a no-op.
	require.NoError(t, Migrate(context.Background(), testPool))
}

// TestEndToEnd exercises the full stack — handler + Process + Postgres store
// and blob backend — to confirm the pieces compose against a real database.
func TestEndToEnd_UploadServe(t *testing.T) {
	store, blobs := fresh(t)
	h, err := NewHandler(store, blobs, Config{Category: "campaign", PublicBaseURL: "/media"})
	require.NoError(t, err)

	up := httptest.NewRecorder()
	h.Routes().ServeHTTP(up, uploadRequest(t, "image", pngBytes(t, 16, 9)))
	require.Equal(t, http.StatusCreated, up.Code, up.Body.String())

	var resp uploadResponse
	require.NoError(t, json.Unmarshal(up.Body.Bytes(), &resp))
	require.Equal(t, 16, resp.Width)
	require.Equal(t, 9, resp.Height)

	path := resp.URL[len("/media"):]
	get := httptest.NewRecorder()
	h.Routes().ServeHTTP(get, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, get.Code)
	require.Equal(t, "image/webp", get.Header().Get("Content-Type")) // PNG re-encodes to WebP
	require.Equal(t, "nosniff", get.Header().Get("X-Content-Type-Options"))
	require.NotEmpty(t, get.Body.Bytes())
}
