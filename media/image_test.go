package media

import (
	"bytes"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// --- fixtures ---------------------------------------------------------------

// solidImage returns a w×h image with a recognizable, non-uniform pattern so a
// re-encode round trip is lossy-but-recognizable and dimensions are checkable.
func solidImage(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xff})
		}
	}
	return img
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, solidImage(w, h)); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return b.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, solidImage(w, h), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return b.Bytes()
}

func staticGIFBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := gif.Encode(&b, solidImage(w, h), nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return b.Bytes()
}

func animatedGIFBytes(t *testing.T) []byte {
	t.Helper()
	pal := color.Palette{color.Black, color.White}
	frame := func() *image.Paletted {
		p := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
		return p
	}
	g := &gif.GIF{
		Image:     []*image.Paletted{frame(), frame()},
		Delay:     []int{10, 10},
		LoopCount: 0,
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatalf("encode animated gif: %v", err)
	}
	return b.Bytes()
}

// bombPNG forges a PNG with only a valid signature + IHDR declaring w×h. It has
// no IDAT, so a full Decode would fail — but DecodeConfig reads the dimensions
// from IHDR alone, which is exactly where the decompression-bomb guard fires.
func bombPNG(t *testing.T, w, h uint32) []byte {
	t.Helper()
	var b bytes.Buffer
	b.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) // signature

	ihdr := make([]byte, 13)
	be := func(off int, v uint32) {
		ihdr[off] = byte(v >> 24)
		ihdr[off+1] = byte(v >> 16)
		ihdr[off+2] = byte(v >> 8)
		ihdr[off+3] = byte(v)
	}
	be(0, w)
	be(4, h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // color type: truecolor
	// compression=0, filter=0, interlace=0 already zero

	// chunk: length, "IHDR", data, crc32(type+data)
	length := uint32(len(ihdr))
	b.Write([]byte{byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)})
	typeAndData := append([]byte("IHDR"), ihdr...)
	b.Write(typeAndData)
	crc := crc32.ChecksumIEEE(typeAndData)
	b.Write([]byte{byte(crc >> 24), byte(crc >> 16), byte(crc >> 8), byte(crc)})
	return b.Bytes()
}

const svgWithScript = `<?xml version="1.0"?>
<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10">
  <script>alert(1)</script>
</svg>`

// --- tests ------------------------------------------------------------------

func TestProcess_AcceptsPNG(t *testing.T) {
	in := pngBytes(t, 20, 12)
	got, err := Process(bytes.NewReader(in), Config{})
	if err != nil {
		t.Fatalf("Process png: %v", err)
	}
	if got.ContentType != "image/png" {
		t.Errorf("content type = %q, want image/png", got.ContentType)
	}
	if got.Width != 20 || got.Height != 12 {
		t.Errorf("dims = %dx%d, want 20x12", got.Width, got.Height)
	}
	if got.SHA256 == "" || len(got.SHA256) != 64 {
		t.Errorf("sha256 = %q, want 64 hex chars", got.SHA256)
	}
	if _, _, err := image.Decode(bytes.NewReader(got.Bytes)); err != nil {
		t.Errorf("output does not decode: %v", err)
	}
}

func TestProcess_AcceptsJPEG(t *testing.T) {
	got, err := Process(bytes.NewReader(jpegBytes(t, 16, 16)), Config{})
	if err != nil {
		t.Fatalf("Process jpeg: %v", err)
	}
	if got.ContentType != "image/jpeg" {
		t.Errorf("content type = %q, want image/jpeg", got.ContentType)
	}
}

// A static GIF is re-encoded to PNG (canonical lossless raster).
func TestProcess_StaticGIFBecomesPNG(t *testing.T) {
	got, err := Process(bytes.NewReader(staticGIFBytes(t, 8, 8)), Config{})
	if err != nil {
		t.Fatalf("Process gif: %v", err)
	}
	if got.ContentType != "image/png" {
		t.Errorf("content type = %q, want image/png", got.ContentType)
	}
}

func TestProcess_RejectsAnimatedGIF(t *testing.T) {
	_, err := Process(bytes.NewReader(animatedGIFBytes(t)), Config{})
	if !errors.Is(err, ErrAnimated) {
		t.Fatalf("err = %v, want ErrAnimated", err)
	}
}

func TestProcess_RejectsDecompressionBomb(t *testing.T) {
	// 50000×50000 = 2.5e9 pixels, well over the default cap, in a few bytes.
	_, err := Process(bytes.NewReader(bombPNG(t, 50000, 50000)), Config{})
	if !errors.Is(err, ErrTooManyPixels) {
		t.Fatalf("err = %v, want ErrTooManyPixels", err)
	}
}

func TestProcess_RejectsOversize(t *testing.T) {
	in := pngBytes(t, 64, 64)
	_, err := Process(bytes.NewReader(in), Config{MaxUploadBytes: 32})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestProcess_RejectsSVG(t *testing.T) {
	_, err := Process(strings.NewReader(svgWithScript), Config{})
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedFormat", err)
	}
}

func TestProcess_RejectsGarbage(t *testing.T) {
	_, err := Process(bytes.NewReader([]byte("not an image at all, just text")), Config{})
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedFormat", err)
	}
}

func TestProcess_RejectsZeroByte(t *testing.T) {
	_, err := Process(bytes.NewReader(nil), Config{})
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedFormat", err)
	}
}

// A polyglot: a valid PNG with a script payload appended. The re-encode round
// trip must drop the trailing bytes — the output is a clean image, not a file
// that is simultaneously a script.
func TestProcess_StripsTrailingPolyglotBytes(t *testing.T) {
	payload := []byte("<script>alert('polyglot')</script>")
	in := append(pngBytes(t, 10, 10), payload...)
	got, err := Process(bytes.NewReader(in), Config{})
	if err != nil {
		t.Fatalf("Process polyglot: %v", err)
	}
	if bytes.Contains(got.Bytes, payload) {
		t.Errorf("re-encoded output still contains the appended payload")
	}
}

// Identical uploads content-address to the same hash (dedupe).
func TestProcess_DedupesByContent(t *testing.T) {
	in := pngBytes(t, 24, 24)
	a, err := Process(bytes.NewReader(in), Config{})
	if err != nil {
		t.Fatalf("Process a: %v", err)
	}
	b, err := Process(bytes.NewReader(in), Config{})
	if err != nil {
		t.Fatalf("Process b: %v", err)
	}
	if a.SHA256 != b.SHA256 {
		t.Errorf("same input produced different hashes: %s vs %s", a.SHA256, b.SHA256)
	}
}
