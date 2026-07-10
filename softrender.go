package shirei

import (
	"image"
	"image/color"
)

// Core software renderer: turn a flat []Surface into a single pixel buffer a
// backend can put on screen with no further per-pixel processing. This is the
// shared "smart rendering" layer described in notes/architecture.md ("data is
// cheap; the backend is where rendering gets smart") and notes/software-
// renderer-plan.md: rasterization becomes core's job, so a software backend
// shrinks to a window + a frame driver + input translation + one blit.
//
// It is purely additive and opt-in. Backends with their own rasterizer (cocoa
// via Core Graphics, gio via the GPU) ignore it; the first consumer is the
// headless bitmapbackend, with Windows/Linux shells to follow.
//
// Output format — BGRA8888, 8 bits/channel, premultiplied alpha, top-down rows,
// device-pixel dimensions — is the one format every present path consumes with
// zero conversion (Win32 StretchDIBits, X11 ZPixmap, Wayland wl_shm ARGB8888,
// macOS CGImage little-endian-premultiplied). The window is opaque, so alpha is
// 0xFF everywhere (premultiplied == straight) and PNG only ever appears in tests.
//
// Everything reduces to a single primitive: blend a color (solid or vertical
// gradient) through a coverage mask, clipped by the current clip. Only the
// coverage differs per shape — full for a plain rect, an antialiased mask for a
// rounded shape (x/image/vector), the cached alpha mask for a glyph
// (glyphcache.go), the image's own alpha for an image.

// Framebuffer is a reusable BGRA, premultiplied, top-down pixel buffer at device
// resolution. Pix is a raw []byte (NOT image.RGBA, which is RGBA): we composite
// into BGRA directly so the buffer is presentable without a per-pixel swizzle.
type Framebuffer struct {
	W, H   int    // device pixels
	Stride int    // bytes per row (== W*4)
	Pix    []byte // BGRA, premultiplied, top-down; reused across frames
}

func (fb *Framebuffer) ensure(w, h int) {
	fb.W, fb.H, fb.Stride = w, h, w*4
	need := fb.Stride * h
	if cap(fb.Pix) < need {
		fb.Pix = make([]byte, need)
	} else {
		fb.Pix = fb.Pix[:need]
	}
}

// clearWhite paints the opaque white background the cocoa offscreen oracle uses,
// so a backgroundless scene matches the parity reference. Every byte 0xFF =>
// B=G=R=A=255.
func (fb *Framebuffer) clearWhite() {
	fillByte(fb.Pix, 0xff) // every byte 0xFF == opaque white BGRA
}

// fillByte sets every byte of p to v via span-doubling: seed one byte, then keep
// copying the filled prefix onto the rest. copy is a vectorized memmove, so this
// runs several times faster than a scalar byte loop — the Go compiler only lowers
// the byte loop to a fast memset for the zero value, not arbitrary bytes.
func fillByte(p []byte, v byte) {
	if len(p) == 0 {
		return
	}
	p[0] = v
	for i := 1; i < len(p); i *= 2 {
		copy(p[i:], p[:i])
	}
}

// ToRGBA copies the BGRA buffer into an *image.RGBA (swizzling B<->R). For tests
// only: snapshot/parity comparisons go through PNG, which the runtime never touches.
func (fb *Framebuffer) ToRGBA() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, fb.W, fb.H))
	for y := 0; y < fb.H; y++ {
		src := fb.Pix[y*fb.Stride:]
		dst := img.Pix[y*img.Stride:]
		for x := 0; x < fb.W; x++ {
			o := x * 4
			dst[o] = src[o+2]   // R
			dst[o+1] = src[o+1] // G
			dst[o+2] = src[o]   // B
			dst[o+3] = src[o+3] // A
		}
	}
	return img
}

// clipState is the current clip: a scissor rect plus an optional per-pixel
// coverage mask. The mask is nil for the fast rectangular case (a plain scroll
// container) and non-nil only for rounded clips; when present it is sized to rect
// (stride == rect.Dx()) and holds 0..255 coverage. Drawing always clamps to rect
// first, so a pixel being inside rect is guaranteed before the mask is sampled.
//
// corners.cornerOnly/corners.squares are the key to keeping rounded clips cheap: a
// rounded clip's coverage is 255 everywhere except in its (small) corner squares.
// When cornerOnly is set, a draw that avoids all squares needs no per-pixel mask at
// all — it takes the plain-rect fast path. So the bulk of a rounded-clipped scene
// (scroll content away from the four corners) pays nothing for the clip.
//
// corners lives behind a pointer (nil for a plain-rect clip) so this struct — copied
// by value on every clip push and pop — stays 64 bytes instead of duffcopying the
// 128-byte [4]Rectangle every time. Profiling a text-heavy scroll list showed those
// copies (clipCoverage-by-value in the mask build, and the push/pop saves) at ~36%
// of render time.
type clipState struct {
	rect    image.Rectangle
	mask    []byte
	corners *clipCorners // nil for plain-rect / full-coverage clips
}

// clipCorners records where a coverage clip's mask may be < 255. It is set once when
// the clip is built and never mutated after, so value-copies of a clipState may
// share (alias) the same clipCorners safely.
type clipCorners struct {
	squares    [4]image.Rectangle // device regions where mask may be < 255
	cornerOnly bool               // mask is 255 except within squares
}

