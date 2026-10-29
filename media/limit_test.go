package media

import (
	"bytes"
	"image"
	"testing"
)

// checkDims asserts both the reported and the actually encoded dimensions.
func checkDims(t *testing.T, got Processed, w, h int) {
	t.Helper()
	if got.Width != w || got.Height != h {
		t.Errorf("reported dims = %dx%d, want %dx%d", got.Width, got.Height, w, h)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(got.Bytes))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if cfg.Width != w || cfg.Height != h {
		t.Errorf("encoded dims = %dx%d, want %dx%d", cfg.Width, cfg.Height, w, h)
	}
}

func TestProcess_MaxDimensions(t *testing.T) {
	cases := []struct {
		name         string
		in           func(t *testing.T) []byte
		maxW, maxH   int
		wantW, wantH int
		wantType     string
	}{
		{"no limit keeps native size", func(t *testing.T) []byte { return pngBytes(t, 300, 200) }, 0, 0, 300, 200, "image/webp"},
		{"landscape is width-bound", func(t *testing.T) []byte { return pngBytes(t, 300, 200) }, 150, 150, 150, 100, "image/webp"},
		{"portrait is height-bound", func(t *testing.T) []byte { return pngBytes(t, 200, 300) }, 150, 150, 100, 150, "image/webp"},
		{"already fits is not upscaled", func(t *testing.T) []byte { return pngBytes(t, 80, 60) }, 150, 150, 80, 60, "image/webp"},
		{"width only", func(t *testing.T) []byte { return pngBytes(t, 300, 200) }, 150, 0, 150, 100, "image/webp"},
		{"height only", func(t *testing.T) []byte { return pngBytes(t, 300, 200) }, 0, 100, 150, 100, "image/webp"},
		{"jpeg stays jpeg", func(t *testing.T) []byte { return jpegBytes(t, 300, 200) }, 150, 150, 150, 100, "image/jpeg"},
		{"static gif", func(t *testing.T) []byte { return staticGIFBytes(t, 300, 200) }, 150, 150, 150, 100, "image/webp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Process(bytes.NewReader(tc.in(t)), Config{MaxWidth: tc.maxW, MaxHeight: tc.maxH})
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			checkDims(t, got, tc.wantW, tc.wantH)
			if got.ContentType != tc.wantType {
				t.Errorf("content type = %q, want %q", got.ContentType, tc.wantType)
			}
		})
	}
}

// Thumbnails derive from the capped output, so no variant exceeds the cap.
func TestProcess_ThumbnailsDeriveFromCappedOutput(t *testing.T) {
	cfg := Config{
		MaxWidth:  150,
		MaxHeight: 150,
		Thumbnails: []ThumbnailSpec{
			{Name: "big", MaxWidth: 1000, MaxHeight: 1000},
			{Name: "small", MaxWidth: 30, MaxHeight: 30},
		},
	}
	got, err := Process(bytes.NewReader(pngBytes(t, 600, 400)), cfg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	checkDims(t, got, 150, 100)
	if big := thumbByName(t, got.Thumbnails, "big"); big.Width != 150 || big.Height != 100 {
		t.Errorf("big thumb = %dx%d, want 150x100", big.Width, big.Height)
	}
	if small := thumbByName(t, got.Thumbnails, "small"); small.Width != 30 || small.Height != 20 {
		t.Errorf("small thumb = %dx%d, want 30x20", small.Width, small.Height)
	}
}

func TestProcess_RejectsNegativeMaxDimensions(t *testing.T) {
	for _, cfg := range []Config{{MaxWidth: -1}, {MaxHeight: -1}} {
		if _, err := Process(bytes.NewReader(pngBytes(t, 10, 10)), cfg); err == nil {
			t.Errorf("Process(%+v) accepted negative limit", cfg)
		}
	}
}
