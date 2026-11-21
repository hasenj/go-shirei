//go:build darwin || linux || android || windows || js

package gpurender

import (
	"image"
	"image/color"

	"go.hasen.dev/shirei"
)

// Draw list: fills, vertical gradients, borders, rect+rounded clip, group
// alpha, glyphs, images. Rounded fills/borders use CPU corner-mask stamps
// (same decompose as softrender). Rounded clip is an SDF in the shader.

type Quad struct {
	X, Y, W, H     float32
	U, V, UW, VH   float32
	R, G, B, A     float32
	R2, G2, B2, A2 float32
}

type Batch struct {
	First, Count               int32
	ClipX, ClipY, ClipW, ClipH int32
	TexKind, TexKey            int32
	ClipN, ClipGen             int32
	ClipRect                   [4][4]float32
	ClipRad                    [4][4]float32
}

type clipFrame struct {
	scissor image.Rectangle
	shape   image.Rectangle
	radii   [4]float32
	round   bool
}

type builder struct {
	quads       []Quad
	batches     []Batch
	clip        image.Rectangle
	clipStack   []clipFrame
	curRound    bool
	curShape    image.Rectangle
	curRadii    [4]float32
	clipGen     int32
	alpha       float32
	alphaPrev   []float32
	scale       float32
	viewport    image.Rectangle
	skipClear   bool
	interiorBuf [5]image.Rectangle
	cornerBuf   [4]cornerPlace
	glyphRuns   []shirei.GlyphRun
}

type cornerPlace struct {
	rect         image.Rectangle
	rad          int
	flipH, flipV bool
}

func (b *builder) reset(devW, devH int, scale float32) {
	b.quads = b.quads[:0]
	b.batches = b.batches[:0]
	b.clipStack = b.clipStack[:0]
	b.alphaPrev = b.alphaPrev[:0]
	b.alpha = 1
	b.scale = scale
	b.viewport = image.Rect(0, 0, devW, devH)
	b.clip = b.viewport
	b.curRound = false
	b.curShape = image.Rectangle{}
	b.curRadii = [4]float32{}
	b.clipGen = 0
	b.skipClear = false
}

func (b *builder) devRect(rect shirei.Rect) image.Rectangle {
	x0 := int(shirei.Roundf32(rect.Origin[0] * b.scale))
	y0 := int(shirei.Roundf32(rect.Origin[1] * b.scale))
	x1 := int(shirei.Roundf32((rect.Origin[0] + rect.Size[0]) * b.scale))
	y1 := int(shirei.Roundf32((rect.Origin[1] + rect.Size[1]) * b.scale))
	return image.Rect(x0, y0, x1, y1)
}

func (b *builder) build(surfaces []shirei.Surface, runs []shirei.GlyphRun, scale float32, devW, devH int) {
	b.reset(devW, devH, scale)
	b.glyphRuns = runs
	if len(surfaces) > 0 && coversViewportOpaque(&surfaces[0], b) {
		b.skipClear = true
	}
	for i := range surfaces {
		b.one(&surfaces[i])
	}
}

func coversViewportOpaque(s *shirei.Surface, b *builder) bool {
	if s.Stroke != 0 || s.ImageId != 0 || s.GlyphId != 0 || s.GlyphRunCount != 0 || s.Transparency != 0 {
		return false
	}
	if s.Corners != (shirei.Vec4{}) || s.Color1[3] < 1 || s.Color2[3] < 1 {
		return false
	}
	dr := b.devRect(s.Rect)
	return dr.Min.X <= 0 && dr.Min.Y <= 0 && dr.Max.X >= b.viewport.Max.X && dr.Max.Y >= b.viewport.Max.Y
}