// SoftRenderer holds the reusable framebuffer and the clip/transparency stacks.
// One per window/consumer; not safe for concurrent use (neither is a frame).
type SoftRenderer struct {
	fb        Framebuffer
	scale     float32
	clip      clipState
	clipStack []clipState
	alpha     float32 // cumulative transparency-group alpha (1 = opaque)
	alphaPrev []float32

	// devOrigin shifts every device coordinate by an integer pixel amount, so a
	// region can be rasterized into a local buffer whose top-left corresponds to
	// the region's device origin (see regioncache.go). Zero for normal rendering,
	// so it adds only an int subtract on the hot path. Integer-only ⇒ no sub-pixel
	// drift, which is what keeps a cached region's blit pixel-identical to inline.
	devOrigin image.Point

	// scratch for the rounded-rect decomposition (decompose), reused per shape to
	// avoid per-call allocation. Five interior rects + four corners is the maximum.
	interiorBuf [5]image.Rectangle
	cornerBuf   [4]cornerPlace

	// reusable per-clip-depth coverage buffers, so a rounded clip costs no
	// allocation across frames. Only one mask is live per depth at a time (clips
	// nest and pop LIFO; siblings at one depth never overlap in time).
	clipMaskArena [][]byte

	// region cache / measurement (notes/container-cache-plan.md).
	regions regionCache

	// noRegionCache opts THIS renderer out of the region raster cache (which is
	// otherwise always on). Used by the golden test's inline reference and by
	// benchmarks that want to measure raw rasterization.
	noRegionCache bool

	// isolated scratch for renderRegionInto: separate clip/alpha stacks and
	// clip-mask arena so rasterizing a region into its own buffer cannot clobber
	// the in-progress main render's live clip masks. Reused across regions/frames.
	regionClipStack []clipState
	regionAlphaPrev []float32
	regionMaskArena [][]byte

	regionVerifyRaster regionRaster // scratch for SHIREI_REGION_VERIFY re-render
}

// RegionCacheBytes reports the number of cached region bitmaps and their total
// byte size, for the perf printer.
func (r *SoftRenderer) RegionCacheBytes() (entries int, bytes int64) {
	for _, e := range r.regions.entries {
		entries++
		bytes += int64(cap(e.raster.pix))
	}
	return entries, bytes
}

var defaultRenderer SoftRenderer

// RenderToBuffer renders the surfaces into a shared reusable buffer at the device
// size implied by WindowSize * scale. Convenience entry point mirroring the
// frameSurfaces reuse pattern; backends that own their buffer use a SoftRenderer
// directly.
func RenderToBuffer(surfaces []Surface, scale float32) *Framebuffer {
	devW := int(Roundf32(WindowSize[0] * scale))
	devH := int(Roundf32(WindowSize[1] * scale))
	return defaultRenderer.Render(surfaces, devW, devH, scale)
}

// Render rasterizes the surface list into the renderer's own (reused) framebuffer
// at the given device dimensions and scale (device pixels per logical point), and
// returns it.
func (r *SoftRenderer) Render(surfaces []Surface, devW, devH int, scale float32) *Framebuffer {
	r.fb.ensure(devW, devH)
	r.renderSurfaces(surfaces, scale)
	return &r.fb
}

// RenderInto rasterizes into a caller-owned BGRA buffer instead of the renderer's
// own — e.g. an IOSurface / DIB section / shm region that the backend presents
// zero-copy. dst must be at least stride*devH bytes; stride is bytes per row and
// may exceed devW*4 (row padding/alignment is honored).
func (r *SoftRenderer) RenderInto(dst []byte, stride, devW, devH int, scale float32, surfaces []Surface) {
	r.fb.W, r.fb.H, r.fb.Stride, r.fb.Pix = devW, devH, stride, dst
	r.renderSurfaces(surfaces, scale)
}

// RegionStats returns and resets the measure-only region-cache counters (see
// notes/container-cache-plan.md). A backend's perf printer reads it once a second.
func (r *SoftRenderer) RegionStats() RegionStats { return r.regions.fetchStats() }

func (r *SoftRenderer) renderSurfaces(surfaces []Surface, scale float32) {
	r.scale = scale
	r.devOrigin = image.Point{} // the main render is always at the buffer origin
	// The white canvas is only needed where nothing paints over it. If the backmost
	// surface is an opaque fill covering the whole viewport (a typical app/root
	// background), it overwrites every pixel anyway, so skip the clear.
	if len(surfaces) == 0 || !r.coversViewportOpaque(&surfaces[0]) {
		r.fb.clearWhite()
	}
	r.clip = clipState{rect: image.Rect(0, 0, r.fb.W, r.fb.H)}
	r.clipStack = r.clipStack[:0]
	r.alpha = 1
	r.alphaPrev = r.alphaPrev[:0]

	if !r.noRegionCache {
		r.renderCached(surfaces)
		return
	}
	for i := range surfaces {
		r.renderOne(&surfaces[i])
	}
}

// renderOne applies one surface's transparency/clip stack ops and draws its
// content, against the renderer's current clip and group alpha. Split out of the
// render loop so the region cache can replay a sub-range of surfaces into a local
// buffer with the exact same semantics (regioncache.go).
func (r *SoftRenderer) renderOne(s *Surface) {
	if s.Transparency > 0 {
		r.alphaPrev = append(r.alphaPrev, r.alpha)
		r.alpha *= 1 - s.Transparency
	}
	// A clip constrains the surfaces BETWEEN the push and the pop, not
	// the carriers themselves: the pushing surface is the container's
	// own background (already shaped by its Corners) and the popping
	// surface is its border, whose stroke sits ON the rounded boundary
	// — clipping it to its own corner-coverage mask would eat it.
	// Hence: pop before drawing, push after drawing.
	if s.Clip == ClipPop {
		if len(r.clipStack) == 0 {
			panic("softrender: uneven push/pop clip stack")
		}
		r.clip = r.clipStack[len(r.clipStack)-1]
		r.clipStack = r.clipStack[:len(r.clipStack)-1]
	}

	// Skip surfaces that paint nothing (transparent per-container backgrounds)
	// or fall entirely outside the buffer (scrolled away) — but only the
	// content draw; the clip/transparency stack ops above and below still run.
	// This is the per-frame work reduction the cocoa backend does, centralized
	// here so every software backend inherits it (architecture.md).
	if r.visible(s) {
		r.drawContent(s)
	}

	if s.Clip == ClipPush {
		r.pushClip(s)
	}
	if s.PopTransparency {
		if len(r.alphaPrev) == 0 {
			panic("softrender: uneven push/pop transparency stack")
		}
		r.alpha = r.alphaPrev[len(r.alphaPrev)-1]
		r.alphaPrev = r.alphaPrev[:len(r.alphaPrev)-1]
	}
}

// coversViewportOpaque reports whether s is a plain, fully opaque fill (solid or
// opaque gradient) with square corners that covers the entire device viewport — so
// painting it overwrites every pixel and the white clear can be skipped. Called
// only for the backmost surface, where no clip or group transparency is in effect.
func (r *SoftRenderer) coversViewportOpaque(s *Surface) bool {
	if s.Stroke != 0 || s.ImageId != 0 || s.GlyphId != 0 || s.Transparency != 0 {
		return false
	}
	if s.Corners != (Vec4{}) || s.Color1[3] < 1 || s.Color2[3] < 1 {
		return false
	}
	dr := r.devRect(s.Rect)
	return dr.Min.X <= 0 && dr.Min.Y <= 0 && dr.Max.X >= r.fb.W && dr.Max.Y >= r.fb.H
}

