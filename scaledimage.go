package shirei

import (
	"image"
	"time"
	"unsafe"

	"github.com/anthonynsimon/bild/transform"
)

// Scaled-image cache. Container images (and Retina-upscaled shadows) are resampled
// to a device-pixel size that is usually the same every frame. Caching the scaled
// result by (image id, device size, pixel order, quality) avoids both the
// per-frame allocation and the resample work for the common static-image case.
//
// All resampling goes through bild/transform.Resize. While the requested size is
// still moving (continuous resize / splitter drag), we use ScaleMotionFilter
// (default: Nearest) and optionally quantize the cache size so neighboring
// frames share work; once the size is idle we upgrade to ScaleIdleFilter
// (default: Linear) at the exact size. A motion-quality paint bumps
// ImageData.Generation and RequestNextFrame so present-skip (which folds
// Generation into SurfacesHash) cannot keep the motion derivative after idle.
//
// Entries are stored in Host.PixelOrder so blitPremul can copy channels 1:1
// without a per-pixel swizzle.
//
// Single-threaded use (like the glyph and corner caches). The entry is invalidated
// when the source pixels change — a reload or a late background decode replaces the
// ImageData's Pix, so the base address / length no longer match.

const scaledCacheCap = 256

// Tunables for interactive resize. Flip these while profiling:
//
//	ScaleMotionIdle = 0            → always ScaleIdleFilter
//	ScaleMotionQuantize = 0        → no size rounding during motion
//	ScaleMotionQuantize = 8        → round dw/dh to 8px while moving
var (
	// ScaleMotionIdle is how long the requested device size must stay unchanged
	// before ScaleIdleFilter is used. While size is moving (or just moved),
	// ScaleMotionFilter is used instead.
	ScaleMotionIdle = 120 * time.Millisecond

	// ScaleMotionQuantize rounds dw/dh to this step during motion (0 = off).
	// Idle frames always use the exact size.
	ScaleMotionQuantize = 0

	// ScaleIdleFilter is the resampler when size is stable (exact dw/dh).
	ScaleIdleFilter = transform.Linear

	// ScaleMotionFilter is the resampler while size is moving.
	ScaleMotionFilter = transform.NearestNeighbor
)

// scaledKey distinguishes cache entries. ResampleFilter itself is not comparable
// (holds a func), so we key on Support + whether Fn is nil (NearestNeighbor).
type scaledKey struct {
	id      ImageId
	dw, dh  int
	order   [4]uint8
	support float64
	fnNil   bool
}

type scaledEntry struct {
	img     *image.RGBA
	opaque  bool
	srcBase uintptr // &src.Pix[0] when scaled — changes if the image is replaced
	srcLen  int
}

type imageOpacity struct {
	srcBase uintptr
	srcLen  int
	rect    image.Rectangle
	stride  int
	opaque  bool
}

type scaledResult struct {
	img    *image.RGBA
	opaque bool
}

type scaleMotion struct {
	dw, dh int
	at     time.Time
}

// dropScaledForImage removes cached resamples for a reclaimed ImageId.
func dropScaledForImage(id ImageId) {
	for k := range res.scaledImageCache {
		if k.id == id {
			delete(res.scaledImageCache, k)
		}
	}
	delete(res.scaleMotionById, id)
	delete(res.imageOpacityById, id)
}

func quantizeDim(v, step int) int {
	if step <= 1 || v <= 0 {
		return v
	}
	q := ((v + step/2) / step) * step
	if q < 1 {
		q = 1
	}
	return q
}

// noteScaleMotion updates per-id size history and reports whether we are still
// inside the motion (cheap-scale) window. First sighting of an id is not motion
// — only a change away from a previously seen size starts the idle timer.
func noteScaleMotion(id ImageId, dw, dh int) bool {
	if ScaleMotionIdle <= 0 {
		return false
	}
	if ui != nil && ui.Host.HeadlessRender {
		// Snapshots / PNG must be deterministic quality.
		return false
	}
	now := time.Now()
	m, ok := res.scaleMotionById[id]
	if !ok {
		// Stable baseline; backdate so the idle check passes immediately.
		res.scaleMotionById[id] = scaleMotion{dw: dw, dh: dh, at: now.Add(-ScaleMotionIdle)}
		return false
	}
	if m.dw != dw || m.dh != dh {
		res.scaleMotionById[id] = scaleMotion{dw: dw, dh: dh, at: now}
		return true
	}
	return now.Sub(m.at) < ScaleMotionIdle
}

