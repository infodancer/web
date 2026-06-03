package images

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func makeTestJPEG(w, h int) *bytes.Buffer {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	return &buf
}

func makeTestPNG(w, h int) *bytes.Buffer {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return &buf
}

func TestProcessPortrait_JPEG(t *testing.T) {
	outDir := t.TempDir()
	buf := makeTestJPEG(400, 300)

	result, err := ProcessPortrait(buf, outDir)
	if err != nil {
		t.Fatalf("ProcessPortrait: %v", err)
	}

	if result.ContentType != "image/jpeg" {
		t.Errorf("content type = %q, want image/jpeg", result.ContentType)
	}
	if result.Width != 400 || result.Height != 300 {
		t.Errorf("dimensions = %dx%d, want 400x300", result.Width, result.Height)
	}
	if result.FileSize == 0 {
		t.Error("file size = 0")
	}

	// Check that all expected files were created.
	for _, name := range []string{"original.jpg", "profile.jpg", "thumb.jpg"} {
		path := filepath.Join(outDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing %s: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}

	// Verify profile dimensions.
	assertImageSize(t, filepath.Join(outDir, "profile.jpg"), 256, 256)
	assertImageSize(t, filepath.Join(outDir, "thumb.jpg"), 64, 64)
}

func TestProcessPortrait_PNG(t *testing.T) {
	outDir := t.TempDir()
	buf := makeTestPNG(500, 500)

	result, err := ProcessPortrait(buf, outDir)
	if err != nil {
		t.Fatalf("ProcessPortrait: %v", err)
	}

	if result.ContentType != "image/png" {
		t.Errorf("content type = %q, want image/png", result.ContentType)
	}

	// Original should be PNG, variants should be JPEG.
	if _, err := os.Stat(filepath.Join(outDir, "original.png")); err != nil {
		t.Errorf("missing original.png: %v", err)
	}
	assertImageSize(t, filepath.Join(outDir, "profile.jpg"), 256, 256)
}

func TestProcessPortrait_TallImage(t *testing.T) {
	outDir := t.TempDir()
	buf := makeTestJPEG(200, 600) // tall and narrow

	_, err := ProcessPortrait(buf, outDir)
	if err != nil {
		t.Fatalf("ProcessPortrait: %v", err)
	}

	// Should center-crop to 200x200 square, then scale.
	assertImageSize(t, filepath.Join(outDir, "profile.jpg"), 256, 256)
	assertImageSize(t, filepath.Join(outDir, "thumb.jpg"), 64, 64)
}

func assertImageSize(t *testing.T, path string, wantW, wantH int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode config %s: %v", path, err)
	}
	if cfg.Width != wantW || cfg.Height != wantH {
		t.Errorf("%s: size = %dx%d, want %dx%d", filepath.Base(path), cfg.Width, cfg.Height, wantW, wantH)
	}
}
