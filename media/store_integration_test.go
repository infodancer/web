//go:build integration

package media

import (
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
		`TRUNCATE media_objects, media_blobs`)
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
	require.Equal(t, "image/png", get.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", get.Header().Get("X-Content-Type-Options"))
	require.NotEmpty(t, get.Body.Bytes())
}
