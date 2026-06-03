package images

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"

	"golang.org/x/image/draw"
)

// PortraitDim is the side length, in pixels, of the square portrait produced by
// [PortraitBytes].
const PortraitDim = 256

// PortraitBytes decodes an image, center-crops it to a square, scales it to a
// PortraitDim×PortraitDim JPEG, and returns the encoded bytes. Unlike
// [ProcessPortrait] it touches no filesystem — the caller stores the bytes
// wherever it likes (timeline hands them to the media blob store).
func PortraitBytes(r io.Reader) (data []byte, contentType string, width, height int, err error) {
	src, _, err := image.Decode(r)
	if err != nil {
		return nil, "", 0, 0, fmt.Errorf("decode image: %w", err)
	}
	cropped := centerCropSquare(src)
	dst := image.NewRGBA(image.Rect(0, 0, PortraitDim, PortraitDim))
	draw.CatmullRom.Scale(dst, dst.Bounds(), cropped, cropped.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", 0, 0, fmt.Errorf("encode portrait: %w", err)
	}
	return buf.Bytes(), "image/jpeg", PortraitDim, PortraitDim, nil
}

// centerCropSquare returns the largest centered square sub-image of src.
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
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Copy(dst, image.Point{}, src, image.Rect(x0, y0, x0+side, y0+side), draw.Src, nil)
	return dst
}