// visible mirrors the cocoa backend's surfaceHasVisibleContent: a surface is
// worth rasterizing only if it lies in the viewport and actually paints. The
// clip/transparency ops are handled by the caller regardless.
func (r *SoftRenderer) visible(s *Surface) bool {
	dr := r.devRect(s.Rect)
	if dr.Min.X >= r.fb.W || dr.Min.Y >= r.fb.H || dr.Max.X <= 0 || dr.Max.Y <= 0 {
		return false
	}
	switch {
	case s.FontId > 0 && s.GlyphId > 0: // glyph
		return true
	case s.ImageId > 0: // image
		return true
	case s.Stroke > 0: // border
		return true
	case s.Color2 != s.Color1: // gradient
		return true
	default: // plain fill: visible only if not fully transparent
		return s.Color1[3] > 0
	}
}

func (r *SoftRenderer) drawContent(s *Surface) {
	switch {
	case s.FontId > 0 && s.GlyphId > 0:
		r.drawGlyph(s)
	case s.ImageId > 0:
		r.drawImage(s)
	case s.Stroke > 0:
		r.drawBorder(s)
	case s.Color2 != s.Color1:
		r.drawGradient(s)
	default:
		r.drawSolid(s)
	}
}

// devRect maps a logical surface rect to device pixels, rounding each edge so
// fills stay crisp (the rounding-after-scale equivalent of the layout_tests seed).
func (r *SoftRenderer) devRect(rect Rect) image.Rectangle {
	x0 := int(Roundf32(rect.Origin[0]*r.scale)) - r.devOrigin.X
	y0 := int(Roundf32(rect.Origin[1]*r.scale)) - r.devOrigin.Y
	x1 := int(Roundf32((rect.Origin[0]+rect.Size[0])*r.scale)) - r.devOrigin.X
	y1 := int(Roundf32((rect.Origin[1]+rect.Size[1])*r.scale)) - r.devOrigin.Y
	return image.Rect(x0, y0, x1, y1)
}

func (r *SoftRenderer) galpha() uint32 { return uint32(r.alpha*255 + 0.5) }

// -----------------------------------------------------------------------------
//  Shapes
// -----------------------------------------------------------------------------

// A rounded rect is drawn as a plain-fill interior (the bulk of the pixels, on the
// solidRect fast path) plus up to four corner coverage masks. The corner mask is
// the only curved work, and it is cached by device-px radius (cornercache.go), so
// the per-frame vector rasterization that dominated phase-1 perf happens once per
// distinct radius and is then reused across thousands of surfaces — the same
// hit-rate story as glyphs. Color (solid or gradient) is applied per pixel as
// color × coverage, so the same coverage mask serves fills, gradients, and the
// clip mask alike (the plan's single primitive).

func (r *SoftRenderer) drawSolid(s *Surface) {
	dr := r.devRect(s.Rect)
	c := HSLAColor(s.Color1)
	if s.Corners == (Vec4{}) {
		r.solidRect(dr, c)
		return
	}
	ni, nc := r.decompose(dr, s.Corners)
	for i := 0; i < ni; i++ {
		r.solidRect(r.interiorBuf[i], c)
	}
	for i := 0; i < nc; i++ {
		cp := &r.cornerBuf[i]
		r.blendCornerSolid(cp.rect, fillCornerMask(cp.rad), cp.flipH, cp.flipV, c)
	}
}

func (r *SoftRenderer) drawGradient(s *Surface) {
	dr := r.devRect(s.Rect)
	c1 := HSLAColor(s.Color1)
	c2 := HSLAColor(s.Color2)
	if s.Corners == (Vec4{}) {
		r.gradientArea(dr, dr, c1, c2)
		return
	}
	ni, nc := r.decompose(dr, s.Corners)
	for i := 0; i < ni; i++ {
		r.gradientArea(r.interiorBuf[i], dr, c1, c2)
	}
	for i := 0; i < nc; i++ {
		cp := &r.cornerBuf[i]
		r.gradientCorner(cp, dr, fillCornerMask(cp.rad), c1, c2)
	}
}

// cornerPlace locates one corner of a rounded shape: the device square it occupies,
// the cache radius, and how to read the canonical (top-left) mask for this corner
// (a horizontal/vertical flip — free, no separate mask per orientation).
type cornerPlace struct {
	rect         image.Rectangle
	rad          int
	flipH, flipV bool
}

// decompose partitions a rounded rect's device bounds EXACTLY (disjoint, no gaps)
// into up to five interior rectangles (filled flat) plus up to four corner squares
// (filled through a coverage mask). Disjointness matters: overlapping fills would
// double-blend a translucent color. Handles per-corner radii; a zero radius folds
// into the interior rects (sharp corner). Results land in r.interiorBuf / cornerBuf
// to avoid per-call allocation.
func (r *SoftRenderer) decompose(dr image.Rectangle, corners Vec4) (ni, nc int) {
	maxr := float32(min(dr.Dx(), dr.Dy())) / 2
	rd := func(v float32) int {
		d := int(v*r.scale + 0.5)
		if float32(d) > maxr {
			d = int(maxr)
		}
		if d < 0 {
			d = 0
		}
		return d
	}
	tl, tr, br, bl := rd(corners[0]), rd(corners[1]), rd(corners[2]), rd(corners[3])
	x0, y0, x1, y1 := dr.Min.X, dr.Min.Y, dr.Max.X, dr.Max.Y

	add := func(a, b, c, d int) {
		if c > a && d > b {
			r.interiorBuf[ni] = image.Rect(a, b, c, d)
			ni++
		}
	}
	yTop := y0 + max(tl, tr)
	yBot := y1 - max(bl, br)
	add(x0, yTop, x1, yBot) // middle, full width

	mt := min(tl, tr) // top region: both corners cut, then the larger one only
	add(x0+tl, y0, x1-tr, y0+mt)
	if tl > tr {
		add(x0+tl, y0+mt, x1, yTop)
	} else if tr > tl {
		add(x0, y0+mt, x1-tr, yTop)
	}

	mb := min(bl, br) // bottom region, symmetric
	add(x0+bl, y1-mb, x1-br, y1)
	if bl > br {
		add(x0+bl, yBot, x1, y1-mb)
	} else if br > bl {
		add(x0, yBot, x1-br, y1-mb)
	}

	addCorner := func(rect image.Rectangle, rad int, fh, fv bool) {
		if rad > 0 {
			r.cornerBuf[nc] = cornerPlace{rect, rad, fh, fv}
			nc++
		}
	}
	addCorner(image.Rect(x0, y0, x0+tl, y0+tl), tl, false, false)
	addCorner(image.Rect(x1-tr, y0, x1, y0+tr), tr, true, false)
	addCorner(image.Rect(x1-br, y1-br, x1, y1), br, true, true)
	addCorner(image.Rect(x0, y1-bl, x0+bl, y1), bl, false, true)
	return ni, nc
}

