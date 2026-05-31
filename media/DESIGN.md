# media — design (v0.1)

## Problem

The portfolio's authoring tools need somewhere to put images. mdedit (the
Markdown editor) can insert an image *reference*; `infodancer/ui/markdown`
decides what `<img>` survives sanitization; neither stores or serves bytes.
blog's DESIGN names "the v0.2 media subsystem" as the piece it waits on for
embedded images. `media` is that piece: a mountable component that accepts an
upload, makes it safe, stores it, and serves it back under a stable URL.

Serving user-uploaded files is a classic stored-XSS and privacy vector (SVG
with script, polyglot files sniffed as HTML, EXIF GPS leakage, decompression
bombs). Concentrating that risk in one audited, tested component — the way
`markdown` concentrates sanitization — is the point.

## Scope

In scope: accept an image upload over HTTP, validate and re-encode it to a
safe canonical form, content-address and store it, and serve it with correct
security headers. A small host integration surface matching the suite mount
contract.

Out of scope (host owns these, via hooks): authentication, authorization, CSRF,
and the rate-limit policy on the public intake. `media` exposes seams for them;
it does not decide who may upload. Also out of scope for v0.1: SVG, animation,
responsive/derived sizes (thumbnails), and non-image media.

## Decisions

### Home: `github.com/infodancer/web/media`

A nested module in the backend monorepo, next to blog (its first consumer) and
matching the suite's Postgres mount contract (`New`/`NewStore(pool)`, a mounted
`http.Handler`, `Migrate(ctx, pool)` under a dedicated goose table, `media_*`
tables, sqlc + pgx/v5). It is *not* in `infodancer/ui` — that repo is the
front-end visual layer; the store/serve service belongs with the other
backend components.

### Byte storage: Postgres `bytea` behind a `BlobStore` seam

Bytes live in Postgres (`bytea`) by default — consistent with the suite's
"Postgres everywhere, host owns one pool" posture, transactional with the
metadata, and backed up with the database. At portfolio scale (personal sites,
session-notes images capped at a few MB) this is fine, and content-addressed
URLs with immutable caching keep origin reads rare.

A `BlobStore` interface isolates byte storage so a filesystem or object-store
backend can be added later without touching the pipeline or the metadata:

```go
type BlobStore interface {
    Put(ctx context.Context, hash string, contentType string, data []byte) error
    Get(ctx context.Context, hash string) (data []byte, contentType string, err error)
    Has(ctx context.Context, hash string) (bool, error)
}
```

Metadata (`media_objects`: hash, content_type, width, height, byte_size, owner,
created_at) is always in Postgres; only the bytes move when the backend is
swapped. Shipped backends:

- `PostgresBlobStore` — bytes in `media_blobs` (default).
- `FilesystemBlobStore` — bytes on disk under a root dir, content-addressed and
  sharded `<root>/ab/cd/<hash>` with a content-type sidecar and atomic
  (temp-file + rename) writes. This is the same "image files under a root"
  posture osg uses for chronicle images, adapted to the content-addressed
  model; the hash is validated (`looksLikeHash`) before any path is built, so
  traversal is impossible, and bytes are still served through the handler
  (Content-Type + nosniff), never via a bare directory listing.
- `MemoryBlobStore` — in-memory, for tests and no-database dev mounts.

### Transform policy: re-encode raster; reject SVG and animated

Allowed input formats: **PNG, JPEG, GIF, WebP**. Every upload is decoded and
**re-encoded** to a canonical form — lossless PNG for PNG and static GIF, q85
JPEG for JPEG and WebP. Re-encoding is the strongest available guarantee: it
strips EXIF (including GPS), discards trailing/leading bytes, and neutralizes
polyglots (a file that is both a valid GIF and valid HTML/JS does not survive a
decode→encode round trip).

- **SVG is rejected.** It is XML, not a raster format, so it fails the decode
  gate; there is no safe-by-construction SVG, and a sanitized-SVG path is
  deferred until something needs vector images.
