# Security Policy — media module

## Reporting

Part of the [infodancer/web](../README.md) monorepo. Use
[GitHub's private vulnerability reporting](https://github.com/infodancer/web/security/advisories/new).
Do not open public issues for security problems.

## Security model

`media` treats every upload as hostile. Serving user-uploaded files is a
stored-XSS and privacy vector; this module is the one audited place that risk is
contained.

- **Content decides, not the client.** The declared content-type, the form
  field's filename, and the extension are all ignored. The format is sniffed
  from the bytes, and acceptance requires a successful decode as an allowed
  raster format (PNG, JPEG, GIF, WebP).

- **Re-encode is the guarantee.** Every accepted image is decoded and
  re-encoded to a canonical raster. This strips EXIF (including GPS), discards
  any leading/trailing bytes, and neutralizes polyglots — a file that is both a
  valid image and valid HTML/JS does not survive a decode→encode round trip.

- **SVG is rejected.** It is XML, not a raster format, so it fails the decode
  gate. There is no safe-by-construction SVG; a sanitized-SVG path is a
  deliberate non-goal for v0.1.

- **Decompression bombs are refused before allocation.** Dimensions are read
  from the header (`image.DecodeConfig`) and checked against a `MaxPixels` cap
  *before* a full decode, so a tiny file declaring 50000×50000 is rejected
  without ever being decoded into memory. Upload size is independently capped.

- **Serving is locked down.** Bytes are served with the stored `Content-Type`,
  `X-Content-Type-Options: nosniff` (defeats content sniffing), and a
  content-addressed `ETag` with immutable caching. A configurable public base
  URL lets a host serve from a separate origin or CDN for defense in depth.

- **Category is server-side; uploads are tied to a record.** The category
  (`Config.Category`) is fixed per handler and never read from client input, so
  a client cannot direct bytes into another bucket. An upload is always tied to
  an owning record the user may write to: `Config.Authorize` returns that
  record's id and the permission decision, and `ok=false` rejects the upload —
  there are no free-floating uploads. The category and subject are opaque
  host-defined strings (`ValidCategory` constrains the category to a safe path
  segment), never foreign keys into a host table.

- **Boundaries the host owns.** Authentication, authorization, CSRF, and the
  rate-limit policy on the public intake are the host's responsibility, exposed
  as hooks on `Config`. The handler enforces a same-origin guard and the
  `Authorize` gate, but does not decide who may upload.

- **The corpus is the contract.** `image_test.go` carries a malicious-upload
  corpus (polyglots, SVG, animated, decompression bomb, oversize, garbage,
  zero-byte) that runs on every build. New attack vectors are added to it, never
  removed.

## Known limitations (v0.1)

- Animated WebP and animated GIF are rejected (animation is not supported), as
  is SVG. A caller may opt in to `FlattenAnimation`, which keeps an animated
  GIF's first frame as a static image; animated WebP is rejected either way.
- Re-encoding is lossy for JPEG sources (q85) by design; PNG/GIF/WebP re-encode
  to lossless WebP. The safety win (EXIF/polyglot stripping) is the deliberate
  trade.
- A WebP image side may not exceed 16383px (the VP8L bitstream limit); larger
  sources are refused.
