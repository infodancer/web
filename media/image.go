package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math"

	"github.com/HugoSmits86/nativewebp"

	// High-quality pure-Go downscaler for thumbnail generation (CatmullRom).
	// x/image is already a dependency (the WebP decoder below); /draw adds no
	// cgo and no new module.
	xdraw "golang.org/x/image/draw"

	// Register decoders for every format Process accepts. Go's image registry
	// is populated by each decoder package's init(), so a format the switch
	// lists ("png") is still rejected by image.DecodeConfig unless its decoder
	// is linked in. image/jpeg above is imported directly for its encoder; GIF,
	// PNG, and WebP are decode-only here (Process re-encodes to WebP/JPEG), so
	// they are blank imports.
	_ "image/gif"
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
	// Thumbnails holds the derived sizes requested via Config.Thumbnails, in the
	// same order. Each is a safe, content-addressed image produced from the same
	// decoded source and re-encoded with the same format policy as the original.
	// Nil when no thumbnails were configured.
	Thumbnails []Thumbnail
}

// Thumbnail is one derived size produced by [Process]. Like [Processed] it is a
// re-encoded, content-addressed image (its own SHA256), carrying the spec Name
// that requested it so a caller can map it back to a [ThumbnailSpec].
type Thumbnail struct {
	Name        string // the requesting ThumbnailSpec.Name
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
// guard); reject animation, or with Config.FlattenAnimation keep only the
// first frame of an animated GIF; decode; scale down to Config.MaxWidth/MaxHeight
// if set; re-encode to a canonical raster
// (lossless WebP for PNG, static GIF, and WebP; q85 JPEG for JPEG, which would
// only bloat under lossless), which strips EXIF and discards any
// trailing/leading polyglot bytes; and finally content-address the result with
// SHA-256.
func Process(r io.Reader, cfg Config) (Processed, error) {
	// Fail fast on a misconfigured Config before reading or decoding anything.
	if err := validateProcessConfig(cfg); err != nil {
		return Processed{}, err
	}

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
		// Count frames from the block structure, not by decoding them: a full
		// multi-frame decode is a memory bomb (see gifFrameCount).
		frames, err := gifFrameCount(data)
		if err != nil {
			return Processed{}, ErrDecode
		}
		if frames > 1 && !cfg.FlattenAnimation {
			return Processed{}, ErrAnimated
		}
	}

	// For GIF this decodes only the first frame (image/gif's Decode stops there),
	// which is what makes flattening as cheap as a static image.
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Processed{}, ErrDecode
	}
	if format == "gif" {
		img = onLogicalScreen(img, cfgImg.Width, cfgImg.Height)
	}
	img = fitWithin(img, cfg.MaxWidth, cfg.MaxHeight)

	out, contentType, err := reencode(img, format)
	if err != nil {
		return Processed{}, err
	}

	// Derive the requested thumbnails from the one decoded source (no re-decode
	// per size). Each is re-encoded with the same format policy as the original.
	thumbs, err := generateThumbnails(img, format, cfg.Thumbnails)
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
		Thumbnails:  thumbs,
	}, nil
}

// generateThumbnails downscales src once per spec and re-encodes each with the
// same format policy as the original (JPEG source -> JPEG thumb, else lossless
// WebP). It is shared by [Process] and [BackfillThumbnails]; specs are assumed
// already validated by [validateThumbnailSpecs]. Returns nil for no specs.
func generateThumbnails(src image.Image, format string, specs []ThumbnailSpec) ([]Thumbnail, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	thumbs := make([]Thumbnail, 0, len(specs))
	for _, spec := range specs {
		scaled := resize(src, spec)
		data, contentType, err := reencode(scaled, format)
		if err != nil {
			return nil, fmt.Errorf("media: thumbnail %q: %w", spec.Name, err)
		}
		sum := sha256.Sum256(data)
		b := scaled.Bounds()
		thumbs = append(thumbs, Thumbnail{
			Name:        spec.Name,
			Bytes:       data,
			ContentType: contentType,
			Width:       b.Dx(),
			Height:      b.Dy(),
			SHA256:      hex.EncodeToString(sum[:]),
		})
	}
	return thumbs, nil
}

