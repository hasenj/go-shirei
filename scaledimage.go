package shirei

import (
	"image"
	"unsafe"

	xdraw "golang.org/x/image/draw"
)

// Scaled-image cache. Container images (and Retina-upscaled shadows) are resampled
// to a device-pixel size that is usually the same every frame, but x/image/draw
// rebuilds its scaler — and we allocate a destination buffer — on every Scale call.
// Caching the scaled result by (image id, device size) avoids both the per-frame
// allocation and the resample work for the common static-image case.
//
// Single-threaded use (like the glyph and corner caches). The entry is invalidated
// when the source pixels change — a reload or a late background decode replaces the
// ImageData's Pix, so the base address / length no longer match.

const scaledCacheCap = 256

type scaledKey struct {
	id     ImageId
	dw, dh int
}

type scaledEntry struct {
	img     *image.RGBA
	srcBase uintptr // &src.Pix[0] when scaled — changes if the image is replaced
	srcLen  int
}

var scaledImageCache = map[scaledKey]*scaledEntry{}

// scaledImage returns src resampled to dw×dh (BiLinear), cached by (id, size).
func scaledImage(id ImageId, src *image.RGBA, dw, dh int) *image.RGBA {
	if len(src.Pix) == 0 {
		return src
	}
	base := uintptr(unsafe.Pointer(&src.Pix[0]))
	key := scaledKey{id, dw, dh}
	if e, ok := scaledImageCache[key]; ok && e.srcBase == base && e.srcLen == len(src.Pix) {
		return e.img
	}
	if len(scaledImageCache) >= scaledCacheCap {
		scaledImageCache = map[scaledKey]*scaledEntry{}
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	scaledImageCache[key] = &scaledEntry{img: dst, srcBase: base, srcLen: len(src.Pix)}
	return dst
}