func sourceImageOpaque(id ImageId, src *image.RGBA, base uintptr) bool {
	if op, ok := res.imageOpacityById[id]; ok &&
		op.srcBase == base && op.srcLen == len(src.Pix) &&
		op.rect == src.Rect && op.stride == src.Stride {
		return op.opaque
	}
	opaque := src.Opaque()
	res.imageOpacityById[id] = imageOpacity{
		srcBase: base,
		srcLen:  len(src.Pix),
		rect:    src.Rect,
		stride:  src.Stride,
		opaque:  opaque,
	}
	return opaque
}

// scaledImage returns src resampled to dw×dh and converted into the active
// Host.PixelOrder, cached by (id, size, order, quality).
func scaledImage(id ImageId, src *image.RGBA, dw, dh int) scaledResult {
	t0 := time.Now()
	defer func() {
		if ui != nil {
			ui.Host.ImageScaleTime += time.Since(t0)
		}
	}()

	if len(src.Pix) == 0 {
		return scaledResult{img: src}
	}
	base := uintptr(unsafe.Pointer(&src.Pix[0]))
	order := pixelOrder()
	opaque := sourceImageOpaque(id, src, base)

	moving := noteScaleMotion(id, dw, dh)
	scaleDw, scaleDh := dw, dh
	if moving && ScaleMotionQuantize > 0 {
		scaleDw = quantizeDim(dw, ScaleMotionQuantize)
		scaleDh = quantizeDim(dh, ScaleMotionQuantize)
	}

	filter := ScaleIdleFilter
	if moving {
		filter = ScaleMotionFilter
	}
	key := scaledKey{id, scaleDw, scaleDh, order, filter.Support, filter.Fn == nil}
	if e, ok := res.scaledImageCache[key]; ok && e.srcBase == base && e.srcLen == len(src.Pix) {
		if moving {
			bumpImageGeneration(id)
			RequestNextFrame()
		}
		if scaleDw == dw && scaleDh == dh {
			return scaledResult{img: e.img, opaque: e.opaque}
		}
		return scaledResult{img: nearestStretchRGBA(e.img, dw, dh), opaque: e.opaque}
	}
	if len(res.scaledImageCache) >= scaledCacheCap {
		res.scaledImageCache = map[scaledKey]*scaledEntry{}
	}

	var rgba *image.RGBA
	if scaleDw == src.Bounds().Dx() && scaleDh == src.Bounds().Dy() {
		rgba = src
	} else {
		rgba = transform.Resize(src, scaleDw, scaleDh, filter)
	}

	out := applyPixelOrderRGBA(rgba, order)
	res.scaledImageCache[key] = &scaledEntry{
		img: out, opaque: opaque, srcBase: base, srcLen: len(src.Pix),
	}

	if moving {
		// Same Surface list next frame would present-skip; bump Generation so
		// SurfacesHash moves and the idle quality pass actually paints.
		bumpImageGeneration(id)
		RequestNextFrame()
	}

	if scaleDw == dw && scaleDh == dh {
		return scaledResult{img: out, opaque: opaque}
	}
	return scaledResult{img: nearestStretchRGBA(out, dw, dh), opaque: opaque}
}

func bumpImageGeneration(id ImageId) {
	if img := LookupImage(id); img != nil {
		img.Generation = nextImageGeneration()
	}
}

func nearestStretchRGBA(src *image.RGBA, dw, dh int) *image.RGBA {
	if src.Bounds().Dx() == dw && src.Bounds().Dy() == dh {
		return src
	}
	return transform.Resize(src, dw, dh, transform.NearestNeighbor)
}

// applyPixelOrderRGBA returns src with each pixel rewritten so dest slot k holds
// source channel order[k] of (R,G,B,A). When order is RGBA, reuses src if it is
// already a private buffer; otherwise allocates a copy.
func applyPixelOrderRGBA(src *image.RGBA, order [4]uint8) *image.RGBA {
	if order == PixelOrderRGBA {
		// Caller may still share the original image pixels — always copy so the
		// cache owns the buffer and blitPremul never sees a live shared Pix.
		dst := image.NewRGBA(src.Bounds())
		copy(dst.Pix, src.Pix)
		return dst
	}
	b := src.Bounds()
	dst := image.NewRGBA(b)
	sp, dp := src.Pix, dst.Pix
	n := len(sp)
	for i := 0; i < n; i += 4 {
		ch := [4]byte{sp[i], sp[i+1], sp[i+2], sp[i+3]}
		dp[i+0] = ch[order[0]&3]
		dp[i+1] = ch[order[1]&3]
		dp[i+2] = ch[order[2]&3]
		dp[i+3] = ch[order[3]&3]
	}
	return dst
}
