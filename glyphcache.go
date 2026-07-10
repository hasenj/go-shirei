package shirei

import (
	"container/list"
	"image"
	"math"

	"github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/vector"
)

// Shared, backend-agnostic glyph bitmap cache. A glyph is rasterized to an alpha
// coverage bitmap once (on first sight at a given device-pixel size) and cached;
// backends blit the cached bitmap (tinted with the text color) instead of
// re-deriving the outline / re-filling a path every frame.
//
// Design + constraints: cocoabackend/GLYPH_CACHE_PLAN.md. In short:
//   - core never calls a backend callback; it surfaces what changed this frame as
//     data (GlyphsAdded / GlyphsEvicted in FrameOutputData), keyed by GlyphKey.
//   - the work is gated on GlyphCacheBudgetBytes > 0, so backends that don't use
//     it (e.g. giobackend, which has its own path cache) pay nothing.

// GlyphCacheBudgetBytes is the soft cap on total cached glyph-bitmap bytes. 0
// disables the cache entirely (no rasterization, no delta lists). Set by the
// backend that consumes the cache.
var GlyphCacheBudgetBytes int

// GlyphKey identifies a cached glyph bitmap. Px is the glyph box height in *device*
// pixels (round(Rect.Size[1] * WindowScale)), which subsumes the backing scale: a
// 16pt glyph at 2x and a 32pt glyph at 1x share one bitmap (same physical pixels).
type GlyphKey struct {
	FontId  FontId
	GlyphId GlyphId
	Px      uint16
}

// GlyphBM is a rasterized glyph: an alpha coverage bitmap plus the placement
// metrics needed to position it relative to the pen origin. All geometry is in
// device pixels (scale-independent), so an entry is valid regardless of the
// WindowScale in effect when it is drawn; the backend divides by the current
// WindowScale to get logical coordinates.
type GlyphBM struct {
	W, H   int     // device-px bitmap dimensions (0 for an empty glyph, e.g. space)
	OffX   float32 // device-px offset from pen origin to bitmap top-left (x rightward)
	OffY   float32 // device-px offset from pen origin to bitmap top-left (y downward)
	Alpha  []byte  // coverage, one byte per pixel, len == Stride*H
	Stride int
}

// GlyphKeyForSurface derives the cache key for a glyph surface. The SINGLE source
// of truth for quantization, used by both core's cache pass and the backend's draw
// path so the two can never disagree. ok is false for non-glyph surfaces.
func GlyphKeyForSurface(s *Surface) (GlyphKey, bool) {
	if s.FontId == 0 || s.GlyphId == 0 {
		return GlyphKey{}, false
	}
	px := int(s.Rect.Size[1]*WindowScale + 0.5)
	if px < 1 || px > 65535 {
		return GlyphKey{}, false
	}
	return GlyphKey{FontId: s.FontId, GlyphId: s.GlyphId, Px: uint16(px)}, true
}

// --- the LRU (map + intrusive list, O(1) touch, evict from tail) ---------------
//
// A purpose-built tiny LRU rather than the vendored dboslee/lru because we must
// surface evictions *as data* (no callbacks), and we must NOT do a full-map scan
// per frame (that is exactly Gio's textureCache.frame() CPU sink we're avoiding).

type glyphCacheEntry struct {
	key      GlyphKey
	bm       GlyphBM
	lastUsed int64 // FrameNumber; never evict an entry used this frame
}

var (
	glyphMap   = make(map[GlyphKey]*list.Element)
	glyphList  = list.New() // front = most recently used
	glyphBytes int

	// reused per-frame delta buffers (assigned into FrameOutputData; the backend
	// consumes them before the next frame, so no copy is needed)
	glyphsAddedBuf   []GlyphKey
	glyphsEvictedBuf []GlyphKey
)

// updateGlyphCache walks the frame's surfaces, ensures every used glyph is cached
// (rasterize-on-miss), evicts down to budget, and returns this frame's deltas.
// Called from RunFrameFn under the frame mutex when the cache is enabled.
func updateGlyphCache(surfaces []Surface) (added, evicted []GlyphKey) {
	glyphsAddedBuf = glyphsAddedBuf[:0]
	glyphsEvictedBuf = glyphsEvictedBuf[:0]

	for i := range surfaces {
		key, ok := GlyphKeyForSurface(&surfaces[i])
		if !ok {
			continue
		}
		if elem, ok := glyphMap[key]; ok {
			// hit: mark most-recently-used
			glyphList.MoveToFront(elem)
			elem.Value.(*glyphCacheEntry).lastUsed = FrameNumber
			continue
		}
		// miss: rasterize and insert at the front
		bm := rasterizeGlyph(key)
		e := &glyphCacheEntry{key: key, bm: bm, lastUsed: FrameNumber}
		glyphMap[key] = glyphList.PushFront(e)
		glyphBytes += len(bm.Alpha)
		glyphsAddedBuf = append(glyphsAddedBuf, key)
	}

	// evict least-recently-used until under budget, but never evict an entry used
	// this frame (the backend needs its handle to draw this frame).
	for glyphBytes > GlyphCacheBudgetBytes && glyphList.Len() > 0 {
		back := glyphList.Back()
		e := back.Value.(*glyphCacheEntry)
		if e.lastUsed == FrameNumber {
			break
		}
		glyphList.Remove(back)
		delete(glyphMap, e.key)
		glyphBytes -= len(e.bm.Alpha)
		glyphsEvictedBuf = append(glyphsEvictedBuf, e.key)
	}

	return glyphsAddedBuf, glyphsEvictedBuf
}

