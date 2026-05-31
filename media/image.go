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
	"image/png"
	"io"

	// Register the WebP decoder so DecodeConfig/Decode recognize it. WebP is
	// decode-only here; it is re-encoded to JPEG (animation not preserved).
	_ "golang.org/x/image/webp"
)

// Default limits, applied when a [Config] field is zero.
const (
	defaultMaxUploadBytes int64 = 10 << 20   // 10 MiB
	defaultMaxPixels      int   = 25_000_000 // 25 MP — guards decompression bombs
	jpegQuality           int   = 85
)

// Pipeline errors. Callers match with [errors.Is].
var (
	// ErrTooLarge means the upload exceeded Config.MaxUploadBytes.
	ErrTooLarge = errors.New("media: upload too large")
	// ErrTooManyPixels means width*height exceeded Config.MaxPixels — refused
	// before a full decode so a decompression bomb is never allocated.
	ErrTooManyPixels = errors.New("media: image dimensions too large")
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
	ContentType string // "image/png" or "image/jpeg"
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
// (lossless PNG for PNG and static GIF, q85 JPEG for JPEG and WebP), which
// strips EXIF and discards any trailing/leading polyglot bytes; and finally
// content-address the result with SHA-256.
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

// reencode writes img to a canonical format chosen by its source: JPEG (q85)
// for photographic sources (JPEG, WebP), lossless PNG for everything else.
func reencode(img image.Image, format string) (data []byte, contentType string, err error) {
	var buf bytes.Buffer
	switch format {
	case "jpeg", "webp":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return nil, "", fmt.Errorf("media: re-encode jpeg: %w", err)
		}
		return buf.Bytes(), "image/jpeg", nil
	default: // png, static gif
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", fmt.Errorf("media: re-encode png: %w", err)
		}
		return buf.Bytes(), "image/png", nil
	}
}