- **Animated GIF is rejected** (detected via multi-frame decode). Static GIF
  re-encodes to PNG. WebP animation is not preserved (decoded to its first
  frame) — a documented limitation, not a guarantee.

### Serving: the module owns the GET handler

`media` serves bytes from its own handler so the security headers live in one
audited place: the stored `Content-Type`, `X-Content-Type-Options: nosniff`
(defeats content sniffing), a long immutable `Cache-Control` and `ETag`
(content-addressed, so the bytes for a URL never change), and
`Content-Disposition: inline`. A configurable public base URL lets a host front
the bytes with a CDN or a separate origin for defense-in-depth.

## The security pipeline

One function, pure and Postgres-free, is the heart of the module and the focus
of the test corpus:

```
Process(r io.Reader, cfg Config) (Processed, error)
```

1. **Bound the input.** `io.LimitReader` to `cfg.MaxUploadBytes`; read fully.
   The client's declared content-type and filename are ignored.
2. **Sniff.** Magic-byte detection selects a decoder. Extension and client type
   are never trusted.
3. **Guard dimensions.** `image.DecodeConfig` reads width/height cheaply;
   reject if `width*height > cfg.MaxPixels` *before* a full decode, so a
   decompression bomb (a tiny file declaring 50000×50000) is refused without
   allocating it.
4. **Decode + animation check.** Decode the image; for GIF, a multi-frame
   decode rejects animation.
5. **Re-encode** to the canonical format → strips metadata, kills polyglots.
6. **Content-address.** `hash = sha256(reencoded)`; the hash is the object id
   and the URL slug. Identical uploads dedupe.

`Processed` carries the safe bytes, canonical content-type, width, height, and
hash. The store persists metadata + bytes (via `BlobStore`); the handler maps
HTTP ⇆ pipeline.

## Host integration surface

```go
store := media.NewStore(pool)               // metadata, Postgres
blobs := media.NewPostgresBlobStore(pool)    // bytes, Postgres (default)
h := media.NewHandler(store, blobs, media.Config{
    MaxUploadBytes: 10 << 20,
    MaxPixels:      25_000_000,
    PublicBaseURL:  "/media",                // or "https://cdn.example.com"
    Authorize:      func(r *http.Request) (owner string, ok bool) { ... },
})
mux.Handle("/media/", http.StripPrefix("/media", h.Routes()))
media.Migrate(ctx, pool)
```

- `POST {mount}` — multipart field `image`. Runs the pipeline; on success
  `201` + JSON `{ "url": "...", "width": N, "height": N }`. mdedit's adapter
  inserts `![](url)`.
- `GET {mount}/{hash}` — serves the bytes with the headers above.
- `Authorize` gates uploads (host auth); a rate-limit hook guards public
  intake. CSRF on the upload POST is the host's responsibility, per the suite
  conventions (the handler verifies `Origin`/`Referer` when configured).

## Testing

A malicious-upload corpus, mirroring `markdown`'s XSS corpus and grown the same
way (new vectors are added, never removed):

- polyglots: GIF/JS, JPEG with appended HTML, PNG with trailing script;
- SVG with `<script>` (rejected);
- animated GIF (rejected);
- decompression bomb: small file, huge declared dimensions (rejected at
  `DecodeConfig`);
- magic/extension mismatch (`.png` that is really JPEG — accepted as JPEG by
  content, never by extension);
- EXIF with GPS (assert stripped from the re-encoded output);
- truncated / zero-byte / non-image (PDF, HTML) — rejected.

The pipeline corpus needs no database. Store and handler get Postgres-backed
tests via testcontainers behind the `integration` build tag, plus an in-memory
`BlobStore` and a fake store interface for fast handler unit tests.

## Versioning

Path-prefixed tags (`media/vX.Y.Z`), pre-1.0. `Config`, `Process`/`Processed`,
`BlobStore`, `NewStore`/`NewHandler`/`Migrate`, and the upload/serve HTTP
contract are the public surface.