// drawBorder paints a border stroke centered on the rect edge (CG semantics: the
// cocoa oracle strokes the path, so the line straddles the edge). The straight
// case is exact axis-aligned bands. The rounded case is four straight bands plus
// four cached corner rings (outer coverage minus inner coverage, keyed by radius +
// stroke); bands and rings share integer boundaries so they tile without overlap.
// ifloor / iceil are math.Floor / math.Ceil for a float32 (softrender.go avoids
// importing math); correct for negative inputs too (a border can sit above/left
// of the viewport).
func ifloor(v float32) int {
	i := int(v)
	if float32(i) > v {
		i--
	}
	return i
}

func iceil(v float32) int {
	i := int(v)
	if float32(i) < v {
		i++
	}
	return i
}

func (r *SoftRenderer) drawBorder(s *Surface) {
	c := HSLAColor(s.Color1)
	o, sz := s.Rect.Origin, s.Rect.Size
	// devOrigin (integer) subtracted before rounding: Round(v-k) == Round(v)-k, so
	// the border stays flush with its corner arcs under a region-local translation.
	ox, oy := float32(r.devOrigin.X), float32(r.devOrigin.Y)
	X0, Y0 := o[0]*r.scale-ox, o[1]*r.scale-oy
	X1, Y1 := (o[0]+sz[0])*r.scale-ox, (o[1]+sz[1])*r.scale-oy
	strokeDev := s.Stroke * r.scale
	hs := strokeDev / 2

	if s.Corners == (Vec4{}) {
		ox0, oy0 := int(Roundf32(X0-hs)), int(Roundf32(Y0-hs))
		ox1, oy1 := int(Roundf32(X1+hs)), int(Roundf32(Y1+hs))
		ix0, iy0 := int(Roundf32(X0+hs)), int(Roundf32(Y0+hs))
		ix1, iy1 := int(Roundf32(X1-hs)), int(Roundf32(Y1-hs))
		if ix1 <= ix0 || iy1 <= iy0 { // stroke meets in the middle: solid fill
			r.solidRect(image.Rect(ox0, oy0, ox1, oy1), c)
			return
		}
		r.solidRect(image.Rect(ox0, oy0, ox1, iy0), c) // top
		r.solidRect(image.Rect(ox0, iy1, ox1, oy1), c) // bottom
		r.solidRect(image.Rect(ox0, iy0, ix0, iy1), c) // left
		r.solidRect(image.Rect(ix1, iy0, ox1, iy1), c) // right
		return
	}

	x0i, y0i := int(Roundf32(X0)), int(Roundf32(Y0))
	x1i, y1i := int(Roundf32(X1)), int(Roundf32(Y1))
	maxr := float32(min(x1i-x0i, y1i-y0i)) / 2
	rd := func(v float32) int {
		d := int(v*r.scale + 0.5)
		if float32(d) > maxr {
			d = int(maxr)
		}
		if d < 0 {
			d = 0
		}
		return d
	}
	tl, tr, br, bl := rd(s.Corners[0]), rd(s.Corners[1]), rd(s.Corners[2]), rd(s.Corners[3])
	si := int(strokeDev + 0.5)
	// Snap each band's OUTER edge to the exact pixel its corner arc rasterizes onto,
	// then make it si thick inward. Two things are essential for the bands to stay
	// flush with the arcs on every box: (1) use the SAME snapped corner references
	// (x0i/y0i/...) the arcs are built from, not the raw edges, so a box's sub-pixel
	// position can't shift the band relative to its corner; (2) snap the outer edge
	// with floor on the low (top/left) side and ceil on the high (bottom/right)
	// side, which holds for both odd and even strokes (a 1px border becomes a 2px,
	// even, stroke at 2x). A plain Round left the top/left bands a pixel inside
	// their arcs — visible as corners poking past thin borders.
	loOut := func(s int) int { return ifloor(float32(s) - hs) } // top/left outer (incl)
	hiOut := func(s int) int { return iceil(float32(s) + hs) }  // bottom/right outer (excl)

	// corner centers (where each arc is centered)
	tlcx, tlcy := x0i+tl, y0i+tl
	trcx, trcy := x1i-tr, y0i+tr
	brcx, brcy := x1i-br, y1i-br
	blcx, blcy := x0i+bl, y1i-bl

	// straight edge bands between the corner centers, si thick from the outer edge
	tEdge, lEdge := loOut(y0i), loOut(x0i)
	bEdge, rEdge := hiOut(y1i), hiOut(x1i)
	r.solidRect(image.Rect(tlcx, tEdge, trcx, tEdge+si), c) // top
	r.solidRect(image.Rect(blcx, bEdge-si, brcx, bEdge), c) // bottom
	r.solidRect(image.Rect(lEdge, tlcy, lEdge+si, blcy), c) // left
	r.solidRect(image.Rect(rEdge-si, trcy, rEdge, brcy), c) // right

	// corner rings; the canonical mask curves toward the top-left, flipped per corner
	ringCorner := func(cx, cy, rad int, fh, fv bool) {
		cm := borderCornerMask(rad, si)
		if cm == nil {
			return
		}
		n := cm.dim
		var box image.Rectangle
		switch {
		case !fh && !fv: // TL: center is bottom-right of the box
			box = image.Rect(cx-n, cy-n, cx, cy)
		case fh && !fv: // TR: center bottom-left
			box = image.Rect(cx, cy-n, cx+n, cy)
		case fh && fv: // BR: center top-left
			box = image.Rect(cx, cy, cx+n, cy+n)
		default: // BL: center top-right
			box = image.Rect(cx-n, cy, cx, cy+n)
		}
		r.blendCornerSolid(box, cm, fh, fv, c)
	}
	ringCorner(tlcx, tlcy, tl, false, false)
	ringCorner(trcx, trcy, tr, true, false)
	ringCorner(brcx, brcy, br, true, true)
	ringCorner(blcx, blcy, bl, false, true)
}

