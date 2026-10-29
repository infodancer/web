package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"runtime"
	"strings"
	"testing"

	"github.com/HugoSmits86/nativewebp"
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

// manyFrameGIFBytes returns an animated GIF of n w×h frames. All frames are a
// single color, so LZW compresses each to a few KB while a full decode costs
// w*h bytes per frame: a small upload that is expensive to decode completely.
func manyFrameGIFBytes(t *testing.T, w, h, n int) []byte {
	t.Helper()
	fr := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White})
	g := &gif.GIF{}
	for range n {
		g.Image = append(g.Image, fr)
		g.Delay = append(g.Delay, 0)
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatalf("encode many-frame gif: %v", err)
	}
	return b.Bytes()
}

// webpBytes returns a lossless-WebP-encoded solid image. nativewebp is the same
// encoder Process uses, so this exercises the WebP→WebP path with valid input.
func webpBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := nativewebp.Encode(&b, solidImage(w, h), nil); err != nil {
		t.Fatalf("encode webp: %v", err)
	}
	return b.Bytes()
}

// alphaPNGBytes returns a PNG with a genuinely transparent region so the WebP
// re-encode can be checked for alpha preservation.
func alphaPNGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := uint8(0xff)
			if x < w/2 {
				a = 0x00 // left half fully transparent
			}
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 0x40, A: a})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("encode alpha png: %v", err)
	}
	return b.Bytes()
}

