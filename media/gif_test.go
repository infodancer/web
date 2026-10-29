package media

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

// localPaletteGIFBytes returns a two-frame GIF whose frames carry local color
// tables, so the walker must skip an LCT after each image descriptor.
func localPaletteGIFBytes(t *testing.T) []byte {
	t.Helper()
	pal1 := color.Palette{color.Black, color.White}
	pal2 := color.Palette{color.RGBA{R: 0xff, A: 0xff}, color.RGBA{G: 0xff, A: 0xff}, color.RGBA{B: 0xff, A: 0xff}}
	g := &gif.GIF{
		Image: []*image.Paletted{
			image.NewPaletted(image.Rect(0, 0, 4, 4), pal1),
			image.NewPaletted(image.Rect(0, 0, 4, 4), pal2),
		},
		Delay: []int{10, 10},
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatalf("encode local-palette gif: %v", err)
	}
	return b.Bytes()
}

func TestGIFFrameCount(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want int
	}{
		{"static", staticGIFBytes(t, 8, 8), 1},
		{"animated", animatedGIFBytes(t), 2},
		{"local palettes", localPaletteGIFBytes(t), 2},
		{"many frames stops at two", manyFrameGIFBytes(t, 8, 8, 50), 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := gifFrameCount(tc.data)
			if err != nil {
				t.Fatalf("gifFrameCount: %v", err)
			}
			if got != tc.want {
				t.Fatalf("frames = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGIFFrameCount_Malformed(t *testing.T) {
	static := staticGIFBytes(t, 8, 8)
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"not a gif", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")},
		{"header only", []byte("GIF89a")},
		{"truncated", static[:len(static)/2]},
		{"no trailer", static[:len(static)-1]},
		{"unknown block", append(append([]byte{}, static[:len(static)-1]...), 0x99)},
		{"no frames", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00\x3b")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := gifFrameCount(tc.data); err == nil {
				t.Fatal("malformed GIF accepted, want error")
			}
		})
	}
}

// The walker must agree with the standard decoder: whenever gif.DecodeAll
// accepts the input, the walker reports the same frame count (capped at 2).
func FuzzGIFFrameCount(f *testing.F) {
	f.Add(staticGIFBytes(&testing.T{}, 4, 4))
	f.Add(animatedGIFBytes(&testing.T{}))
	f.Add(localPaletteGIFBytes(&testing.T{}))
	f.Add([]byte("GIF89a"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := gifFrameCount(data)
		g, derr := gif.DecodeAll(bytes.NewReader(data))
		if derr != nil {
			return
		}
		want := min(len(g.Image), 2)
		if err != nil {
			t.Fatalf("walker rejected input the decoder accepts: %v", err)
		}
		if got != want {
			t.Fatalf("walker frames = %d, decoder frames = %d", got, want)
		}
	})
}