// -----------------------------------------------------------------------------
//  Glyphs & images
// -----------------------------------------------------------------------------

// drawGlyph composites a cached glyph alpha mask (glyphcache.go), tinted with the
// text color (Color1). Placement reuses the cocoa pen origin: baseline ~0.82 down
// from the rect top; the cached device-px OffX/OffY locate the bitmap. Requires
// GlyphCacheBudgetBytes > 0 (the cache must be populated for this frame).
func (r *SoftRenderer) drawGlyph(s *Surface) {
	key, ok := GlyphKeyForSurface(s)
	if !ok {
		return
	}
	bm, ok := GlyphBitmap(key)
	if !ok || bm.W == 0 || bm.H == 0 || len(bm.Alpha) == 0 {
		return // not cached yet, or an empty glyph (whitespace)
	}
	penX := (s.Rect.Origin[0] + s.GlyphOffset[0]) * r.scale
	penY := (s.Rect.Origin[1] + s.Rect.Size[1]*0.82 + s.GlyphOffset[1]) * r.scale
	x0 := int(Roundf32(penX+bm.OffX)) - r.devOrigin.X
	y0 := int(Roundf32(penY+bm.OffY)) - r.devOrigin.Y
	dest := image.Rect(x0, y0, x0+bm.W, y0+bm.H)
	r.maskColor(dest, bm.Alpha, bm.Stride, HSLAColor(s.Color1))
}

// drawImage paints an image surface (a loaded image, or a generated shadow).
// Container images set ImageScale (fit to the surface height, like the cocoa/gio
// backends); shadows draw at natural size. Scaling goes through x/image/draw; the
// scaled premultiplied RGBA is then blitted (swizzled to BGRA) with src-over.
func (r *SoftRenderer) drawImage(s *Surface) {
	imgData := LookupImage(s.ImageId)
	if imgData == nil {
		return
	}
	src := &imgData.RGBA
	b := src.Bounds()
	iw, ih := b.Dx(), b.Dy()
	if iw == 0 || ih == 0 || len(src.Pix) == 0 {
		return // not decoded yet (large images decode in the background)
	}

	dwl, dhl := float32(iw), float32(ih) // logical dest size
	if s.ImageScale {
		fit := s.Rect.Size[1] / float32(ih)
		dwl, dhl = float32(iw)*fit, float32(ih)*fit
	}
	x0 := int(Roundf32(s.Rect.Origin[0]*r.scale)) - r.devOrigin.X
	y0 := int(Roundf32(s.Rect.Origin[1]*r.scale)) - r.devOrigin.Y
	dw := int(Roundf32(dwl * r.scale))
	dh := int(Roundf32(dhl * r.scale))
	if dw <= 0 || dh <= 0 {
		return
	}
	dest := image.Rect(x0, y0, x0+dw, y0+dh)

	scaled := src
	if dw != iw || dh != ih {
		scaled = scaledImage(s.ImageId, src, dw, dh)
	}
	r.blitPremul(dest, scaled)
}

// -----------------------------------------------------------------------------
//  Compositing primitives (the single "blend through coverage" operation)
// -----------------------------------------------------------------------------

// blendBGRA src-over composites a premultiplied source (sb,sg,sr already scaled
// by effA) with effective alpha effA onto a 4-byte BGRA pixel.
func blendBGRA(p []byte, sr, sg, sb, effA uint32) {
	inv := 255 - effA
	p[0] = uint8(sb + uint32(p[0])*inv/255)
	p[1] = uint8(sg + uint32(p[1])*inv/255)
	p[2] = uint8(sr + uint32(p[2])*inv/255)
	p[3] = uint8(effA + uint32(p[3])*inv/255)
}

// solidRect fills a device rect with a solid color, honoring the clip and group
// alpha. Three cases by cost: no clip mask + opaque → a memmove fill; no clip mask
// + translucent → a constant-coverage src-over (effA hoisted out of the loop); a
// clip coverage mask present → the general per-pixel blend.
func (r *SoftRenderer) solidRect(dr image.Rectangle, c color.NRGBA) {
	area := dr.Intersect(r.clip.rect)
	if area.Empty() || c.A == 0 {
		return
	}
	mask := r.clipMaskFor(area)
	ga := r.galpha()
	R, G, B, A := uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A)

	if mask == nil {
		// Coverage is a constant 255, so effA is loop-invariant.
		effA := A * ga / 255
		if effA == 0 {
			return
		}
		if effA == 255 { // opaque: fill with one repeated pixel via memmove
			r.fillRectOpaque(area, c.B, c.G, c.R)
			return
		}
		sr, sg, sb := R*effA/255, G*effA/255, B*effA/255
		for y := area.Min.Y; y < area.Max.Y; y++ {
			i := y*r.fb.Stride + area.Min.X*4
			for x := area.Min.X; x < area.Max.X; x++ {
				blendBGRA(r.fb.Pix[i:i+4:i+4], sr, sg, sb, effA)
				i += 4
			}
		}
		return
	}

	// Clip coverage varies per pixel.
	cr := r.clip.rect
	cstride := cr.Dx()
	for y := area.Min.Y; y < area.Max.Y; y++ {
		i := y*r.fb.Stride + area.Min.X*4
		mi := (y-cr.Min.Y)*cstride + (area.Min.X - cr.Min.X)
		for x := area.Min.X; x < area.Max.X; x++ {
			cov := uint32(mask[mi])
			effA := A * cov / 255 * ga / 255
			if effA > 0 {
				blendBGRA(r.fb.Pix[i:i+4:i+4], R*effA/255, G*effA/255, B*effA/255, effA)
			}
			mi++
			i += 4
		}
	}
}

