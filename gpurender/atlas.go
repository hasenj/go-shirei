//go:build darwin || linux || android || windows || js

package gpurender

import (
	"image"

	"go.hasen.dev/shirei"
)

const (
	glyphAtlasW = 2048
	glyphAtlasH = 2048
	colorAtlasW = 1024
	colorAtlasH = 1024
	atlasPad    = 1
)

const (
	texFill       int32 = 0
	texGlyph      int32 = 1
	texColorGlyph int32 = 2
	texImage      int32 = 3
)

type atlasSlot struct {
	x, y, w, h   int
	u, v, uw, vh float32
}

// stroke 0 = fill quarter-disk; otherwise an inside-stroke ring.
type cornerStampKey struct {
	rad, stroke uint16
}

type shelfAtlas struct {
	w, h       int
	x, y, rowH int
}

func (a *shelfAtlas) reset() {
	a.x, a.y, a.rowH = 0, 0, 0
}

// alloc packs a bw×bh stamp with 1px zero padding. ok is false when the
// atlas is full (caller resets and retries, or falls back to software).
func (a *shelfAtlas) alloc(bw, bh int) (x, y int, ok bool) {
	if bw <= 0 || bh <= 0 {
		return 0, 0, false
	}
	w, h := bw+2*atlasPad, bh+2*atlasPad
	if w > a.w || h > a.h {
		return 0, 0, false
	}
	if a.x+w > a.w {
		a.y += a.rowH
		a.x = 0
		a.rowH = 0
	}
	if a.y+h > a.h {
		return 0, 0, false
	}
	if h > a.rowH {
		a.rowH = h
	}
	x, y = a.x, a.y
	a.x += w
	return x, y, true
}

func slotUV(ax, ay, aw, ah, atlasW, atlasH int) atlasSlot {
	innerX := ax + atlasPad
	innerY := ay + atlasPad
	return atlasSlot{
		x: innerX, y: innerY, w: aw, h: ah,
		u:  float32(innerX) / float32(atlasW),
		v:  float32(innerY) / float32(atlasH),
		uw: float32(aw) / float32(atlasW),
		vh: float32(ah) / float32(atlasH),
	}
}

type gpuCache struct {
	glyphs      shelfAtlas
	colors      shelfAtlas
	glyph       map[shirei.GlyphKey]atlasSlot
	color       map[shirei.GlyphKey]atlasSlot
	corner      map[cornerStampKey]atlasSlot
	image       map[shirei.ImageId]gpuImg // last uploaded stamp
	failed      bool
	upload      int
	rgbaScratch []byte
}

func (c *gpuCache) init() {
	c.glyphs = shelfAtlas{w: glyphAtlasW, h: glyphAtlasH}
	c.colors = shelfAtlas{w: colorAtlasW, h: colorAtlasH}
	c.glyph = make(map[shirei.GlyphKey]atlasSlot)
	c.color = make(map[shirei.GlyphKey]atlasSlot)
	c.corner = make(map[cornerStampKey]atlasSlot)
	c.image = make(map[shirei.ImageId]gpuImg)
}

type gpuImg struct {
	gen      uint64
	w, h     int
	external bool
}

func (c *gpuCache) resetAtlases() {
	c.glyphs.reset()
	c.colors.reset()
	clear(c.glyph)
	clear(c.color)
	clear(c.corner)
	gpuResetAtlases()
}

func (c *gpuCache) evict(keys []shirei.GlyphKey) {
	for _, k := range keys {
		delete(c.glyph, k)
		delete(c.color, k)
	}
}

var gpu gpuCache

func packCorner(rad, stroke int) (atlasSlot, bool) {
	if rad <= 0 || rad > 65535 || stroke < 0 || stroke > 65535 {
		return atlasSlot{}, false
	}
	key := cornerStampKey{uint16(rad), uint16(stroke)}
	if s, ok := gpu.corner[key]; ok {
		return s, true
	}
	var pix []byte
	var dim int
	if stroke == 0 {
		pix, dim = shirei.CornerFillCoverage(rad)
	} else {
		pix, dim = shirei.CornerBorderCoverage(rad, stroke)
	}
	if pix == nil || dim <= 0 {
		return atlasSlot{}, false
	}
	ax, ay, ok := gpu.glyphs.alloc(dim, dim)
	if !ok {
		return atlasSlot{}, false
	}
	slot := slotUV(ax, ay, dim, dim, glyphAtlasW, glyphAtlasH)
	if !gpuUploadR8(slot.x, slot.y, dim, dim, dim, pix) {
		return atlasSlot{}, false
	}
	gpu.corner[key] = slot
	gpu.upload += dim * dim
	return slot, true
}