func (b *builder) one(s *shirei.Surface) {
	if s.Transparency > 0 {
		b.alphaPrev = append(b.alphaPrev, b.alpha)
		b.alpha *= 1 - s.Transparency
	}
	if s.Clip == shirei.ClipPop {
		if len(b.clipStack) == 0 {
			panic("gpurender: uneven push/pop clip stack")
		}
		prev := b.clipStack[len(b.clipStack)-1]
		b.clipStack = b.clipStack[:len(b.clipStack)-1]
		b.clip = prev.scissor
		b.curRound = prev.round
		b.curShape = prev.shape
		b.curRadii = prev.radii
		b.clipGen++
	}

	if b.contentVisible(s) {
		b.emit(s)
	}

	if s.Clip == shirei.ClipPush {
		b.clipStack = append(b.clipStack, clipFrame{
			scissor: b.clip,
			shape:   b.curShape,
			radii:   b.curRadii,
			round:   b.curRound,
		})
		dr := b.devRect(s.Rect)
		b.clip = b.clip.Intersect(dr)
		b.curRound = s.Corners != (shirei.Vec4{})
		b.curShape = dr
		if b.curRound {
			b.curRadii = b.deviceRadii(dr, s.Corners)
		} else {
			b.curRadii = [4]float32{}
		}
		b.clipGen++
	}
	if s.PopTransparency {
		if len(b.alphaPrev) == 0 {
			panic("gpurender: uneven push/pop transparency stack")
		}
		b.alpha = b.alphaPrev[len(b.alphaPrev)-1]
		b.alphaPrev = b.alphaPrev[:len(b.alphaPrev)-1]
	}
}

func (b *builder) contentVisible(s *shirei.Surface) bool {
	dr := b.devRect(s.Rect)
	if dr.Min.X >= b.viewport.Max.X || dr.Min.Y >= b.viewport.Max.Y || dr.Max.X <= 0 || dr.Max.Y <= 0 {
		return false
	}
	switch {
	case s.GlyphRunCount > 0:
		return true
	case s.FontId > 0 && s.GlyphId > 0:
		return true
	case s.ImageId > 0:
		return true
	case s.Stroke > 0:
		return true
	case s.Color2 != s.Color1:
		return true
	default:
		return s.Color1[3] > 0
	}
}

func (b *builder) emit(s *shirei.Surface) {
	switch {
	case s.GlyphRunCount > 0:
		b.emitGlyphRun(s)
	case s.FontId > 0 && s.GlyphId > 0:
		g := shirei.GlyphRun{Rect: s.Rect, Color: s.Color1,
			FontId: s.FontId, GlyphId: s.GlyphId, GlyphOffset: s.GlyphOffset}
		b.emitGlyph(&g, b.glyphColor(g.Color))
	case s.ImageId > 0:
		b.emitImage(s)
	case s.Stroke > 0:
		b.emitBorder(s)
	default:
		b.emitFill(s)
	}
}

func (b *builder) emitFill(s *shirei.Surface) {
	c1 := shirei.HSLAColor(s.Color1)
	c2 := c1
	grad := s.Color2 != s.Color1
	if grad {
		c2 = shirei.HSLAColor(s.Color2)
	}
	dr := b.devRect(s.Rect)
	if s.Corners == (shirei.Vec4{}) {
		b.appendFillRect(dr, dr, c1, c2, grad)
		return
	}
	ni, nc := b.decompose(dr, s.Corners)
	for i := 0; i < ni; i++ {
		b.appendFillRect(b.interiorBuf[i], dr, c1, c2, grad)
	}
	for i := 0; i < nc; i++ {
		b.appendCorner(&b.cornerBuf[i], dr, c1, c2, grad, 0)
	}
}

func (b *builder) emitGlyphRun(s *shirei.Surface) {
	n := int(s.GlyphRunCount)
	var color, tint shirei.Vec4
	for i := 0; i < n; i++ {
		g := s.GlyphRunAt(i, b.glyphRuns)
		// Consecutive glyphs commonly share a color, including uniform runs.
		if i == 0 || g.Color != color {
			color = g.Color
			tint = b.glyphColor(color)
		}
		b.emitGlyph(&g, tint)
	}
}

