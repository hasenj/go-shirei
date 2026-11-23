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
	cheap  bool // last resample used the motion filter
}

type scalePhase int

const (
	scaleIdle scalePhase = iota
	scaleChanged
	scaleWaiting
)

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

// resolveScaleSize picks the device size to resample to and whether this
// paint is a live resize, the post-resize wait for idle quality, or idle.
//
// A 1px flip-flop (subpixel layout of a 16px icon) is not motion — it is
// locked to the last committed size. Treating it as motion used to bump
// ImageData.Generation and RequestNextFrame on every paint, which kept
// SurfacesHash changing and the display link at 60fps.
func resolveScaleSize(id ImageId, dw, dh int) (outW, outH int, phase scalePhase) {
	if ScaleMotionIdle <= 0 || (ui != nil && ui.Host.HeadlessRender) {
		return dw, dh, scaleIdle
	}
	now := time.Now()
	m, ok := res.scaleMotionById[id]
	if !ok {
		res.scaleMotionById[id] = scaleMotion{dw: dw, dh: dh, at: now.Add(-ScaleMotionIdle)}
		return dw, dh, scaleIdle
	}
	if absInt(m.dw-dw) <= 1 && absInt(m.dh-dh) <= 1 {
		if m.cheap && now.Sub(m.at) < ScaleMotionIdle {
			return m.dw, m.dh, scaleWaiting
		}
		return m.dw, m.dh, scaleIdle
	}
	res.scaleMotionById[id] = scaleMotion{dw: dw, dh: dh, at: now, cheap: true}
	return dw, dh, scaleChanged
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sourceImageOpaque(id ImageId, src *image.RGBA, base uintptr) bool {
	if op, ok := res.imageOpacityById[id]; ok &&
		op.srcBase == base && op.srcLen == len(src.Pix) &&
		op.rect == src.Rect && op.stride == src.Stride {
		return op.opaque
	}
	opaque := rgbaOpaque(src)
	res.imageOpacityById[id] = imageOpacity{
		srcBase: base,
		srcLen:  len(src.Pix),
		rect:    src.Rect,
		stride:  src.Stride,
		opaque:  opaque,
	}
	return opaque
}

func rgbaOpaque(src *image.RGBA) bool {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return true
	}
	rowBytes := w * 4
	base := src.PixOffset(b.Min.X, b.Min.Y)
	pix := src.Pix
	if src.Stride == rowBytes && base == 0 {
		end := h * rowBytes
		for i := 3; i < end; i += 4 {
			if pix[i] != 0xff {
				return false
			}
		}
		return true
	}
	for y := 0; y < h; y++ {
		row := pix[base+y*src.Stride : base+y*src.Stride+rowBytes]
		for i := 3; i < rowBytes; i += 4 {
			if row[i] != 0xff {
				return false
			}
		}
	}
	return true
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
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()

	// Pre-scaled bitmaps (icons at logical×WindowScale) hit here: pixel-order
	// cache only. A 1px dest jitter is still 1:1 — not a resize, not motion.
	if absInt(dw-sw) <= 1 && absInt(dh-sh) <= 1 {
		return orderedImage(id, src, sw, sh, base, order, opaque)
	}

	// This dest size already has an idle-quality entry. Painting the same
	// image at another stable size must not look like a live resize.
	idleKey := scaledKey{id, dw, dh, order, ScaleIdleFilter.Support, ScaleIdleFilter.Fn == nil}
	if e, ok := res.scaledImageCache[idleKey]; ok && e.srcBase == base && e.srcLen == len(src.Pix) {
		return scaledResult{img: e.img, opaque: e.opaque}
	}

	scaleDw, scaleDh, phase := resolveScaleSize(id, dw, dh)
	if phase == scaleChanged && ScaleMotionQuantize > 0 {
		scaleDw = quantizeDim(scaleDw, ScaleMotionQuantize)
		scaleDh = quantizeDim(scaleDh, ScaleMotionQuantize)
	}

	moving := phase == scaleChanged || phase == scaleWaiting
	filter := ScaleIdleFilter
	if moving {
		filter = ScaleMotionFilter
	}
	key := scaledKey{id, scaleDw, scaleDh, order, filter.Support, filter.Fn == nil}
	if e, ok := res.scaledImageCache[key]; ok && e.srcBase == base && e.srcLen == len(src.Pix) {
		if phase == scaleWaiting {
			// Size is locked; tick until the idle-quality upgrade.
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

	var reuse *image.RGBA
	if prev := res.scaledImageCache[key]; prev != nil {
		reuse = prev.img
	}
	out := applyPixelOrderRGBAInto(rgba, order, reuse)
	res.scaledImageCache[key] = &scaledEntry{
		img: out, opaque: opaque, srcBase: base, srcLen: len(src.Pix),
	}

	if phase == scaleIdle {
		// First exact-size linear after a cheap pass: one Generation bump so
		// present-skip cannot keep the nearest-neighbor pixels.
		if m, ok := res.scaleMotionById[id]; ok && m.cheap {
			m.cheap = false
			res.scaleMotionById[id] = m
			bumpImageGeneration(id)
			RequestNextFrame()
		}
	} else if phase == scaleWaiting {
		RequestNextFrame()
	}

	if scaleDw == dw && scaleDh == dh {
		return scaledResult{img: out, opaque: opaque}
	}
	return scaledResult{img: nearestStretchRGBA(out, dw, dh), opaque: opaque}
}

// orderedImage is the 1:1 path: source pixels copied into Host.PixelOrder.
func orderedImage(id ImageId, src *image.RGBA, dw, dh int, base uintptr, order [4]uint8, opaque bool) scaledResult {
	key := scaledKey{id, dw, dh, order, 0, true}
	prev, ok := res.scaledImageCache[key]
	if ok && prev.srcBase == base && prev.srcLen == len(src.Pix) {
		return scaledResult{img: prev.img, opaque: prev.opaque}
	}
	if len(res.scaledImageCache) >= scaledCacheCap {
		res.scaledImageCache = map[scaledKey]*scaledEntry{}
		prev = nil
	}
	var reuse *image.RGBA
	if prev != nil {
		reuse = prev.img
	}
	out := applyPixelOrderRGBAInto(src, order, reuse)
	res.scaledImageCache[key] = &scaledEntry{
		img: out, opaque: opaque, srcBase: base, srcLen: len(src.Pix),
	}
	return scaledResult{img: out, opaque: opaque}
}

func bumpImageGeneration(id ImageId) {
	res.imageMu.Lock()
	defer res.imageMu.Unlock()
	if id == 0 || int(id) >= len(res.imageIds) || res.imageIds[id] == nil {
		return
	}
	cp := *res.imageIds[id]
	cp.Generation = nextImageGeneration()
	res.imageIds[id] = &cp
}

func nearestStretchRGBA(src *image.RGBA, dw, dh int) *image.RGBA {
	if src.Bounds().Dx() == dw && src.Bounds().Dy() == dh {
		return src
	}
	return transform.Resize(src, dw, dh, transform.NearestNeighbor)
}

// applyPixelOrderRGBA returns src with each pixel rewritten into order.
func applyPixelOrderRGBA(src *image.RGBA, order [4]uint8) *image.RGBA {
	return applyPixelOrderRGBAInto(src, order, nil)
}

// applyPixelOrderRGBAInto rewrites src so dest slot k holds source channel
// order[k] of (R,G,B,A), reusing dst when its bounds match src.
func applyPixelOrderRGBAInto(src *image.RGBA, order [4]uint8, dst *image.RGBA) *image.RGBA {
	b := src.Bounds()
	if dst == nil || dst.Rect != b {
		dst = image.NewRGBA(b)
	}
	if order == PixelOrderRGBA {
		copy(dst.Pix, src.Pix)
		return dst
	}
	sp, dp := src.Pix, dst.Pix
	n := len(sp)
	if order == PixelOrderBGRA {
		for i := 0; i < n; i += 4 {
			dp[i+0] = sp[i+2]
			dp[i+1] = sp[i+1]
			dp[i+2] = sp[i+0]
			dp[i+3] = sp[i+3]
		}
		return dst
	}
	for i := 0; i < n; i += 4 {
		ch := [4]byte{sp[i], sp[i+1], sp[i+2], sp[i+3]}
		dp[i+0] = ch[order[0]&3]
		dp[i+1] = ch[order[1]&3]
		dp[i+2] = ch[order[2]&3]
		dp[i+3] = ch[order[3]&3]
	}
	return dst
}
