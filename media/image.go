package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"io"

	"github.com/HugoSmits86/nativewebp"

	// Register decoders for every format Process accepts. Go's image registry
	// is populated by each decoder package's init(), so a format the switch
	// lists ("png") is still rejected by image.DecodeConfig unless its decoder
	// is linked in. image/jpeg and image/gif above are imported directly for
	// their encode/decode; PNG and WebP are decode-only here (Process re-encodes
	// to WebP/JPEG), so they are blank imports.
	_ "image/png"

	// The standard library has no WebP encoder; the lossless VP8L re-encode is
	// provided by nativewebp (pure Go, no cgo). Animation is not preserved.
	_ "golang.org/x/image/webp"
)

// Default limits, applied when a [Config] field is zero.
const (
	defaultMaxUploadBytes int64 = 10 << 20   // 10 MiB
	defaultMaxPixels      int   = 25_000_000 // 25 MP — guards decompression bombs
	jpegQuality           int   = 85

	// webpMaxDimension is the largest side length the WebP/VP8L bitstream can
	// represent (a 14-bit field, so 2^14 - 1). Sources re-encoded to WebP must
	// fit; it is checked from the header before decode, so an over-wide strip
	// that slips under the pixel cap (e.g. 25000000×1) is refused before a full
	// decode allocates it.
	webpMaxDimension int = 16383
)

// Pipeline errors. Callers match with [errors.Is].
var (
	// ErrTooLarge means the upload exceeded Config.MaxUploadBytes.
	ErrTooLarge = errors.New("media: upload too large")
	// ErrTooManyPixels means width*height exceeded Config.MaxPixels — refused
	// before a full decode so a decompression bomb is never allocated.
	ErrTooManyPixels = errors.New("media: image dimensions too large")
	// ErrDimensionsTooLarge means a source bound for WebP re-encode has a side
	// longer than the format can represent (see webpMaxDimension). It is
	// distinct from ErrTooManyPixels: the total pixel count may be small (a long
	// thin strip), but the WebP bitstream still cannot hold that dimension.
	ErrDimensionsTooLarge = errors.New("media: image dimension exceeds WebP maximum")
	// ErrUnsupportedFormat means the bytes are not a decodable allowed image
	// (PNG/JPEG/GIF/WebP). SVG, other XML, and non-images land here.
	ErrUnsupportedFormat = errors.New("media: unsupported image format")
	// ErrAnimated means an animated image was rejected (animated GIF).
	ErrAnimated = errors.New("media: animated images are not allowed")
	// ErrDecode means the format was recognized but the image failed to decode
	// (truncated or corrupt).
	ErrDecode = errors.New("media: image failed to decode")
)

// Processed is a safe, canonical image ready to store and serve. Bytes is the
// re-encoded output (metadata stripped, polyglots neutralized); SHA256 is its
// hex content address and serves as the object id and URL slug.
type Processed struct {
	Bytes       []byte
	ContentType string // "image/webp" or "image/jpeg"
	Width       int
	Height      int
	SHA256      string
}

// Process validates an untrusted image and returns a safe canonical form.
//
// The steps, in order: bound the input size; sniff the format from content
// (extension and any client-declared type are irrelevant — Process takes raw
// bytes); reject anything outside the PNG/JPEG/GIF/WebP allowlist; refuse
// oversized dimensions from the header before decoding (decompression-bomb
// guard); reject animation; decode and re-encode to a canonical raster
// (lossless WebP for PNG, static GIF, and WebP; q85 JPEG for JPEG, which would
// only bloat under lossless), which strips EXIF and discards any
// trailing/leading polyglot bytes; and finally content-address the result with
// SHA-256.
func Process(r io.Reader, cfg Config) (Processed, error) {
	maxBytes := cfg.MaxUploadBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxUploadBytes
	}
	maxPixels := cfg.MaxPixels
	if maxPixels <= 0 {
		maxPixels = defaultMaxPixels
	}

	// Read one byte past the cap so we can tell "exactly at the cap" from "over".
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return Processed{}, fmt.Errorf("media: read upload: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return Processed{}, ErrTooLarge
	}

	cfgImg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Processed{}, ErrUnsupportedFormat
	}
	switch format {
	case "png", "jpeg", "gif", "webp":
	default:
		return Processed{}, ErrUnsupportedFormat
	}

	if cfgImg.Width <= 0 || cfgImg.Height <= 0 {
		return Processed{}, ErrUnsupportedFormat
	}
	if int64(cfgImg.Width)*int64(cfgImg.Height) > int64(maxPixels) {
		return Processed{}, ErrTooManyPixels
	}
	// Everything but JPEG is re-encoded to WebP; refuse a side the WebP
	// bitstream can't hold before the full decode allocates it. A long thin
	// strip (e.g. 25000000×1) slips under the pixel cap but not this one.
	if format != "jpeg" && (cfgImg.Width > webpMaxDimension || cfgImg.Height > webpMaxDimension) {
		return Processed{}, ErrDimensionsTooLarge
	}

	if format == "gif" {
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return Processed{}, ErrDecode
		}
		if len(g.Image) > 1 {
			return Processed{}, ErrAnimated
		}
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Processed{}, ErrDecode
	}

	out, contentType, err := reencode(img, format)
	if err != nil {
		return Processed{}, err
	}

	sum := sha256.Sum256(out)
	b := img.Bounds()
	return Processed{
		Bytes:       out,
		ContentType: contentType,
		Width:       b.Dx(),
		Height:      b.Dy(),
		SHA256:      hex.EncodeToString(sum[:]),
	}, nil
}

// reencode writes img to a canonical format chosen by its source: q85 JPEG for
// JPEG (a photographic source that lossless WebP would only bloat), and
// lossless WebP (VP8L) for everything else (PNG, static GIF, WebP) — preserving
// alpha and adding no generation loss. WebP encoding is pure Go (nativewebp); a
// lossy WebP encoder would need cgo and is deliberately not used.
func reencode(img image.Image, format string) (data []byte, contentType string, err error) {
	var buf bytes.Buffer
	if format == "jpeg" {
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return nil, "", fmt.Errorf("media: re-encode jpeg: %w", err)
		}
		return buf.Bytes(), "image/jpeg", nil
	}
	// png, static gif, webp → lossless WebP.
	if err := nativewebp.Encode(&buf, img, nil); err != nil {
		return nil, "", fmt.Errorf("media: re-encode webp: %w", err)
	}
	return buf.Bytes(), "image/webp", nil
}