func flipUV(s atlasSlot, fh, fv bool) (u, v, uw, vh float32) {
	u, v, uw, vh = s.u, s.v, s.uw, s.vh
	if fh {
		u = u + uw
		uw = -uw
	}
	if fv {
		v = v + vh
		vh = -vh
	}
	return
}

func packGlyph(key shirei.GlyphKey, bm shirei.GlyphBM) (atlasSlot, bool) {
	if s, ok := gpu.glyph[key]; ok {
		return s, true
	}
	ax, ay, ok := gpu.glyphs.alloc(bm.W, bm.H)
	if !ok {
		return atlasSlot{}, false
	}
	slot := slotUV(ax, ay, bm.W, bm.H, glyphAtlasW, glyphAtlasH)
	if !gpuUploadR8(slot.x, slot.y, bm.W, bm.H, bm.Stride, bm.Alpha) {
		return atlasSlot{}, false
	}
	gpu.glyph[key] = slot
	gpu.upload += bm.W * bm.H
	return slot, true
}

func packColorGlyph(key shirei.GlyphKey, bm shirei.GlyphBM) (atlasSlot, bool) {
	if s, ok := gpu.color[key]; ok {
		return s, true
	}
	ax, ay, ok := gpu.colors.alloc(bm.W, bm.H)
	if !ok {
		return atlasSlot{}, false
	}
	slot := slotUV(ax, ay, bm.W, bm.H, colorAtlasW, colorAtlasH)
	dst := scratchRGBA(bm.W * bm.H * 4)
	bgraPremulToRGBA(bm.RGBA, bm.Stride, bm.W, bm.H, dst)
	if !gpuUploadColor(slot.x, slot.y, bm.W, bm.H, bm.W*4, dst) {
		return atlasSlot{}, false
	}
	gpu.color[key] = slot
	gpu.upload += bm.W * bm.H * 4
	return slot, true
}

func ensureImage(id shirei.ImageId, data *shirei.ImageData) bool {
	src := &data.RGBA
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w <= 0 || h <= 0 {
		return false
	}
	if m, ok := gpu.image[id]; ok && !m.external && m.gen == data.Generation && m.w == w && m.h == h {
		return true
	}
	dst := scratchRGBA(w * h * 4)
	premulImageRGBA(src, dst)
	if !gpuImageSet(uint32(id), w, h, w*4, dst) {
		return false
	}
	gpu.image[id] = gpuImg{gen: data.Generation, w: w, h: h}
	gpu.upload += w * h * 4
	return true
}

func scratchRGBA(n int) []byte {
	if cap(gpu.rgbaScratch) < n {
		gpu.rgbaScratch = make([]byte, n)
	}
	return gpu.rgbaScratch[:n]
}

// cocoa glyph RGBA stamps are premul BGRA (Host.PixelOrder).
func bgraPremulToRGBA(src []byte, stride, w, h int, dst []byte) {
	di := 0
	for y := 0; y < h; y++ {
		row := y * stride
		for x := 0; x < w; x++ {
			si := row + x*4
			dst[di+0] = src[si+2]
			dst[di+1] = src[si+1]
			dst[di+2] = src[si+0]
			dst[di+3] = src[si+3]
			di += 4
		}
	}
}

func premulImageRGBA(src *image.RGBA, dst []byte) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	rowBytes := w * 4
	base := src.PixOffset(b.Min.X, b.Min.Y)
	pix := src.Pix
	for y := 0; y < h; y++ {
		si := base + y*src.Stride
		di := y * rowBytes
		srcRow := pix[si : si+rowBytes]
		dstRow := dst[di : di+rowBytes]
		for i := 0; i < rowBytes; i += 4 {
			a := uint32(srcRow[i+3])
			if a == 255 {
				dstRow[i+0] = srcRow[i+0]
				dstRow[i+1] = srcRow[i+1]
				dstRow[i+2] = srcRow[i+2]
				dstRow[i+3] = 255
			} else if a == 0 {
				dstRow[i+0] = 0
				dstRow[i+1] = 0
				dstRow[i+2] = 0
				dstRow[i+3] = 0
			} else {
				dstRow[i+0] = byte(uint32(srcRow[i+0]) * a / 255)
				dstRow[i+1] = byte(uint32(srcRow[i+1]) * a / 255)
				dstRow[i+2] = byte(uint32(srcRow[i+2]) * a / 255)
				dstRow[i+3] = byte(a)
			}
		}
	}
}
