# CLAUDE.md

Guidance for Claude Code when working in the **media** module.

## What this is

A reusable image upload/store/serve component for infodancer websites. See
`DESIGN.md` for architecture and `README.md` for the integration surface; coding
standards and the mount contract are in the root
[`CONVENTIONS.md`](../CONVENTIONS.md).

## Hard rules

- **Untrusted in, safe out.** Every upload is sniffed by content, allowlisted
  to PNG/JPEG/GIF/WebP, dimension-guarded before decode, and **re-encoded** to
  strip metadata and kill polyglots — lossless WebP for PNG/GIF/WebP, q85 JPEG
  for JPEG (WebP encode is pure Go via nativewebp; no cgo). Never accept an image
  by extension or the client's declared type, and never serve original bytes
  without the re-encode. SVG, animated GIF, and animated WebP are rejected
  (`FlattenAnimation` lets a caller keep an animated GIF's first frame as a
  static image; animation itself is never stored) — do not "just allow" them
  without a hardened sanitizer and explicit approval.
- **The corpus is the contract.** `image_test.go` is a malicious-upload corpus
  driven TDD-style: add the failing payload test first, then the code. New
  vectors are *added*, never removed.
- **Serving headers are load-bearing.** `X-Content-Type-Options: nosniff` and
  the stored `Content-Type` must always be set on the serve path; the
  content-addressed `ETag`/immutable cache depend on bytes never changing for a
  hash.
- **The host owns auth/authz/CSRF/rate-limit.** They are `Config` hooks, not
  module policy. The subject (owning record id) is an opaque string — never a
  foreign key into a host table.
- **Category is server-side, never client-set.** It is `Config.Category`, fixed
  per handler and validated by `NewHandler`; one handler per category over a
  shared store. The client never names a category, and uploads are always tied
  to an owning record via `Authorize` (no free-floating uploads). Keep it that
  way — do not add a client-supplied category path/param.
- **The `BlobStore` seam stays clean.** Byte storage is swappable; keep the
  pipeline, metadata store, and handler independent of the concrete backend.
- **Migrations are append-only and namespaced.** New schema goes in a new
  `migrations/NNNNN_*.sql`; goose history lives in `media_goose_db_version`.

## Workflow

- Module path `github.com/infodancer/web/media`, Go 1.26.10, part of the parent
  `go.work`. Build with `GOWORK=off` to match CI.
- After editing `db/queries.sql` or `migrations/`, run `task generate` to
  regenerate the `db` package (sqlc), then `task check`.
- `task test` is unit-only (no Docker). Integration tests need Docker and the
  `integration` build tag: `task test:integration`.
- Per infodancer conventions: GitHub issue before branching
  (`feature/<issue>` or `bug/<issue>`), commits reference the issue, PRs merge
  to main.

## Roadmap notes

- `BlobStore` backends shipped: `PostgresBlobStore` (default),
  `FilesystemBlobStore` (sharded, atomic writes), `MemoryBlobStore` (tests/dev).
- Thumbnails/derived sizes shipped (v0.2): `Config.Thumbnails` pregenerates
  variants at upload time (FitInside/FitCover, never upscale); they are stored
  as ordinary content-addressed blobs (served by the existing GET path) with a
  `media_variants` map table; `BackfillThumbnails` fills an existing corpus.
- Deletion shipped: `BlobStore.Delete` on every backend, `Store.DeleteObject`
  (rows in one transaction, reports blobs no longer referenced), and
  `media.Delete` (rows first, then unreferenced blobs). No HTTP delete route:
  who may delete what is host policy.
- Deferred: an object-store (S3) `BlobStore` backend; lossy-WebP thumbnails (would
  need cgo); a hardened sanitized-SVG path; per-owner quotas.
