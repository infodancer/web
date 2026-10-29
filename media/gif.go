package media

import (
	"errors"
	"fmt"
)

// GIF block introducers (GIF89a spec, sections 15-27).
const (
	gifExtension       = 0x21
	gifImageDescriptor = 0x2C
	gifTrailer         = 0x3B

	gifExtPlainText      = 0x01
	gifExtGraphicControl = 0xF9
	gifExtComment        = 0xFE
	gifExtApplication    = 0xFF
)

var errGIFTruncated = errors.New("media: gif: truncated")

// gifFrameCount reports how many frames a GIF holds, stopping at two: callers
// only need to tell a static image from an animated one. It walks the block
// structure and skips every data sub-block without decompressing it, so the cost
// is linear in the input size and allocation-free. gif.DecodeAll would answer the
// same question by fully decoding every frame, which lets a small upload of many
// highly compressible frames demand gigabytes.
func gifFrameCount(data []byte) (int, error) {
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return 0, errors.New("media: gif: bad header")
	}
	// Logical screen descriptor: width(2) height(2) packed(1) bg(1) aspect(1).
	pos := 13
	if packed := data[10]; packed&0x80 != 0 {
		pos += colorTableSize(packed)
	}

	frames := 0
	for {
		if pos >= len(data) {
			return 0, errGIFTruncated
		}
		block := data[pos]
		pos++
		switch block {
		case gifTrailer:
			if frames == 0 {
				return 0, errors.New("media: gif: no image data")
			}
			return frames, nil

		case gifExtension:
			var err error
			if pos, err = skipExtension(data, pos); err != nil {
				return 0, err
			}

		case gifImageDescriptor:
			frames++
			if frames >= 2 {
				return frames, nil
			}
			// x(2) y(2) w(2) h(2) packed(1).
			if pos+9 > len(data) {
				return 0, errGIFTruncated
			}
			packed := data[pos+8]
			pos += 9
			if packed&0x80 != 0 {
				pos += colorTableSize(packed)
			}
			pos++ // LZW minimum code size
			var err error
			if pos, err = skipSubBlocks(data, pos); err != nil {
				return 0, err
			}

		default:
			return 0, fmt.Errorf("media: gif: unknown block 0x%02x", block)
		}
	}
}

// skipExtension advances past an extension block whose label is at pos. Each
// label is framed the way image/gif reads it, so the walker and the decoder agree
// on where the next block starts: plain text and application extensions open
// with a fixed header block that is consumed even when its length byte is zero,
// the graphic control extension is a fixed 6 bytes, and unknown labels are
// rejected.
func skipExtension(data []byte, pos int) (int, error) {
	if pos >= len(data) {
		return 0, errGIFTruncated
	}
	label := data[pos]
	pos++
	switch label {
	case gifExtGraphicControl:
		// size(1)=4, flags(1), delay(2), transparent index(1), terminator(1)=0.
		if pos+6 > len(data) {
			return 0, errGIFTruncated
		}
		if data[pos] != 4 || data[pos+5] != 0 {
			return 0, errors.New("media: gif: bad graphic control extension")
		}
		return pos + 6, nil
	case gifExtPlainText:
		// image/gif reads a fixed 13-byte header (size byte plus 12) whatever
		// the size byte says.
		pos += 13
	case gifExtApplication:
		if pos >= len(data) {
			return 0, errGIFTruncated
		}
		pos += 1 + int(data[pos])
	case gifExtComment:
		// Only sub-blocks.
	default:
		return 0, fmt.Errorf("media: gif: unknown extension 0x%02x", label)
	}
	return skipSubBlocks(data, pos)
}

// colorTableSize returns the byte length of the color table a packed field
// declares: 3 bytes per entry, 2^(n+1) entries.
func colorTableSize(packed byte) int {
	return 3 << ((packed & 0x07) + 1)
}

// skipSubBlocks advances past a run of data sub-blocks (length byte, then that
// many bytes) ending in a zero-length terminator, returning the position after it.
func skipSubBlocks(data []byte, pos int) (int, error) {
	for {
		if pos >= len(data) {
			return 0, errGIFTruncated
		}
		n := int(data[pos])
		pos++
		if n == 0 {
			return pos, nil
		}
		pos += n
	}
}