// fillRectOpaque fills area with the opaque pixel (b,g,r,0xFF) using memmove: seed
// one pixel, double-fill the first row, then copy that row onto the rest. This runs
// near memory bandwidth instead of a per-pixel store loop.
func (r *SoftRenderer) fillRectOpaque(area image.Rectangle, b, g, rr byte) {
	span := area.Dx() * 4
	if span <= 0 {
		return
	}
	stride := r.fb.Stride
	p := r.fb.Pix
	row0 := area.Min.Y*stride + area.Min.X*4
	p[row0], p[row0+1], p[row0+2], p[row0+3] = b, g, rr, 0xff
	dst := p[row0 : row0+span]
	for n := 4; n < span; n *= 2 {
		copy(dst[n:], dst[:n])
	}
	for y := area.Min.Y + 1; y < area.Max.Y; y++ {
		o := y*stride + area.Min.X*4
		copy(p[o:o+span], dst)
	}
}

// maskColor blends a solid color through a shape coverage mask (rounded fill,
// border ring, or glyph). The mask is aligned to dr (one byte per pixel,
// smask[(y-dr.Min.Y)*sstride + (x-dr.Min.X)]); coverage is combined with the clip
// mask and group alpha.
func (r *SoftRenderer) maskColor(dr image.Rectangle, smask []byte, sstride int, c color.NRGBA) {
	area := dr.Intersect(r.clip.rect)
	if area.Empty() || c.A == 0 || len(smask) == 0 {
		return
	}
	cr := r.clip.rect
	cmask := r.clipMaskFor(area)
	cstride := cr.Dx()
	ga := r.galpha()
	R, G, B, A := uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A)

	for y := area.Min.Y; y < area.Max.Y; y++ {
		i := y*r.fb.Stride + area.Min.X*4
		si := (y-dr.Min.Y)*sstride + (area.Min.X - dr.Min.X)
		ci := 0
		if cmask != nil {
			ci = (y-cr.Min.Y)*cstride + (area.Min.X - cr.Min.X)
		}
		for x := area.Min.X; x < area.Max.X; x++ {
			cov := uint32(smask[si])
			if cmask != nil {
				cov = cov * uint32(cmask[ci]) / 255
				ci++
			}
			effA := A * cov / 255 * ga / 255
			if effA > 0 {
				blendBGRA(r.fb.Pix[i:i+4:i+4], R*effA/255, G*effA/255, B*effA/255, effA)
			}
			si++
			i += 4
		}
	}
}

// gradientArea fills a device rect with a vertical gradient (Color1 at the top of
// `full`, Color2 at its bottom; interpolated in straight RGBA, matching the CG
// oracle), honoring clip and group alpha. `area` is the rect to paint; `full` is
// the whole surface rect that parameterizes the gradient (so an interior sub-rect
// of a rounded gradient still samples the right color band).
func (r *SoftRenderer) gradientArea(area, full image.Rectangle, c1, c2 color.NRGBA) {
	area = area.Intersect(r.clip.rect)
	if area.Empty() {
		return
	}
	fh := float32(full.Dy())
	if fh <= 0 {
		return
	}
	cr := r.clip.rect
	cmask := r.clipMaskFor(area)
	cstride := cr.Dx()
	ga := r.galpha()

	for y := area.Min.Y; y < area.Max.Y; y++ {
		R, G, B, A := gradientRow(full, y, c1, c2)
		i := y*r.fb.Stride + area.Min.X*4
		ci := 0
		if cmask != nil {
			ci = (y-cr.Min.Y)*cstride + (area.Min.X - cr.Min.X)
		}
		for x := area.Min.X; x < area.Max.X; x++ {
			cov := uint32(255)
			if cmask != nil {
				cov = uint32(cmask[ci])
				ci++
			}
			effA := A * cov / 255 * ga / 255
			if effA > 0 {
				blendBGRA(r.fb.Pix[i:i+4:i+4], R*effA/255, G*effA/255, B*effA/255, effA)
			}
			i += 4
		}
	}
}

// gradientRow returns the straight-RGBA gradient color for device row y, sampled
// over the full surface rect (Color1 at top, Color2 at bottom).
func gradientRow(full image.Rectangle, y int, c1, c2 color.NRGBA) (R, G, B, A uint32) {
	t := (float32(y-full.Min.Y) + 0.5) / float32(full.Dy())
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return lerp8(c1.R, c2.R, t), lerp8(c1.G, c2.G, t), lerp8(c1.B, c2.B, t), lerp8(c1.A, c2.A, t)
}

func (r *SoftRenderer) blendCornerSolid(box image.Rectangle, cm *cornerMask, flipH, flipV bool, c color.NRGBA) {
	r.drawCorner(box, cm, flipH, flipV, c, c, false, image.Rectangle{})
}

func (r *SoftRenderer) gradientCorner(cp *cornerPlace, full image.Rectangle, cm *cornerMask, c1, c2 color.NRGBA) {
	r.drawCorner(cp.rect, cm, cp.flipH, cp.flipV, c1, c2, true, full)
}