// resize downscales src per spec, never upscaling. For FitInside the result fits
// within MaxWidth×MaxHeight with the aspect ratio preserved; for FitCover it is
// the largest centered crop of src matching the box aspect ratio, scaled down to
// at most MaxWidth×MaxHeight. The high-quality CatmullRom kernel is used.
func resize(src image.Image, spec ThumbnailSpec) image.Image {
	sb := src.Bounds()
	srcW, srcH := sb.Dx(), sb.Dy()
	if srcW <= 0 || srcH <= 0 {
		return src
	}

	if spec.Fit == FitCover {
		// Largest centered sub-rectangle of src with the box aspect ratio.
		cropW, cropH := srcW, srcH
		if int64(srcW)*int64(spec.MaxHeight) > int64(srcH)*int64(spec.MaxWidth) {
			// Source is wider than the box: crop the width.
			cropW = scaleRound(srcH, spec.MaxWidth, spec.MaxHeight)
		} else {
			// Source is taller (or equal): crop the height.
			cropH = scaleRound(srcW, spec.MaxHeight, spec.MaxWidth)
		}
		cropW = clampInt(cropW, 1, srcW)
		cropH = clampInt(cropH, 1, srcH)
		cropRect := image.Rect(0, 0, cropW, cropH).
			Add(image.Pt(sb.Min.X+(srcW-cropW)/2, sb.Min.Y+(srcH-cropH)/2))

		// No upscale: shrink the crop toward the box, but never enlarge it.
		outW, outH := cropW, cropH
		if cropW > spec.MaxWidth {
			outW = spec.MaxWidth
			outH = clampInt(scaleRound(cropH, spec.MaxWidth, cropW), 1, spec.MaxHeight)
		}
		dst := image.NewRGBA(image.Rect(0, 0, outW, outH))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, cropRect, xdraw.Over, nil)
		return dst
	}

	// FitInside: scale down to fit within the box, aspect ratio preserved.
	if srcW <= spec.MaxWidth && srcH <= spec.MaxHeight {
		return src // already fits; no upscale, re-encode at native size
	}
	var outW, outH int
	if int64(srcW)*int64(spec.MaxHeight) > int64(srcH)*int64(spec.MaxWidth) {
		// Width is the binding constraint.
		outW = spec.MaxWidth
		outH = clampInt(scaleRound(srcH, spec.MaxWidth, srcW), 1, spec.MaxHeight)
	} else {
		outH = spec.MaxHeight
		outW = clampInt(scaleRound(srcW, spec.MaxHeight, srcH), 1, spec.MaxWidth)
	}
	dst := image.NewRGBA(image.Rect(0, 0, outW, outH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, xdraw.Over, nil)
	return dst
}

// onLogicalScreen places a GIF frame on its logical screen. A frame may be
// smaller than the screen and offset within it; a browser draws it at that
// offset over a transparent screen, so the canonical output does the same.
// image/gif rejects frames that extend past the screen, so the frame always
// fits.
func onLogicalScreen(frame image.Image, w, h int) image.Image {
	screen := image.Rect(0, 0, w, h)
	if frame.Bounds() == screen {
		return frame
	}
	canvas := image.NewNRGBA(screen)
	xdraw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, xdraw.Src)
	return canvas
}

// fitWithin scales img down to fit maxW×maxH, aspect ratio preserved, never
// upscaling. A zero limit leaves that side unbounded.
func fitWithin(img image.Image, maxW, maxH int) image.Image {
	if maxW == 0 && maxH == 0 {
		return img
	}
	b := img.Bounds()
	if maxW == 0 {
		maxW = b.Dx()
	}
	if maxH == 0 {
		maxH = b.Dy()
	}
	return resize(img, ThumbnailSpec{MaxWidth: maxW, MaxHeight: maxH, Fit: FitInside})
}

// scaleRound returns round(v * num / den), the standard aspect-preserving scale.
func scaleRound(v, num, den int) int {
	if den == 0 {
		return v
	}
	return int(math.Round(float64(v) * float64(num) / float64(den)))
}

// clampInt constrains v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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
