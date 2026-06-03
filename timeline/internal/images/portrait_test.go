package images

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// TestPortraitBytes confirms a non-square source is cropped and scaled to a
// PortraitDim×PortraitDim JPEG.
func TestPortraitBytes(t *testing.T) {
	// A 400x200 source (wider than tall).
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var in bytes.Buffer
	if err := jpeg.Encode(&in, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode source: %v", err)
	}

	data, contentType, w, h, err := PortraitBytes(&in)
	if err != nil {
		t.Fatalf("PortraitBytes: %v", err)
	}
	if contentType != "image/jpeg" {
		t.Errorf("content type: got %q, want image/jpeg", contentType)
	}
	if w != PortraitDim || h != PortraitDim {
		t.Errorf("dims: got %dx%d, want %dx%d", w, h, PortraitDim, PortraitDim)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode result config: %v", err)
	}
	if cfg.Width != PortraitDim || cfg.Height != PortraitDim {
		t.Errorf("decoded dims: got %dx%d, want square %d", cfg.Width, cfg.Height, PortraitDim)
	}
}