func (b *builder) glyphColor(hsla shirei.Vec4) shirei.Vec4 {
	c := shirei.HSLAColor(hsla)
	return shirei.Vec4{float32(c.R) / 255, float32(c.G) / 255,
		float32(c.B) / 255, float32(c.A) / 255 * b.alpha}
}

func (b *builder) emitGlyph(g *shirei.GlyphRun, tint shirei.Vec4) {
	px := int(g.Rect.Size[1]*b.scale + 0.5)
	if px < 1 || px > 65535 {
		return
	}
	key := shirei.GlyphKey{FontId: g.FontId, GlyphId: g.GlyphId, Px: uint16(px)}
	bm, ok := shirei.GlyphBitmap(key)
	if !ok || bm.W == 0 || bm.H == 0 {
		return
	}
	penX := (g.Rect.Origin[0] + g.GlyphOffset[0]) * b.scale
	penY := (g.Rect.Origin[1] + g.Rect.Size[1]*0.82 + g.GlyphOffset[1]) * b.scale
	x0 := int(shirei.Roundf32(penX + bm.OffX))
	y0 := int(shirei.Roundf32(penY + bm.OffY))
	dr := image.Rect(x0, y0, x0+bm.W, y0+bm.H)
	// Test bitmap bounds, not the advance box: italic and combining glyphs
	// can paint beyond their nominal cell. The clip already includes the viewport.
	if !dr.Overlaps(b.clip) {
		return
	}

	if len(bm.RGBA) > 0 {
		slot, ok := packColorGlyph(key, bm)
		if !ok {
			gpu.failed = true
			return
		}
		a := b.alpha
		b.appendQuad(Quad{
			X: float32(dr.Min.X), Y: float32(dr.Min.Y),
			W: float32(dr.Dx()), H: float32(dr.Dy()),
			U: slot.u, V: slot.v, UW: slot.uw, VH: slot.vh,
			R: 1, G: 1, B: 1, A: a,
			R2: 1, G2: 1, B2: 1, A2: a,
		}, texColorGlyph, 0)
		return
	}
	if len(bm.Alpha) == 0 {
		return
	}
	slot, ok := packGlyph(key, bm)
	if !ok {
		gpu.failed = true
		return
	}
	b.appendQuad(Quad{
		X: float32(dr.Min.X), Y: float32(dr.Min.Y),
		W: float32(dr.Dx()), H: float32(dr.Dy()),
		U: slot.u, V: slot.v, UW: slot.uw, VH: slot.vh,
		R: tint[0], G: tint[1], B: tint[2], A: tint[3],
		R2: tint[0], G2: tint[1], B2: tint[2], A2: tint[3],
	}, texGlyph, 0)
}

