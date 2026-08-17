package shirei

import (
	"bytes"
	"container/list"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"

	"github.com/anthonynsimon/bild/transform"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	_ "golang.org/x/image/tiff"
	"golang.org/x/image/vector"
)

// Shared, backend-agnostic glyph bitmap cache. A glyph is rasterized once (on
// first sight at a given device-pixel size) and cached. Outline glyphs become
// an alpha coverage mask (tinted with the text color at blit); color-bitmap
// glyphs (sbix / CBDT PNG) become a precolored RGBA stamp that is blitted
// without tint.
//
// Design constraints:
//   - core never calls a backend callback; it surfaces what changed this frame as
//     data (GlyphsAdded / GlyphsEvicted in FrameOutputData), keyed by GlyphKey.
//   - the work is gated on Host.GlyphCacheBudgetBytes > 0, so backends that don't
//     use it (e.g. giobackend, which has its own path cache) pay nothing.

// Glyph cache budget lives on Host.GlyphCacheBudgetBytes (set by the backend).
// 0 disables the cache entirely (no rasterization, no delta lists).

// GlyphKey identifies a cached glyph bitmap. Px is the glyph box height in *device*
// pixels (round(Rect.Size[1] * Host.WindowScale)), which subsumes the backing
// scale: a 16pt glyph at 2x and a 32pt glyph at 1x share one bitmap (same
// physical pixels).
type GlyphKey struct {
	FontId  FontId
	GlyphId GlyphId
	Px      uint16
}

// GlyphBM is a rasterized glyph plus the placement metrics needed to position
// it relative to the pen origin. All geometry is in device pixels
// (scale-independent), so an entry is valid regardless of the Host.WindowScale
// in effect when it is drawn; the backend divides by the current WindowScale
// to get logical coordinates.
//
// An outline glyph fills Alpha (one coverage byte per pixel). A color-bitmap
// glyph fills RGBA instead: premultiplied, Host.PixelOrder, 4 bytes per pixel.
// Exactly one of Alpha or RGBA is non-empty for a drawable stamp.
type GlyphBM struct {
	W, H   int     // device-px bitmap dimensions (0 for an empty glyph, e.g. space)
	OffX   float32 // device-px offset from pen origin to bitmap top-left (x rightward)
	OffY   float32 // device-px offset from pen origin to bitmap top-left (y downward)
	Alpha  []byte  // coverage, one byte per pixel, len == Stride*H
	RGBA   []byte  // precolored stamp, 4 bytes/pixel, len == Stride*H
	Stride int
}

// GlyphKeyForSurface derives the cache key for a glyph surface. The SINGLE source
// of truth for quantization, used by both core's cache pass and the backend's draw
// path so the two can never disagree. ok is false for non-glyph surfaces.
func GlyphKeyForSurface(s *Surface) (GlyphKey, bool) {
	if s.FontId == 0 || s.GlyphId == 0 {
		return GlyphKey{}, false
	}
	px := int(s.Rect.Size[1]*ui.Host.WindowScale + 0.5)
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
	res.glyphsAddedBuf = res.glyphsAddedBuf[:0]
	res.glyphsEvictedBuf = res.glyphsEvictedBuf[:0]

	for i := range surfaces {
		key, ok := GlyphKeyForSurface(&surfaces[i])
		if !ok {
			continue
		}
		if elem, ok := res.glyphMap[key]; ok {
			// hit: mark most-recently-used
			res.glyphList.MoveToFront(elem)
			elem.Value.(*glyphCacheEntry).lastUsed = ui.FrameNumber
			continue
		}
		// miss: rasterize and insert at the front
		bm := rasterizeGlyph(key)
		e := &glyphCacheEntry{key: key, bm: bm, lastUsed: ui.FrameNumber}
		res.glyphMap[key] = res.glyphList.PushFront(e)
		res.glyphBytes += glyphBMBytes(bm)
		res.glyphsAddedBuf = append(res.glyphsAddedBuf, key)
	}

	// evict least-recently-used until under budget, but never evict an entry used
	// this frame (the backend needs its handle to draw this frame).
	for res.glyphBytes > ui.Host.GlyphCacheBudgetBytes && res.glyphList.Len() > 0 {
		back := res.glyphList.Back()
		e := back.Value.(*glyphCacheEntry)
		if e.lastUsed == ui.FrameNumber {
			break
		}
		res.glyphList.Remove(back)
		delete(res.glyphMap, e.key)
		res.glyphBytes -= glyphBMBytes(e.bm)
		res.glyphsEvictedBuf = append(res.glyphsEvictedBuf, e.key)
	}

	return res.glyphsAddedBuf, res.glyphsEvictedBuf
}

