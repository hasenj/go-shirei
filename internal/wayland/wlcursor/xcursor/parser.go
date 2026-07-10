package xcursor

// Xcursor file parsing. Rewritten from upstream's parser.go when vendored:
// the file stores each pixel as a little-endian ARGB word — byte order
// B,G,R,A — which is exactly wl_shm's ARGB8888 layout, so the raw bytes go
// to the compositor untouched. Upstream R↔B-swapped them through
// hand-written assembly that both crashed on arm64 and produced
// color-swapped cursors (invisible on grayscale arrow themes, which is how
// it survived).

import (
	"encoding/binary"
	"errors"
)

// Image is one cursor image (a single size/frame) from an Xcursor file.
// Pix is the raw pixel data: 32-bit premultiplied ARGB words, little-endian
// (byte order B,G,R,A) — directly usable as wl_shm ARGB8888.
type Image struct {
	Pix      []uint8
	Size     uint32 // nominal size this image serves (e.g. 24, 32, 48)
	Width    uint32
	Height   uint32
	HotspotX uint32
	HotspotY uint32
	Delay    uint32 // animation frame delay in ms
}

const (
	xcurMagic     = "Xcur"
	tocTypeImage  = 0xfffd0002
	imgHeaderSize = 36 // header, type, size, version, w, h, hotx, hoty, delay
)

var ErrNotXcursor = errors.New("xcursor: not an Xcursor file")
var ErrTruncated = errors.New("xcursor: truncated file")

func u32(b []byte, off int) uint32 {
	return binary.LittleEndian.Uint32(b[off : off+4])
}

// ParseXcursor parses an Xcursor file into its image entries (all sizes and
// animation frames; pick with NearestImages).
func ParseXcursor(content []byte) ([]*Image, error) {
	if len(content) < 16 || string(content[:4]) != xcurMagic {
		return nil, ErrNotXcursor
	}
	ntoc := int(u32(content, 12))
	if len(content) < 16+ntoc*12 {
		return nil, ErrTruncated
	}

	imgs := make([]*Image, 0, ntoc)
	for i := 0; i < ntoc; i++ {
		tocOff := 16 + i*12
		if u32(content, tocOff) != tocTypeImage {
			continue
		}
		pos := int(u32(content, tocOff+8))
		img, err := parseImg(content, pos)
		if err != nil {
			return nil, err
		}
		imgs = append(imgs, img)
	}
	return imgs, nil
}

func parseImg(content []byte, pos int) (*Image, error) {
	if pos < 0 || len(content) < pos+imgHeaderSize {
		return nil, ErrTruncated
	}
	b := content[pos:]
	// chunk layout: headerSize, type, subtype(=nominal size), version,
	// width, height, xhot, yhot, delay, pixels...
	img := &Image{
		Size:     u32(b, 8),
		Width:    u32(b, 16),
		Height:   u32(b, 20),
		HotspotX: u32(b, 24),
		HotspotY: u32(b, 28),
		Delay:    u32(b, 32),
	}
	// cap: the spec limit is 32767; this also guards the allocation below
	if img.Width > 0x7fff || img.Height > 0x7fff {
		return nil, errors.New("xcursor: unreasonable image dimensions")
	}
	n := int(4 * img.Width * img.Height)
	if len(b) < imgHeaderSize+n {
		return nil, ErrTruncated
	}
	img.Pix = make([]uint8, n)
	copy(img.Pix, b[imgHeaderSize:imgHeaderSize+n])
	return img, nil
}