// drawCorner blends a corner coverage mask, tinted by a solid color (gradient
// false) or a vertical gradient over `full` (gradient true). It uses the mask's
// per-row spans so the empty region is skipped entirely and the fully-covered run
// is a straight write (or a constant-color blend) — only the antialiased arc takes
// the per-pixel coverage blend. Output is identical to a full per-pixel loop; this
// only avoids touching pixels whose contribution is 0 or whose coverage is 255.
//
// When a clip coverage mask actually applies to this corner (rare — only corners
// that overlap the clip's own corners), coverage varies per pixel, so it falls
// back to a per-pixel loop bounded by the nonzero span.
func (r *SoftRenderer) drawCorner(box image.Rectangle, cm *cornerMask, flipH, flipV bool, c1, c2 color.NRGBA, gradient bool, full image.Rectangle) {
	if cm == nil {
		return
	}
	area := box.Intersect(r.clip.rect)
	if area.Empty() {
		return
	}
	dim := cm.dim
	cmask := r.clipMaskFor(area)
	cr := r.clip.rect
	cstride := cr.Dx()
	ga := r.galpha()
	bx := box.Min.X

	R, G, B, A := uint32(c1.R), uint32(c1.G), uint32(c1.B), uint32(c1.A)
	if !gradient && A == 0 {
		return
	}

	for y := area.Min.Y; y < area.Max.Y; y++ {
		if gradient {
			R, G, B, A = gradientRow(full, y, c1, c2)
			if A == 0 {
				continue
			}
		}
		j := y - box.Min.Y
		if flipV {
			j = dim - 1 - j
		}
		nzLo, nzHi := int(cm.nzLo[j]), int(cm.nzHi[j])
		if nzHi <= nzLo {
			continue // empty row
		}
		row := cm.pix[j*dim:]
		base := y * r.fb.Stride

		dLo, dHi := mapSpan(bx, dim, nzLo, nzHi, flipH)
		if dLo < area.Min.X {
			dLo = area.Min.X
		}
		if dHi > area.Max.X {
			dHi = area.Max.X
		}
		if dLo >= dHi {
			continue
		}

		if cmask != nil {
			// Clip coverage varies per pixel: bounded per-pixel loop over nonzero span.
			ii, step := canonIndex(bx, dim, dLo, flipH)
			ci := (y-cr.Min.Y)*cstride + (dLo - cr.Min.X)
			i := base + dLo*4
			for x := dLo; x < dHi; x++ {
				cov := uint32(row[ii]) * uint32(cmask[ci]) / 255
				if effA := A * cov / 255 * ga / 255; effA > 0 {
					blendBGRA(r.fb.Pix[i:i+4:i+4], R*effA/255, G*effA/255, B*effA/255, effA)
				}
				ii += step
				ci++
				i += 4
			}
			continue
		}

		// Full-coverage run [dfLo,dfHi) within the nonzero span: constant color.
		dfLo, dfHi := dHi, dHi
		if fLo, fHi := int(cm.fLo[j]), int(cm.fHi[j]); fHi > fLo {
			dfLo, dfHi = mapSpan(bx, dim, fLo, fHi, flipH)
			if dfLo < dLo {
				dfLo = dLo
			}
			if dfHi > dHi {
				dfHi = dHi
			}
			if dfHi < dfLo {
				dfLo, dfHi = dHi, dHi
			}
		}

		if dfLo > dLo { // left antialiased edge
			ii, step := canonIndex(bx, dim, dLo, flipH)
			r.cornerAARun(base, dLo, dfLo, ii, step, row, R, G, B, A, ga)
		}
		if dfHi > dfLo { // fully covered run
			fullA := A * ga / 255
			i := base + dfLo*4
			if fullA == 255 {
				cb, cg, crr := byte(B), byte(G), byte(R)
				for x := dfLo; x < dfHi; x++ {
					r.fb.Pix[i] = cb
					r.fb.Pix[i+1] = cg
					r.fb.Pix[i+2] = crr
					r.fb.Pix[i+3] = 0xff
					i += 4
				}
			} else {
				sr, sg, sb := R*fullA/255, G*fullA/255, B*fullA/255
				for x := dfLo; x < dfHi; x++ {
					blendBGRA(r.fb.Pix[i:i+4:i+4], sr, sg, sb, fullA)
					i += 4
				}
			}
		}
		if dHi > dfHi { // right antialiased edge
			ii, step := canonIndex(bx, dim, dfHi, flipH)
			r.cornerAARun(base, dfHi, dHi, ii, step, row, R, G, B, A, ga)
		}
	}
}

// cornerAARun blends one antialiased run of a corner row: device columns [xLo,xHi),
// reading coverage from row starting at canonical index ii0 and stepping by step
// (+1 normal, -1 when the corner is horizontally flipped) — no per-pixel flip branch.
func (r *SoftRenderer) cornerAARun(base, xLo, xHi, ii0, step int, row []byte, R, G, B, A, ga uint32) {
	ii := ii0
	i := base + xLo*4
	for x := xLo; x < xHi; x++ {
		if cov := uint32(row[ii]); cov != 0 {
			if effA := A * cov / 255 * ga / 255; effA > 0 {
				blendBGRA(r.fb.Pix[i:i+4:i+4], R*effA/255, G*effA/255, B*effA/255, effA)
			}
		}
		ii += step
		i += 4
	}
}

// mapSpan maps a canonical x-range [lo,hi) to device columns, accounting for a
// horizontal flip; canonIndex gives the canonical index and step for device column x.
func mapSpan(bx, dim, lo, hi int, flipH bool) (int, int) {
	if flipH {
		return bx + dim - hi, bx + dim - lo
	}
	return bx + lo, bx + hi
}

func canonIndex(bx, dim, x int, flipH bool) (ii, step int) {
	if flipH {
		return dim - 1 - (x - bx), -1
	}
	return x - bx, 1
}

// blitPremul composites a premultiplied RGBA source (already at device dest size,
// or sampled 1:1) into the BGRA framebuffer over dest, swizzling and honoring the
// clip mask and group alpha.
func (r *SoftRenderer) blitPremul(dest image.Rectangle, src *image.RGBA) {
	area := dest.Intersect(r.clip.rect)
	if area.Empty() {
		return
	}
	cr := r.clip.rect
	cmask := r.clipMaskFor(area)
	cstride := cr.Dx()
	ga := r.galpha()
	sb := src.Bounds().Min

	for y := area.Min.Y; y < area.Max.Y; y++ {
		i := y*r.fb.Stride + area.Min.X*4
		sp := src.PixOffset(sb.X+area.Min.X-dest.Min.X, sb.Y+y-dest.Min.Y)
		ci := 0
		if cmask != nil {
			ci = (y-cr.Min.Y)*cstride + (area.Min.X - cr.Min.X)
		}
		for x := area.Min.X; x < area.Max.X; x++ {
			sr := uint32(src.Pix[sp])    // premultiplied R
			sg := uint32(src.Pix[sp+1])  // premultiplied G
			sbb := uint32(src.Pix[sp+2]) // premultiplied B
			sa := uint32(src.Pix[sp+3])  // A
			f := ga
			if cmask != nil {
				f = f * uint32(cmask[ci]) / 255
				ci++
			}
			if f != 255 {
				sr = sr * f / 255
				sg = sg * f / 255
				sbb = sbb * f / 255
				sa = sa * f / 255
			}
			if sa > 0 {
				blendBGRA(r.fb.Pix[i:i+4:i+4], sr, sg, sbb, sa)
			}
			sp += 4
			i += 4
		}
	}
}

