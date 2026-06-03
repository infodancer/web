// Package images handles image decoding, resizing, and storage for the timeline tool.
package images

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

// Size defines a named image variant with target dimensions.
type Size struct {
	Name   string
	Width  int
	Height int
}

// PortraitSizes are the standard sizes generated for person portraits.
var PortraitSizes = []Size{
	{Name: "profile", Width: 256, Height: 256},
	{Name: "thumb", Width: 64, Height: 64},
}

// Result holds metadata about a processed image.
type Result struct {
	ContentType string
	Width       int
	Height      int
	FileSize    int
}

// ProcessPortrait reads an image, saves the original, and generates
// center-cropped square variants at each standard portrait size.
// outDir is created if it does not exist.
func ProcessPortrait(r io.Reader, outDir string) (Result, error) {
	return Process(r, outDir, PortraitSizes)
}

// Process reads an image, saves the original, and generates center-cropped
// square variants at each of the given sizes. outDir is created if needed.
func Process(r io.Reader, outDir string, sizes []Size) (Result, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create output dir: %w", err)
	}

	src, format, err := image.Decode(r)
	if err != nil {
		return Result{}, fmt.Errorf("decode image: %w", err)
	}

	bounds := src.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()

	var contentType string
	switch format {
	case "jpeg":
		contentType = "image/jpeg"
	case "png":
		contentType = "image/png"
	default:
		contentType = "image/" + format
	}

	// Save original.
	origPath := filepath.Join(outDir, "original."+formatExt(format))
	origSize, err := saveImage(src, origPath, format)
	if err != nil {
		return Result{}, fmt.Errorf("save original: %w", err)
	}

	// Generate each size variant as a center-cropped square JPEG.
	for _, sz := range sizes {
		cropped := centerCropSquare(src)
		dst := image.NewRGBA(image.Rect(0, 0, sz.Width, sz.Height))
		draw.CatmullRom.Scale(dst, dst.Bounds(), cropped, cropped.Bounds(), draw.Over, nil)

		variantPath := filepath.Join(outDir, sz.Name+".jpg")
		if _, err := saveJPEG(dst, variantPath, 85); err != nil {
			return Result{}, fmt.Errorf("save %s variant: %w", sz.Name, err)
		}
	}

	return Result{
		ContentType: contentType,
		Width:       origW,
		Height:      origH,
		FileSize:    origSize,
	}, nil
}

// centerCropSquare returns the largest centered square sub-image.
func centerCropSquare(src image.Image) image.Image {
	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	side := w
	if h < w {
		side = h
	}

	x0 := bounds.Min.X + (w-side)/2
	y0 := bounds.Min.Y + (h-side)/2

	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}

	if si, ok := src.(subImager); ok {
		return si.SubImage(image.Rect(x0, y0, x0+side, y0+side))
	}

	// Fallback: copy pixels into a new image.
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Copy(dst, image.Point{}, src, image.Rect(x0, y0, x0+side, y0+side), draw.Src, nil)
	return dst
}

func saveImage(img image.Image, path, format string) (int, error) {
	switch format {
	case "jpeg":
		return saveJPEG(img, path, 90)
	case "png":
		return savePNG(img, path)
	default:
		return saveJPEG(img, path, 90)
	}
}

func saveJPEG(img image.Image, path string, quality int) (int, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: quality}); err != nil {
		return 0, err
	}

	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return int(info.Size()), nil
}

func savePNG(img image.Image, path string) (int, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	if err := png.Encode(f, img); err != nil {
		return 0, err
	}

	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return int(info.Size()), nil
}

func formatExt(format string) string {
	switch format {
	case "jpeg":
		return "jpg"
	case "png":
		return "png"
	default:
		return format
	}
}
