package media

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"runtime"
	"testing"
)

var (
	white = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	black = color.RGBA{A: 0xff}
)

// twoFrameGIF returns an animated GIF on a screenW×screenH logical screen whose
// first frame is white and occupies frame0 (which may be offset and smaller
// than the screen), and whose second frame is solid black.
func twoFrameGIF(t *testing.T, screenW, screenH int, frame0 image.Rectangle) []byte {
	t.Helper()
	pal := color.Palette{black, white}
	f0 := image.NewPaletted(frame0, pal)
	for i := range f0.Pix {
		f0.Pix[i] = 1 // white
	}
	f1 := image.NewPaletted(image.Rect(0, 0, screenW, screenH), pal)
	g := &gif.GIF{
		Image:  []*image.Paletted{f0, f1},
		Delay:  []int{10, 10},
		Config: image.Config{ColorModel: pal, Width: screenW, Height: screenH},
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatalf("encode two-frame gif: %v", err)
	}
	return b.Bytes()
}

func decodeOutput(t *testing.T, p Processed) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(p.Bytes))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	return img
}

func rgba(c color.Color) color.RGBA {
	return color.RGBAModel.Convert(c).(color.RGBA)
}

func TestProcess_FlattenAnimatedGIFKeepsFirstFrame(t *testing.T) {
	in := twoFrameGIF(t, 8, 8, image.Rect(0, 0, 8, 8))
	got, err := Process(bytes.NewReader(in), Config{FlattenAnimation: true})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got.ContentType != "image/webp" {
		t.Errorf("content type = %q, want image/webp", got.ContentType)
	}
	checkDims(t, got, 8, 8)
	if c := rgba(decodeOutput(t, got).At(4, 4)); c != white {
		t.Errorf("pixel = %v, want first frame's white", c)
	}
}

// A first frame smaller than the logical screen is placed at its offset on a
// transparent screen-sized canvas, as a browser would render it.
func TestProcess_FlattenOffsetFirstFrame(t *testing.T) {
	in := twoFrameGIF(t, 8, 8, image.Rect(4, 4, 8, 8))
	got, err := Process(bytes.NewReader(in), Config{FlattenAnimation: true})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	checkDims(t, got, 8, 8)
	out := decodeOutput(t, got)
	if c := rgba(out.At(6, 6)); c != white {
		t.Errorf("inside frame = %v, want white", c)
	}
	if _, _, _, a := out.At(1, 1).RGBA(); a != 0 {
		t.Errorf("outside frame alpha = %d, want transparent", a)
	}
}

func TestProcess_FlattenOffByDefault(t *testing.T) {
	in := twoFrameGIF(t, 8, 8, image.Rect(0, 0, 8, 8))
	if _, err := Process(bytes.NewReader(in), Config{}); !errors.Is(err, ErrAnimated) {
		t.Fatalf("err = %v, want ErrAnimated", err)
	}
}

// Flattening decodes only the first frame, so a many-frame GIF costs one frame.
func TestProcess_FlattenIsCheap(t *testing.T) {
	// 200 frames of 1000x1000: a full decode is ~200 MB.
	data := manyFrameGIFBytes(t, 1000, 1000, 200)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got, err := Process(bytes.NewReader(data), Config{FlattenAnimation: true})
	runtime.ReadMemStats(&after)

	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	checkDims(t, got, 1000, 1000)
	const budget = 64 << 20
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > budget {
		t.Fatalf("Process allocated %d MB flattening, want < %d MB", alloc>>20, budget>>20)
	}
}

// The WebP decoder cannot read the animated VP8X container, so animated WebP is
// still refused with flattening on.
func TestProcess_FlattenStillRejectsAnimatedWebP(t *testing.T) {
	if _, err := Process(bytes.NewReader(animatedWebPBytes(t)), Config{FlattenAnimation: true}); err == nil {
		t.Fatal("animated WebP accepted with FlattenAnimation, want rejection")
	}
}