func lerp8(a, b uint8, t float32) uint32 {
	return uint32(float32(a) + (float32(b)-float32(a))*t + 0.5)
}

// -----------------------------------------------------------------------------
//  Clip stack
// -----------------------------------------------------------------------------

// pushClip intersects the current clip with the surface's rect. The fast path is
// a scissor-rect intersection (no mask) for a square clip with a square parent;
// a rounded clip (or any rounded clip nested inside another) maintains a per-pixel
// coverage mask so content does not bleed into the corners.
func (r *SoftRenderer) pushClip(s *Surface) {
	r.clipStack = append(r.clipStack, r.clip)

	dr := r.devRect(s.Rect)
	newRect := r.clip.rect.Intersect(dr) // r.clip.rect is already within the buffer
	parent := r.clip

	if newRect.Empty() {
		r.clip = clipState{rect: newRect}
		return
	}

	if s.Corners == (Vec4{}) {
		if parent.mask == nil {
			r.clip = clipState{rect: newRect} // pure scissor rect
			return
		}
		// Square clip inside a coverage clip. If the parent is corner-only and the
		// new rect avoids every parent corner, the result is a plain rect — the
		// common case of square scroll content inside a rounded scroll view.
		var corners [4]image.Rectangle
		parentCornerOnly := parent.corners != nil && parent.corners.cornerOnly
		if parentCornerOnly {
			any := false
			for i := range parent.corners.squares {
				sq := parent.corners.squares[i].Intersect(newRect)
				corners[i] = sq
				if !sq.Empty() {
					any = true
				}
			}
			if !any {
				r.clip = clipState{rect: newRect} // pure rect: no corner survives
				return
			}
		}
		// Otherwise crop the parent coverage to newRect.
		w, h := newRect.Dx(), newRect.Dy()
		m := r.clipBuf(w * h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				m[y*w+x] = clipCoverage(&parent, newRect.Min.X+x, newRect.Min.Y+y)
			}
		}
		r.clip = clipState{rect: newRect, mask: m, corners: &clipCorners{squares: corners, cornerOnly: parentCornerOnly}}
		return
	}

	// Rounded clip: build the per-pixel coverage mask over newRect from the cached
	// corner masks instead of rasterizing the whole path every frame. The interior
	// is fully visible (255); only the four corner squares carry the antialiased
	// quarter-disk coverage (0 in the cut, 255 inside). This is the same corner
	// cache used by fills, so a scroll container's rounded clip costs four small
	// stamps + a memset, not a full-window vector rasterization. We also record the
	// corner squares so draws that avoid them skip the mask entirely (clipMaskFor).
	w, h := newRect.Dx(), newRect.Dy()
	m := r.clipBuf(w * h)
	fillByte(m, 255) // fully-visible interior; corners stamped below
	var corners [4]image.Rectangle
	_, nc := r.decompose(dr, s.Corners) // corner squares in device coords (dr-relative)
	for k := 0; k < nc; k++ {
		cp := r.cornerBuf[k]
		cm := fillCornerMask(cp.rad)
		if cm == nil {
			continue
		}
		sq := cp.rect.Intersect(newRect)
		corners[k] = sq
		for y := sq.Min.Y; y < sq.Max.Y; y++ {
			jj := y - cp.rect.Min.Y
			if cp.flipV {
				jj = cp.rad - 1 - jj
			}
			row := jj * cm.dim
			for x := sq.Min.X; x < sq.Max.X; x++ {
				ii := x - cp.rect.Min.X
				if cp.flipH {
					ii = cp.rad - 1 - ii
				}
				m[(y-newRect.Min.Y)*w+(x-newRect.Min.X)] = cm.pix[row+ii]
			}
		}
	}
	// A rounded clip alone is corner-only. Nesting inside another coverage clip
	// reduces the interior too, so the result is no longer corner-only (rare).
	cornerOnly := parent.mask == nil
	if parent.mask != nil {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				idx := y*w + x
				m[idx] = byte(uint32(m[idx]) * uint32(clipCoverage(&parent, newRect.Min.X+x, newRect.Min.Y+y)) / 255)
			}
		}
	}
	r.clip = clipState{rect: newRect, mask: m, corners: &clipCorners{squares: corners, cornerOnly: cornerOnly}}
}

// clipMaskFor returns the per-pixel clip coverage that applies to a draw confined
// to area, or nil if the clip is a plain rectangle there. A corner-only clip (a
// rounded clip whose coverage is 255 except in its corner squares) returns nil for
// any draw that avoids those corners — so interior content takes the fast,
// mask-free path even while a rounded clip is active. area must already be clamped
// to r.clip.rect by the caller.
func (r *SoftRenderer) clipMaskFor(area image.Rectangle) []byte {
	cs := &r.clip
	if cs.mask == nil {
		return nil
	}
	if cs.corners != nil && cs.corners.cornerOnly {
		for i := range cs.corners.squares {
			if cs.corners.squares[i].Overlaps(area) {
				return cs.mask
			}
		}
		return nil
	}
	return cs.mask
}

// clipBuf returns a reusable coverage buffer of length n for the current clip
// depth. One buffer per depth is enough because at most one clip mask is live per
// depth at any time; reusing them keeps rounded clips allocation-free across frames.
func (r *SoftRenderer) clipBuf(n int) []byte {
	d := len(r.clipStack) // parent already pushed; index by the new clip's depth
	for len(r.clipMaskArena) <= d {
		r.clipMaskArena = append(r.clipMaskArena, nil)
	}
	b := r.clipMaskArena[d]
	if cap(b) < n {
		b = make([]byte, n)
		r.clipMaskArena[d] = b
	}
	return b[:n]
}

func clipCoverage(cs *clipState, x, y int) byte {
	if x < cs.rect.Min.X || x >= cs.rect.Max.X || y < cs.rect.Min.Y || y >= cs.rect.Max.Y {
		return 0
	}
	if cs.mask == nil {
		return 255
	}
	return cs.mask[(y-cs.rect.Min.Y)*cs.rect.Dx()+(x-cs.rect.Min.X)]
}
