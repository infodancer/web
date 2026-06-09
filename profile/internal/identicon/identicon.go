// Package identicon renders a deterministic SVG avatar from a seed string.
// It is the fallback for profiles without an uploaded picture, so cards and
// forum posts always have an image without leaking anything about the user
// (unlike gravatar's email hashes, the seed is the public handle).
package identicon

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// cells per side of the symmetric grid. Columns 0-2 are derived from the
// hash; columns 3-4 mirror columns 1-0.
const grid = 5

// Generate renders a deterministic SVG identicon for seed. The output is
// self-contained markup built entirely from hash-derived integers -- no part
// of the seed is echoed into the document, so it is safe to serve as
// image/svg+xml regardless of the seed's content.
func Generate(seed string) []byte {
	sum := sha256.Sum256([]byte(seed))

	// Foreground color: hue from the first two bytes, fixed saturation and
	// lightness so every avatar reads well on the neutral background.
	hue := (int(sum[0])<<8 | int(sum[1])) % 360

	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 7 7" width="64" height="64" role="img" aria-label="avatar">`)
	fmt.Fprintf(&b, `<rect width="7" height="7" fill="hsl(%d 25%% 92%%)"/>`, hue)

	// One hash bit per grid cell, mirrored across the vertical axis. Bits
	// 0..14 cover the 3 source columns x 5 rows; sha256 provides plenty.
	bit := 0
	for col := 0; col < (grid+1)/2; col++ {
		for row := 0; row < grid; row++ {
			on := sum[2+bit/8]>>(bit%8)&1 == 1
			bit++
			if !on {
				continue
			}
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="1" height="1" fill="hsl(%d 55%% 45%%)"/>`, col+1, row+1, hue)
			if mirror := grid - 1 - col; mirror != col {
				fmt.Fprintf(&b, `<rect x="%d" y="%d" width="1" height="1" fill="hsl(%d 55%% 45%%)"/>`, mirror+1, row+1, hue)
			}
		}
	}
	b.WriteString(`</svg>`)
	return []byte(b.String())
}