// animatedWebPBytes returns a two-frame animated WebP (VP8X container). Animation
// must be rejected, mirroring the animated-GIF guard.
func animatedWebPBytes(t *testing.T) []byte {
	t.Helper()
	frame := solidImage(4, 4)
	ani := &nativewebp.Animation{
		Images:    []image.Image{frame, frame},
		Durations: []uint{100, 100},
		Disposals: []uint{0, 0},
		LoopCount: 0,
	}
	var b bytes.Buffer
	if err := nativewebp.EncodeAll(&b, ani, nil); err != nil {
		t.Fatalf("encode animated webp: %v", err)
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
	binary.BigEndian.PutUint32(ihdr[0:4], w)
	binary.BigEndian.PutUint32(ihdr[4:8], h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // color type: truecolor
	// compression=0, filter=0, interlace=0 already zero

	// chunk: length, "IHDR", data, crc32(type+data)
	var u32 [4]byte
	binary.BigEndian.PutUint32(u32[:], 13) // IHDR data length
	b.Write(u32[:])
	typeAndData := append([]byte("IHDR"), ihdr...)
	b.Write(typeAndData)
	binary.BigEndian.PutUint32(u32[:], crc32.ChecksumIEEE(typeAndData))
	b.Write(u32[:])
	return b.Bytes()
}

const svgWithScript = `<?xml version="1.0"?>
<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10">
  <script>alert(1)</script>
</svg>`

// --- tests ------------------------------------------------------------------

// A PNG is re-encoded to lossless WebP (the canonical raster for non-JPEG).
func TestProcess_PNGBecomesWebP(t *testing.T) {
	in := pngBytes(t, 20, 12)
	got, err := Process(bytes.NewReader(in), Config{})
	if err != nil {
		t.Fatalf("Process png: %v", err)
	}
	if got.ContentType != "image/webp" {
		t.Errorf("content type = %q, want image/webp", got.ContentType)
	}
	if got.Width != 20 || got.Height != 12 {
		t.Errorf("dims = %dx%d, want 20x12", got.Width, got.Height)
	}
	if got.SHA256 == "" || len(got.SHA256) != 64 {
		t.Errorf("sha256 = %q, want 64 hex chars", got.SHA256)
	}
	if format := sniffFormat(t, got.Bytes); format != "webp" {
		t.Errorf("output format = %q, want webp", format)
	}
}

// A WebP upload stays WebP — no conversion to JPEG, no generation loss.
func TestProcess_WebPStaysWebP(t *testing.T) {
	got, err := Process(bytes.NewReader(webpBytes(t, 18, 10)), Config{})
	if err != nil {
		t.Fatalf("Process webp: %v", err)
	}
	if got.ContentType != "image/webp" {
		t.Errorf("content type = %q, want image/webp", got.ContentType)
	}
	if got.Width != 18 || got.Height != 10 {
		t.Errorf("dims = %dx%d, want 18x10", got.Width, got.Height)
	}
	if format := sniffFormat(t, got.Bytes); format != "webp" {
		t.Errorf("output format = %q, want webp", format)
	}
}

// PNG transparency must survive the WebP re-encode (VP8L carries alpha).
func TestProcess_PNGAlphaSurvivesWebP(t *testing.T) {
	got, err := Process(bytes.NewReader(alphaPNGBytes(t, 16, 16)), Config{})
	if err != nil {
		t.Fatalf("Process alpha png: %v", err)
	}
	img, _, err := image.Decode(bytes.NewReader(got.Bytes))
	if err != nil {
		t.Fatalf("decode webp output: %v", err)
	}
	// The left-half pixels were fully transparent; they must decode as such.
	_, _, _, a := img.At(0, 0).RGBA()
	if a != 0 {
		t.Errorf("top-left alpha = %d, want 0 (transparent preserved)", a>>8)
	}
	if op, ok := img.(interface{ Opaque() bool }); ok && op.Opaque() {
		t.Errorf("re-encoded image reports opaque; alpha channel was dropped")
	}
}

// sniffFormat returns the format name image.DecodeConfig detects for b.
func sniffFormat(t *testing.T, b []byte) string {
	t.Helper()
	_, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return format
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

// A static GIF is re-encoded to lossless WebP (canonical lossless raster).
func TestProcess_StaticGIFBecomesWebP(t *testing.T) {
	got, err := Process(bytes.NewReader(staticGIFBytes(t, 8, 8)), Config{})
	if err != nil {
		t.Fatalf("Process gif: %v", err)
	}
	if got.ContentType != "image/webp" {
		t.Errorf("content type = %q, want image/webp", got.ContentType)
	}
	if format := sniffFormat(t, got.Bytes); format != "webp" {
		t.Errorf("output format = %q, want webp", format)
	}
}

func TestProcess_RejectsAnimatedGIF(t *testing.T) {
	_, err := Process(bytes.NewReader(animatedGIFBytes(t)), Config{})
	if !errors.Is(err, ErrAnimated) {
		t.Fatalf("err = %v, want ErrAnimated", err)
	}
}

// An animated GIF must be rejected without decoding every frame. Before the
// block walker, Process called gif.DecodeAll, so a ~1 MB upload of 60 large
// single-color frames allocated ~240 MB; the default 10 MiB cap allowed tens
// of GB from one upload.
func TestProcess_AnimatedGIFRejectionIsCheap(t *testing.T) {
	data := manyFrameGIFBytes(t, 2000, 2000, 60)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := Process(bytes.NewReader(data), Config{})
	runtime.ReadMemStats(&after)

	if !errors.Is(err, ErrAnimated) {
		t.Fatalf("err = %v, want ErrAnimated", err)
	}
	const budget = 32 << 20
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > budget {
		t.Fatalf("Process allocated %d MB rejecting an animated GIF, want < %d MB", alloc>>20, budget>>20)
	}
}

// Animated WebP must be rejected, mirroring the animated-GIF guard. The WebP
// decoder does not implement the animated VP8X container, so the upload is
// refused at format detection.
func TestProcess_RejectsAnimatedWebP(t *testing.T) {
	_, err := Process(bytes.NewReader(animatedWebPBytes(t)), Config{})
	if err == nil {
		t.Fatal("animated WebP accepted, want rejection")
	}
	if !errors.Is(err, ErrUnsupportedFormat) && !errors.Is(err, ErrDecode) && !errors.Is(err, ErrAnimated) {
		t.Fatalf("err = %v, want a rejection (unsupported/decode/animated)", err)
	}
}

// A long thin strip slips under the pixel cap but exceeds the WebP per-side
// limit; it must be refused before a full decode.
func TestProcess_RejectsOverWideForWebP(t *testing.T) {
	_, err := Process(bytes.NewReader(bombPNG(t, uint32(webpMaxDimension+1), 1)), Config{})
	if !errors.Is(err, ErrDimensionsTooLarge) {
		t.Fatalf("err = %v, want ErrDimensionsTooLarge", err)
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