// GlyphBitmap returns the cached bitmap for a key (false if not currently cached).
// The backend calls this for keys in FrameOutputData.GlyphsAdded to fetch the bytes
// it needs to build its platform handle.
func GlyphBitmap(key GlyphKey) (GlyphBM, bool) {
	elem, ok := glyphMap[key]
	if !ok {
		return GlyphBM{}, false
	}
	return elem.Value.(*glyphCacheEntry).bm, true
}

// rasterizeGlyph renders a glyph's outline into a tight alpha coverage bitmap at
// the key's device-pixel size, via the pure-Go x/image/vector rasterizer.
func rasterizeGlyph(key GlyphKey) GlyphBM {
	outline := GlyphOutline(key.FontId, key.GlyphId)
	if len(outline.Segments) == 0 {
		return GlyphBM{} // empty glyph (e.g. whitespace)
	}

	face := GetFace(key.FontId)
	dscale := float32(key.Px) * face.InvUPM // font units -> device px

	// ink bbox in device px, Y-up (conservatively includes control points)
	minX, minY := float32(math.Inf(1)), float32(math.Inf(1))
	maxX, maxY := float32(math.Inf(-1)), float32(math.Inf(-1))
	acc := func(p opentype.SegmentPoint) {
		x, y := p.X*dscale, p.Y*dscale
		minX, maxX = min(minX, x), max(maxX, x)
		minY, maxY = min(minY, y), max(maxY, y)
	}
	for i := range outline.Segments {
		seg := &outline.Segments[i]
		switch seg.Op {
		case opentype.SegmentOpMoveTo, opentype.SegmentOpLineTo:
			acc(seg.Args[0])
		case opentype.SegmentOpQuadTo:
			acc(seg.Args[0])
			acc(seg.Args[1])
		case opentype.SegmentOpCubeTo:
			acc(seg.Args[0])
			acc(seg.Args[1])
			acc(seg.Args[2])
		}
	}
	if !(maxX > minX) || !(maxY > minY) {
		return GlyphBM{}
	}

	// 1px pad each side so anti-aliasing isn't clipped
	left := float32(math.Floor(float64(minX))) - 1
	right := float32(math.Ceil(float64(maxX))) + 1
	top := float32(math.Ceil(float64(maxY))) + 1
	bottom := float32(math.Floor(float64(minY))) - 1
	w := int(right - left)
	h := int(top - bottom)
	if w <= 0 || h <= 0 {
		return GlyphBM{}
	}

	// map glyph device-px (Y-up, pen origin) -> image space (Y-down, top-left)
	tx := func(p opentype.SegmentPoint) (float32, float32) {
		return p.X*dscale - left, top - p.Y*dscale
	}

	r := vector.NewRasterizer(w, h)
	started := false
	for i := range outline.Segments {
		seg := &outline.Segments[i]
		switch seg.Op {
		case opentype.SegmentOpMoveTo:
			if started {
				r.ClosePath()
			}
			x, y := tx(seg.Args[0])
			r.MoveTo(x, y)
			started = true
		case opentype.SegmentOpLineTo:
			x, y := tx(seg.Args[0])
			r.LineTo(x, y)
		case opentype.SegmentOpQuadTo:
			cx, cy := tx(seg.Args[0])
			x, y := tx(seg.Args[1])
			r.QuadTo(cx, cy, x, y)
		case opentype.SegmentOpCubeTo:
			c1x, c1y := tx(seg.Args[0])
			c2x, c2y := tx(seg.Args[1])
			x, y := tx(seg.Args[2])
			r.CubeTo(c1x, c1y, c2x, c2y, x, y)
		}
	}
	if started {
		r.ClosePath()
	}

	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	r.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})

	return GlyphBM{
		W:      w,
		H:      h,
		OffX:   left,
		OffY:   -top,
		Alpha:  dst.Pix,
		Stride: dst.Stride,
	}
}