// GlyphBitmap returns the cached bitmap for a key (false if not currently cached).
// The backend calls this for keys in FrameOutputData.GlyphsAdded to fetch the bytes
// it needs to build its platform handle.
func GlyphBitmap(key GlyphKey) (GlyphBM, bool) {
	elem, ok := res.glyphMap[key]
	if !ok {
		return GlyphBM{}, false
	}
	return elem.Value.(*glyphCacheEntry).bm, true
}

func glyphBMBytes(bm GlyphBM) int {
	return len(bm.Alpha) + len(bm.RGBA)
}

// rasterizeGlyph renders a glyph at the key's device-pixel size. Color-bitmap
// data (sbix / CBDT) becomes an RGBA stamp; otherwise the outline is filled
// into an alpha coverage mask via the pure-Go x/image/vector rasterizer.
func rasterizeGlyph(key GlyphKey) GlyphBM {
	if bm, ok := rasterizeColorBitmap(key); ok {
		return bm
	}
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

// rasterizeColorBitmap decodes a color-bitmap strike (PNG/JPG/TIFF) and scales
// it to the key's em. ok is false when the face has no bitmap for this gid
// (outline and COLR faces) so the caller uses the outline path.
func rasterizeColorBitmap(key GlyphKey) (GlyphBM, bool) {
	ttf := GetParsedFont(key.FontId)
	if ttf == nil {
		return GlyphBM{}, false
	}
	face := GetFace(key.FontId)
	if face.InvUPM <= 0 {
		return GlyphBM{}, false
	}

	// Strike choice is ppem on the shared Face. Restore immediately so
	// shaping extents stay in font units.
	oldX, oldY := ttf.Ppem()
	ttf.SetPpem(key.Px, key.Px)
	data := ttf.GlyphData(key.GlyphId)
	ext, extOK := ttf.GlyphExtents(key.GlyphId)
	ttf.SetPpem(oldX, oldY)

	bm, ok := data.(font.GlyphBitmap)
	if !ok || len(bm.Data) == 0 {
		return GlyphBM{}, false
	}
	if bm.Format != font.PNG && bm.Format != font.JPG && bm.Format != font.TIFF {
		return GlyphBM{}, false
	}

	src, _, err := image.Decode(bytes.NewReader(bm.Data))
	if err != nil || src == nil {
		return GlyphBM{}, false
	}
	rgba := imageToRGBA(src)
	if rgba == nil || rgba.Bounds().Empty() {
		return GlyphBM{}, false
	}

	// Premultiply before scale so bilinear edges keep straight coverage.
	pix := rgba.Pix
	for i := 0; i < len(pix); i += 4 {
		a := uint32(pix[i+3])
		if a == 255 {
			continue
		}
		if a == 0 {
			pix[i], pix[i+1], pix[i+2] = 0, 0, 0
			continue
		}
		pix[i+0] = uint8(uint32(pix[i+0]) * a / 255)
		pix[i+1] = uint8(uint32(pix[i+1]) * a / 255)
		pix[i+2] = uint8(uint32(pix[i+2]) * a / 255)
	}

	dscale := float32(key.Px) * face.InvUPM
	var dw, dh int
	var offX, offY float32
	if extOK && ext.Width != 0 && ext.Height != 0 {
		dw = int(math.Abs(float64(ext.Width*dscale)) + 0.5)
		dh = int(math.Abs(float64(ext.Height*dscale)) + 0.5)
		offX = ext.XBearing * dscale
		offY = -ext.YBearing * dscale
	}
	if dw < 1 || dh < 1 {
		sb := rgba.Bounds()
		dh = int(key.Px)
		if dh < 1 {
			return GlyphBM{}, false
		}
		dw = sb.Dx() * dh / sb.Dy()
		if dw < 1 {
			dw = dh
		}
		offX = 0
		offY = -float32(dh)
	}

	if rgba.Bounds().Dx() != dw || rgba.Bounds().Dy() != dh {
		rgba = transform.Resize(rgba, dw, dh, transform.Linear)
	}
	ordered := applyPixelOrderRGBA(rgba, pixelOrder())
	return GlyphBM{
		W:      dw,
		H:      dh,
		OffX:   offX,
		OffY:   offY,
		RGBA:   ordered.Pix,
		Stride: ordered.Stride,
	}, true
}
