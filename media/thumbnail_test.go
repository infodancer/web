package media

import (
	"bytes"
	"image"
	"testing"
)

// thumbByName finds a generated thumbnail by spec name.
func thumbByName(t *testing.T, ts []Thumbnail, name string) Thumbnail {
	t.Helper()
	for _, th := range ts {
		if th.Name == name {
			return th
		}
	}
	t.Fatalf("thumbnail %q not found in %d generated", name, len(ts))
	return Thumbnail{}
}

// Empty Config.Thumbnails generates nothing — the original is the only output.
func TestProcess_NoThumbnailsByDefault(t *testing.T) {
	got, err := Process(bytes.NewReader(pngBytes(t, 40, 40)), Config{})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(got.Thumbnails) != 0 {
		t.Errorf("got %d thumbnails, want 0", len(got.Thumbnails))
	}
}

// FitInside scales down preserving aspect ratio, fitting within the box.
func TestProcess_ThumbnailFitInside(t *testing.T) {
	cfg := Config{Thumbnails: []ThumbnailSpec{
		{Name: "thumb", MaxWidth: 100, MaxHeight: 100, Fit: FitInside},
	}}
	// 600x300 landscape -> width-bound: 100x50.
	got, err := Process(bytes.NewReader(pngBytes(t, 600, 300)), cfg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	th := thumbByName(t, got.Thumbnails, "thumb")
	if th.Width != 100 || th.Height != 50 {
		t.Errorf("dims = %dx%d, want 100x50", th.Width, th.Height)
	}
	if th.ContentType != "image/webp" {
		t.Errorf("content type = %q, want image/webp", th.ContentType)
	}
	if len(th.SHA256) != 64 {
		t.Errorf("sha256 = %q, want 64 hex chars", th.SHA256)
	}
	// The reported dims must match the actual encoded image.
	cfgImg, _, err := image.DecodeConfig(bytes.NewReader(th.Bytes))
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	if cfgImg.Width != th.Width || cfgImg.Height != th.Height {
		t.Errorf("encoded dims %dx%d != reported %dx%d", cfgImg.Width, cfgImg.Height, th.Width, th.Height)
	}
}

// FitInside on a portrait source is height-bound.
func TestProcess_ThumbnailFitInsidePortrait(t *testing.T) {
	cfg := Config{Thumbnails: []ThumbnailSpec{
		{Name: "thumb", MaxWidth: 100, MaxHeight: 100, Fit: FitInside},
	}}
	// 300x600 portrait -> height-bound: 50x100.
	got, err := Process(bytes.NewReader(pngBytes(t, 300, 600)), cfg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	th := thumbByName(t, got.Thumbnails, "thumb")
	if th.Width != 50 || th.Height != 100 {
		t.Errorf("dims = %dx%d, want 50x100", th.Width, th.Height)
	}
}

// FitCover produces exactly the box dimensions for a source large enough.
func TestProcess_ThumbnailFitCover(t *testing.T) {
	cfg := Config{Thumbnails: []ThumbnailSpec{
		{Name: "sq", MaxWidth: 100, MaxHeight: 100, Fit: FitCover},
	}}
	for _, dims := range []struct{ w, h int }{{600, 300}, {300, 600}, {500, 500}} {
		got, err := Process(bytes.NewReader(pngBytes(t, dims.w, dims.h)), cfg)
		if err != nil {
			t.Fatalf("Process %dx%d: %v", dims.w, dims.h, err)
		}
		th := thumbByName(t, got.Thumbnails, "sq")
		if th.Width != 100 || th.Height != 100 {
			t.Errorf("source %dx%d: cover dims = %dx%d, want 100x100", dims.w, dims.h, th.Width, th.Height)
		}
	}
}

// Neither fit mode upscales: a source smaller than the box is not enlarged.
func TestProcess_ThumbnailNeverUpscales(t *testing.T) {
	cfg := Config{Thumbnails: []ThumbnailSpec{
		{Name: "inside", MaxWidth: 500, MaxHeight: 500, Fit: FitInside},
		{Name: "cover", MaxWidth: 500, MaxHeight: 500, Fit: FitCover},
	}}
	// 80x40 source is smaller than both boxes.
	got, err := Process(bytes.NewReader(pngBytes(t, 80, 40)), cfg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	inside := thumbByName(t, got.Thumbnails, "inside")
	if inside.Width != 80 || inside.Height != 40 {
		t.Errorf("inside dims = %dx%d, want 80x40 (no upscale)", inside.Width, inside.Height)
	}
	cover := thumbByName(t, got.Thumbnails, "cover")
	// Cover keeps the box's 1:1 aspect at native scale: the largest centered
	// square within 80x40 is 40x40, not upscaled to 500x500.
	if cover.Width != 40 || cover.Height != 40 {
		t.Errorf("cover dims = %dx%d, want 40x40 (cropped, not upscaled)", cover.Width, cover.Height)
	}
}

// A JPEG source yields a JPEG thumbnail; non-JPEG yields WebP (same policy as
// the original).
func TestProcess_ThumbnailFormatFollowsSource(t *testing.T) {
	cfg := Config{Thumbnails: []ThumbnailSpec{{Name: "t", MaxWidth: 50, MaxHeight: 50}}}

	jp, err := Process(bytes.NewReader(jpegBytes(t, 200, 200)), cfg)
	if err != nil {
		t.Fatalf("Process jpeg: %v", err)
	}
	if th := thumbByName(t, jp.Thumbnails, "t"); th.ContentType != "image/jpeg" {
		t.Errorf("jpeg thumb content type = %q, want image/jpeg", th.ContentType)
	}

	for name, in := range map[string][]byte{
		"png":  pngBytes(t, 200, 200),
		"gif":  staticGIFBytes(t, 200, 200),
		"webp": webpBytes(t, 200, 200),
	} {
		out, err := Process(bytes.NewReader(in), cfg)
		if err != nil {
			t.Fatalf("Process %s: %v", name, err)
		}
		if th := thumbByName(t, out.Thumbnails, "t"); th.ContentType != "image/webp" {
			t.Errorf("%s thumb content type = %q, want image/webp", name, th.ContentType)
		}
	}
}

// A thumbnail is a real, decodable image with metadata stripped (re-encoded
// like the original).
func TestProcess_ThumbnailStripsTrailingPolyglotBytes(t *testing.T) {
	payload := []byte("<script>alert('polyglot')</script>")
	in := append(pngBytes(t, 100, 100), payload...)
	cfg := Config{Thumbnails: []ThumbnailSpec{{Name: "t", MaxWidth: 32, MaxHeight: 32}}}
	got, err := Process(bytes.NewReader(in), cfg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	th := thumbByName(t, got.Thumbnails, "t")
	if bytes.Contains(th.Bytes, payload) {
		t.Errorf("thumbnail still contains the appended payload")
	}
	if _, _, err := image.Decode(bytes.NewReader(th.Bytes)); err != nil {
		t.Errorf("thumbnail does not decode: %v", err)
	}
}

// Multiple specs each produce a distinct variant.
func TestProcess_MultipleThumbnails(t *testing.T) {
	cfg := Config{Thumbnails: []ThumbnailSpec{
		{Name: "small", MaxWidth: 64, MaxHeight: 64},
		{Name: "medium", MaxWidth: 256, MaxHeight: 256},
	}}
	got, err := Process(bytes.NewReader(pngBytes(t, 512, 512)), cfg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(got.Thumbnails) != 2 {
		t.Fatalf("got %d thumbnails, want 2", len(got.Thumbnails))
	}
	small := thumbByName(t, got.Thumbnails, "small")
	medium := thumbByName(t, got.Thumbnails, "medium")
	if small.SHA256 == medium.SHA256 {
		t.Errorf("distinct sizes share a hash: %s", small.SHA256)
	}
	if small.Width != 64 || medium.Width != 256 {
		t.Errorf("dims small=%d medium=%d, want 64 and 256", small.Width, medium.Width)
	}
}

// An invalid thumbnail spec is rejected by the pipeline before any work.
func TestProcess_RejectsInvalidThumbnailSpec(t *testing.T) {
	cases := map[string]ThumbnailSpec{
		"empty name": {Name: "", MaxWidth: 10, MaxHeight: 10},
		"bad name":   {Name: "Thumb!", MaxWidth: 10, MaxHeight: 10},
		"zero width": {Name: "t", MaxWidth: 0, MaxHeight: 10},
		"bad fit":    {Name: "t", MaxWidth: 10, MaxHeight: 10, Fit: FitMode(99)},
	}
	for name, spec := range cases {
		_, err := Process(bytes.NewReader(pngBytes(t, 20, 20)), Config{Thumbnails: []ThumbnailSpec{spec}})
		if err == nil {
			t.Errorf("%s: Process accepted invalid spec", name)
		}
	}
	// Duplicate names are rejected too.
	dup := []ThumbnailSpec{{Name: "t", MaxWidth: 10, MaxHeight: 10}, {Name: "t", MaxWidth: 20, MaxHeight: 20}}
	if _, err := Process(bytes.NewReader(pngBytes(t, 20, 20)), Config{Thumbnails: dup}); err == nil {
		t.Errorf("duplicate names accepted")
	}
}
