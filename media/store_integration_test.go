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
		Hash:        "a1b2c3d4e5f600112233445566778899aabbccddeeff00112233445566778899",
		ContentType: "image/png",
		Width:       30,
		Height:      20,
		ByteSize:    1234,
		Owner:       "alice",
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestStore_PutLookupRoundTrip(t *testing.T) {
	store, _ := fresh(t)
	ctx := context.Background()
	in := sampleMeta()

	require.NoError(t, store.Put(ctx, in))

	got, ok, err := store.Lookup(ctx, in.Hash)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, in.Hash, got.Hash)
	require.Equal(t, in.ContentType, got.ContentType)
	require.Equal(t, in.Width, got.Width)
	require.Equal(t, in.Height, got.Height)
	require.Equal(t, in.ByteSize, got.ByteSize)
	require.Equal(t, in.Owner, got.Owner)
	require.WithinDuration(t, in.CreatedAt, got.CreatedAt, time.Second)
}

func TestStore_LookupMissing(t *testing.T) {
	store, _ := fresh(t)
	_, ok, err := store.Lookup(context.Background(), sampleMeta().Hash)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestStore_PutIdempotent(t *testing.T) {
	store, _ := fresh(t)
	ctx := context.Background()
	in := sampleMeta()
	require.NoError(t, store.Put(ctx, in))
	require.NoError(t, store.Put(ctx, in)) // second insert is a no-op, not an error

	var count int
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT count(*) FROM media_objects WHERE hash = $1`, in.Hash).Scan(&count))
	require.Equal(t, 1, count)
}

func TestPostgresBlobStore_RoundTrip(t *testing.T) {
	_, blobs := fresh(t)
	ctx := context.Background()
	hash := sampleMeta().Hash
	payload := []byte("\x89PNG fake bytes for storage round trip")

	has, err := blobs.Has(ctx, hash)
	require.NoError(t, err)
	require.False(t, has)

	require.NoError(t, blobs.Put(ctx, hash, "image/png", payload))

	has, err = blobs.Has(ctx, hash)
	require.NoError(t, err)
	require.True(t, has)

	got, ct, err := blobs.Get(ctx, hash)
	require.NoError(t, err)
	require.Equal(t, payload, got)
	require.Equal(t, "image/png", ct)

	_, _, err = blobs.Get(ctx, "0000000000000000000000000000000000000000000000000000000000000000")
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
	h := NewHandler(store, blobs, Config{PublicBaseURL: "/media"})

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
