# media

A reusable image upload/store/serve component for infodancer websites. It backs
mdedit's image references and blog's embedded images: a host mounts it, an
author uploads an image, and `media` makes it safe, stores it, and serves it
under a stable content-addressed URL.

It is the storage/serving third of the image split — the editor
([`infodancer/ui/mdedit`](https://github.com/infodancer/ui)) inserts a
reference and the sanitizer
([`infodancer/ui/markdown`](https://github.com/infodancer/ui)) decides what
`<img>` survives; neither stores bytes. `media` does.

```go
import "github.com/infodancer/web/media"
```

## Why it exists

Serving user-uploaded files is a classic stored-XSS and privacy vector — an SVG
with script, a polyglot sniffed as HTML, EXIF GPS leakage, a decompression
bomb. `media` concentrates that risk in one audited, tested place, the way
`markdown` concentrates Markdown sanitization.

## The pipeline

`Process` is the security core, and it touches no database:

1. Bound the input size; ignore the client's declared content-type and filename.
2. Sniff the format from content — extension and client type are never trusted.
3. Allowlist **PNG, JPEG, GIF, WebP**.
4. Guard dimensions from the header *before* decoding (decompression-bomb guard).
5. Reject animation (animated GIF); reject SVG (not a raster format).
6. Decode and **re-encode** to a canonical raster (lossless PNG for PNG and
   static GIF; q85 JPEG for JPEG and WebP) — strips EXIF, kills polyglots.
7. Content-address by SHA-256; identical uploads dedupe.

## Mount it

```go
pool := /* the host's *pgxpool.Pool */
if err := media.Migrate(ctx, pool); err != nil { /* ... */ }

h := media.NewHandler(
    media.NewStore(pool),            // metadata
    media.NewPostgresBlobStore(pool), // bytes (default backend)
    media.Config{
        MaxUploadBytes: 10 << 20,
        MaxPixels:      25_000_000,
        PublicBaseURL:  "/media",     // or "https://cdn.example.com/media"
        Authorize:      func(r *http.Request) (owner string, ok bool) { /* host auth */ },
    },
)
mux.Handle("/media/", http.StripPrefix("/media", h.Routes()))
```

- `POST {mount}/` — multipart field `image`. On success: `201` + JSON
  `{ "url": "...", "width": N, "height": N }`. mdedit's adapter inserts
  `![](url)`.
- `GET {mount}/{hash}[.ext]` — serves the bytes with `Content-Type`,
  `X-Content-Type-Options: nosniff`, immutable `Cache-Control`, and `ETag`.

## Storage

Bytes default to Postgres `bytea`, behind the `BlobStore` seam:

```go
type BlobStore interface {
    Put(ctx, hash, contentType string, data []byte) error
    Get(ctx, hash string) (data []byte, contentType string, err error)
    Has(ctx, hash string) (bool, error)
}
```

Backends, all behind the same seam (swap one for another with no other change):

- `PostgresBlobStore` — bytes in Postgres `bytea` (default).
- `FilesystemBlobStore` — bytes on disk under a root dir, content-addressed and
  sharded (`<root>/ab/cd/<hash>`) with a content-type sidecar; the same
  filesystem-image posture osg uses, with atomic writes. Bytes are still served
  through the handler (so `nosniff` + headers apply), never a bare directory
  listing.
- `MemoryBlobStore` — in-memory, for tests and no-database dev mounts.

```go
blobs, err := media.NewFilesystemBlobStore("/var/lib/app/media")
```

## What the host owns

Authentication, authorization, CSRF, and the rate-limit policy on the public
intake — exposed as hooks on `Config`. The handler adds a same-origin guard and
the `Authorize` gate, but the host decides who may upload.

## Develop

```
task generate          # regenerate db/ from db/queries.sql via sqlc (after schema/query edits)
task test              # unit tests, no Docker
task test:integration  # Postgres-backed tests (Docker, testcontainers)
task check             # fmt, vet, lint, test, vulncheck
```

## Status

Pre-1.0 (tag `media/vX.Y.Z`). `Config`, `Process`/`Processed`, `BlobStore`,
`NewStore`/`NewPostgresBlobStore`/`NewHandler`/`Migrate`, and the upload/serve
HTTP contract are the public surface. See [DESIGN.md](DESIGN.md).

## License

Apache-2.0.