func (b *builder) emitImage(s *shirei.Surface) {
	imgData := shirei.LookupImage(s.ImageId)
	if imgData == nil {
		return
	}
	iw, ih := int(imgData.Config.Width), int(imgData.Config.Height)
	if ext := imgData.External; ext != nil {
		if ext.Width > 0 && ext.Height > 0 {
			iw, ih = ext.Width, ext.Height
		}
	}
	if iw == 0 || ih == 0 {
		ib := imgData.RGBA.Bounds()
		iw, ih = ib.Dx(), ib.Dy()
	}
	if iw <= 0 || ih <= 0 {
		return
	}

	drawn := false
	if ext := imgData.External; ext != nil && gpuExternalImage != nil {
		if m, ok := gpu.image[s.ImageId]; ok && m.external && m.gen == imgData.Generation && m.w == iw && m.h == ih {
			drawn = true
		} else if gpuExternalImage(uint32(s.ImageId), ext, iw, ih) {
			gpu.image[s.ImageId] = gpuImg{gen: imgData.Generation, w: iw, h: ih, external: true}
			drawn = true
		}
	}
	if !drawn {
		src := &imgData.RGBA
		if len(src.Pix) == 0 {
			return
		}
		if !ensureImage(s.ImageId, imgData) {
			gpu.failed = true
			return
		}
	}

	dwl, dhl := s.Rect.Size[0], s.Rect.Size[1]
	if s.ImageScale {
		fit := s.Rect.Size[1] / float32(ih)
		dwl, dhl = float32(iw)*fit, float32(ih)*fit
	}
	x0 := int(shirei.Roundf32(s.Rect.Origin[0] * b.scale))
	y0 := int(shirei.Roundf32(s.Rect.Origin[1] * b.scale))
	dw := int(shirei.Roundf32(dwl * b.scale))
	dh := int(shirei.Roundf32(dhl * b.scale))
	if dw <= 0 || dh <= 0 {
		return
	}
	// Pre-scaled ImageScale stamps may land 1px off; snap to the bitmap.
	// Shadows size the quad from the card rect, not the stamp pixels.
	if s.ImageScale && absInt(dw-iw) <= 1 && absInt(dh-ih) <= 1 {
		dw, dh = iw, ih
	}
	a := b.alpha
	b.appendQuad(Quad{
		X: float32(x0), Y: float32(y0),
		W: float32(dw), H: float32(dh),
		U: 0, V: 0, UW: 1, VH: 1,
		R: 1, G: 1, B: 1, A: a,
		R2: 1, G2: 1, B2: 1, A2: a,
	}, texImage, int32(s.ImageId))
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (b *builder) emitBorder(s *shirei.Surface) {
	c := shirei.HSLAColor(s.Color1)
	dr := b.devRect(s.Rect)
	si := int(s.Stroke*b.scale + 0.5)
	if si < 1 {
		si = 1
	}
	ox0, oy0, ox1, oy1 := dr.Min.X, dr.Min.Y, dr.Max.X, dr.Max.Y
	if s.Corners == (shirei.Vec4{}) {
		ix0, iy0 := ox0+si, oy0+si
		ix1, iy1 := ox1-si, oy1-si
		if ix1 <= ix0 || iy1 <= iy0 {
			b.appendFillRect(image.Rect(ox0, oy0, ox1, oy1), dr, c, c, false)
			return
		}
		b.appendFillRect(image.Rect(ox0, oy0, ox1, iy0), dr, c, c, false)
		b.appendFillRect(image.Rect(ox0, iy1, ox1, oy1), dr, c, c, false)
		b.appendFillRect(image.Rect(ox0, iy0, ix0, iy1), dr, c, c, false)
		b.appendFillRect(image.Rect(ix1, iy0, ox1, iy1), dr, c, c, false)
		return
	}

	maxr := float32(min(ox1-ox0, oy1-oy0)) / 2
	rd := func(v float32) int {
		d := int(v*b.scale + 0.5)
		if float32(d) > maxr {
			d = int(maxr)
		}
		if d < 0 {
			d = 0
		}
		return d
	}
	tl, tr, br, bl := rd(s.Corners[0]), rd(s.Corners[1]), rd(s.Corners[2]), rd(s.Corners[3])
	tlcx, tlcy := ox0+tl, oy0+tl
	trcx, trcy := ox1-tr, oy0+tr
	brcx, brcy := ox1-br, oy1-br
	blcx, blcy := ox0+bl, oy1-bl
	b.appendFillRect(image.Rect(tlcx, oy0, trcx, oy0+si), dr, c, c, false)
	b.appendFillRect(image.Rect(blcx, oy1-si, brcx, oy1), dr, c, c, false)
	b.appendFillRect(image.Rect(ox0, tlcy, ox0+si, blcy), dr, c, c, false)
	b.appendFillRect(image.Rect(ox1-si, trcy, ox1, brcy), dr, c, c, false)

	ring := func(cx, cy, rad int, fh, fv bool) {
		if rad <= 0 {
			return
		}
		slot, ok := packCorner(rad, si)
		if !ok {
			gpu.failed = true
			return
		}
		n := slot.w
		var box image.Rectangle
		switch {
		case !fh && !fv:
			box = image.Rect(cx-n, cy-n, cx, cy)
		case fh && !fv:
			box = image.Rect(cx, cy-n, cx+n, cy)
		case fh && fv:
			box = image.Rect(cx, cy, cx+n, cy+n)
		default:
			box = image.Rect(cx-n, cy, cx, cy+n)
		}
		b.appendCoverage(box, slot, fh, fv, c, c)
	}
	ring(tlcx, tlcy, tl, false, false)
	ring(trcx, trcy, tr, true, false)
	ring(brcx, brcy, br, true, true)
	ring(blcx, blcy, bl, false, true)
}

func (b *builder) decompose(dr image.Rectangle, corners shirei.Vec4) (ni, nc int) {
	maxr := float32(min(dr.Dx(), dr.Dy())) / 2
	rd := func(v float32) int {
		d := int(v*b.scale + 0.5)
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
	add := func(xA, yA, xB, yB int) {
		if xB > xA && yB > yA {
			b.interiorBuf[ni] = image.Rect(xA, yA, xB, yB)
			ni++
		}
	}
	yTop := y0 + max(tl, tr)
	yBot := y1 - max(bl, br)
	add(x0, yTop, x1, yBot)
	mt := min(tl, tr)
	add(x0+tl, y0, x1-tr, y0+mt)
	if tl > tr {
		add(x0+tl, y0+mt, x1, yTop)
	} else if tr > tl {
		add(x0, y0+mt, x1-tr, yTop)
	}
	mb := min(bl, br)
	add(x0+bl, y1-mb, x1-br, y1)
	if bl > br {
		add(x0+bl, yBot, x1, y1-mb)
	} else if br > bl {
		add(x0, yBot, x1-br, y1-mb)
	}
	addCorner := func(rect image.Rectangle, rad int, fh, fv bool) {
		if rad > 0 {
			b.cornerBuf[nc] = cornerPlace{rect, rad, fh, fv}
			nc++
		}
	}
	addCorner(image.Rect(x0, y0, x0+tl, y0+tl), tl, false, false)
	addCorner(image.Rect(x1-tr, y0, x1, y0+tr), tr, true, false)
	addCorner(image.Rect(x1-br, y1-br, x1, y1), br, true, true)
	addCorner(image.Rect(x0, y1-bl, x0+bl, y1), bl, false, true)
	return ni, nc
}

func (b *builder) appendCorner(cp *cornerPlace, full image.Rectangle, c1, c2 color.NRGBA, grad bool, stroke int) {
	slot, ok := packCorner(cp.rad, stroke)
	if !ok {
		gpu.failed = true
		return
	}
	top, bot := c1, c2
	if grad {
		top, bot = bandColors(full, cp.rect.Min.Y, cp.rect.Max.Y, c1, c2)
	}
	b.appendCoverage(cp.rect, slot, cp.flipH, cp.flipV, top, bot)
}

func (b *builder) appendCoverage(box image.Rectangle, slot atlasSlot, fh, fv bool, c1, c2 color.NRGBA) {
	if box.Empty() {
		return
	}
	u, v, uw, vh := flipUV(slot, fh, fv)
	a := b.alpha
	q := Quad{
		X: float32(box.Min.X), Y: float32(box.Min.Y),
		W: float32(box.Dx()), H: float32(box.Dy()),
		U: u, V: v, UW: uw, VH: vh,
		R: float32(c1.R) / 255, G: float32(c1.G) / 255, B: float32(c1.B) / 255, A: float32(c1.A) / 255 * a,
		R2: float32(c2.R) / 255, G2: float32(c2.G) / 255, B2: float32(c2.B) / 255, A2: float32(c2.A) / 255 * a,
	}
	if q.A == 0 && q.A2 == 0 {
		return
	}
	b.appendQuad(q, texGlyph, 0)
}

func (b *builder) appendFillRect(dr, full image.Rectangle, c1, c2 color.NRGBA, grad bool) {
	if dr.Empty() {
		return
	}
	if grad {
		c1, c2 = bandColors(full, dr.Min.Y, dr.Max.Y, c1, c2)
	}
	a := b.alpha
	q := Quad{
		X: float32(dr.Min.X), Y: float32(dr.Min.Y),
		W: float32(dr.Dx()), H: float32(dr.Dy()),
		R: float32(c1.R) / 255, G: float32(c1.G) / 255, B: float32(c1.B) / 255, A: float32(c1.A) / 255 * a,
		R2: float32(c2.R) / 255, G2: float32(c2.G) / 255, B2: float32(c2.B) / 255, A2: float32(c2.A) / 255 * a,
	}
	if q.A == 0 && q.A2 == 0 {
		return
	}
	b.appendQuad(q, texFill, 0)
}

func bandColors(full image.Rectangle, y0, y1 int, c1, c2 color.NRGBA) (color.NRGBA, color.NRGBA) {
	fh := float32(full.Dy())
	if fh <= 0 {
		return c1, c2
	}
	t0 := float32(y0-full.Min.Y) / fh
	t1 := float32(y1-full.Min.Y) / fh
	return lerpNRGBA(c1, c2, t0), lerpNRGBA(c1, c2, t1)
}

func lerpNRGBA(c1, c2 color.NRGBA, t float32) color.NRGBA {
	if t <= 0 {
		return c1
	}
	if t >= 1 {
		return c2
	}
	return color.NRGBA{
		R: lerp8(c1.R, c2.R, t),
		G: lerp8(c1.G, c2.G, t),
		B: lerp8(c1.B, c2.B, t),
		A: lerp8(c1.A, c2.A, t),
	}
}

func lerp8(a, b uint8, t float32) uint8 {
	return uint8(float32(a) + (float32(b)-float32(a))*t + 0.5)
}

func (b *builder) deviceRadii(shape image.Rectangle, corners shirei.Vec4) [4]float32 {
	maxr := float32(min(shape.Dx(), shape.Dy())) / 2
	rd := func(v float32) float32 {
		d := float32(int(v*b.scale + 0.5))
		if d > maxr {
			d = maxr
		}
		if d < 0 {
			d = 0
		}
		return d
	}
	return [4]float32{rd(corners[0]), rd(corners[1]), rd(corners[2]), rd(corners[3])}
}

func (b *builder) gatherClips() (n int32, rects, rads [4][4]float32) {
	add := func(round bool, shape image.Rectangle, radii [4]float32) {
		if !round || n >= 4 || shape.Empty() {
			return
		}
		rects[n] = [4]float32{float32(shape.Min.X), float32(shape.Min.Y), float32(shape.Dx()), float32(shape.Dy())}
		rads[n] = radii
		n++
	}
	for i := range b.clipStack {
		f := b.clipStack[i]
		add(f.round, f.shape, f.radii)
	}
	add(b.curRound, b.curShape, b.curRadii)
	return
}

func (b *builder) appendQuad(q Quad, kind, key int32) {
	sc := b.clip.Intersect(b.viewport)
	if sc.Empty() {
		return
	}
	n := int32(len(b.quads))
	cx, cy := int32(sc.Min.X), int32(sc.Min.Y)
	cw, ch := int32(sc.Dx()), int32(sc.Dy())
	if len(b.batches) > 0 {
		last := &b.batches[len(b.batches)-1]
		if last.ClipX == cx && last.ClipY == cy && last.ClipW == cw && last.ClipH == ch &&
			last.TexKind == kind && last.TexKey == key && last.ClipGen == b.clipGen {
			b.quads = append(b.quads, q)
			last.Count++
			return
		}
	}
	cn, rects, rads := b.gatherClips()
	b.quads = append(b.quads, q)
	b.batches = append(b.batches, Batch{
		First: n, Count: 1,
		ClipX: cx, ClipY: cy, ClipW: cw, ClipH: ch,
		TexKind: kind, TexKey: key,
		ClipN: cn, ClipGen: b.clipGen,
		ClipRect: rects, ClipRad: rads,
	})
}
