package shirei

import (
	"image"

	"golang.org/x/image/vector"
)

// Corner-coverage cache (phase 2 of the software renderer). The expensive curved
// work in a rounded rect is at the corners; the interiors and edges are plain
// fills. So we cache the *corner*, not the whole shape: a quarter-disk alpha mask
// keyed by device-px radius (fills) or by radius + stroke (borders). A design
// system uses a handful of radii, so a few cached masks are reused across thousands
// of surfaces — the same hit-rate story as the glyph cache (glyphcache.go), and the
// same idea: cache device-px-keyed coverage, apply color per pixel.
//
// Coverage is cached, not color, so one mask composes with solid fills, gradients,
// and the clip mask alike. The canonical mask is the top-left orientation; the
// other three corners read it flipped (free), so the cache holds one entry per
// radius regardless of which corner uses it.
//
// Single-threaded use only (frame production and painting both run on one thread,
// like the glyph cache). Maps are cleared wholesale past a soft cap so an animated
// corner radius (a new radius per frame) cannot grow them without bound.

const cornerCacheCap = 512

// cornerMask is a cached corner coverage mask plus per-row spans. The spans let the
// blend skip the empty region and straight-fill the fully-covered run, blending
// only the thin antialiased arc (see drawCorner). All coords are canonical
// top-left orientation; callers apply free H/V flips. For every row j (0..dim):
//
//	[nzLo,nzHi)  nonzero coverage (cov > 0)   — outside it, skip
//	[fLo,fHi)    full coverage   (cov == 255) — straight-fill / constant-blend
//
// nzLo/fLo default to dim and nzHi/fHi to 0 for an empty row/run. fLo/fHi is only
// set when the 255 run is contiguous (it always is for a quarter disk or ring); a
// non-contiguous row disables the fast full run for that row (safe fallback).
type cornerMask struct {
	pix        []byte // coverage, stride == dim
	dim        int
	nzLo, nzHi []int16
	fLo, fHi   []int16
}

func newCornerMask(pix []byte, dim int) *cornerMask {
	cm := &cornerMask{
		pix: pix, dim: dim,
		nzLo: make([]int16, dim), nzHi: make([]int16, dim),
		fLo: make([]int16, dim), fHi: make([]int16, dim),
	}
	for j := 0; j < dim; j++ {
		row := pix[j*dim : j*dim+dim]
		lo, hi, flo, fhi, n255 := dim, 0, dim, 0, 0
		for i := 0; i < dim; i++ {
			v := row[i]
			if v == 0 {
				continue
			}
			if i < lo {
				lo = i
			}
			hi = i + 1
			if v == 255 {
				if i < flo {
					flo = i
				}
				fhi = i + 1
				n255++
			}
		}
		if fhi-flo != n255 { // 255 run not contiguous: no fast full run this row
			flo, fhi = dim, 0
		}
		cm.nzLo[j], cm.nzHi[j] = int16(lo), int16(hi)
		cm.fLo[j], cm.fHi[j] = int16(flo), int16(fhi)
	}
	return cm
}

var fillCornerCache = map[uint16]*cornerMask{}

// fillCornerMask returns the cached quarter-disk corner mask for a fill corner of
// the given device-px radius (canonical top-left, dim == rad). nil for rad <= 0.
func fillCornerMask(rad int) *cornerMask {
	if rad <= 0 || rad > 65535 {
		return nil
	}
	key := uint16(rad)
	if m, ok := fillCornerCache[key]; ok {
		return m
	}
	if len(fillCornerCache) >= cornerCacheCap {
		fillCornerCache = map[uint16]*cornerMask{}
	}
	m := newCornerMask(quarterDiskAt(rad, rad, float32(rad), float32(rad), float32(rad)), rad)
	fillCornerCache[key] = m
	return m
}

type borderCornerKey struct{ rad, stroke uint16 }

var borderCornerCache = map[borderCornerKey]*cornerMask{}

// borderCornerMask returns the cached coverage-ring corner mask for a border: the
// area between the outer (radius rad+stroke/2) and inner (radius rad-stroke/2) arcs
// of a stroke centered on a corner of device-px radius rad. dim is the box side;
// nil if the corner is degenerate.
func borderCornerMask(rad, stroke int) *cornerMask {
	if rad <= 0 || stroke <= 0 || rad > 65535 || stroke > 65535 {
		return nil
	}
	hs := float32(stroke) / 2
	ro := float32(rad) + hs
	n := int(ro + 0.999) // ceil: the box just contains the outer arc
	if n <= 0 {
		return nil
	}
	key := borderCornerKey{uint16(rad), uint16(stroke)}
	if m, ok := borderCornerCache[key]; ok {
		return m
	}
	if len(borderCornerCache) >= cornerCacheCap {
		borderCornerCache = map[borderCornerKey]*cornerMask{}
	}
	fn := float32(n)
	outer := quarterDiskAt(n, n, fn, fn, ro) // centered at the box's bottom-right
	ring := outer
	if ri := float32(rad) - hs; ri > 0 {
		inner := quarterDiskAt(n, n, fn, fn, ri)
		ring = make([]byte, len(outer))
		for i := range outer {
			if d := int(outer[i]) - int(inner[i]); d > 0 {
				ring[i] = byte(d)
			}
		}
	}
	m := newCornerMask(ring, n)
	borderCornerCache[key] = m
	return m
}

// quarterDiskAt rasterizes a quarter disk of the given radius centered at (cx,cy),
// covering the top-left quadrant (x ≤ cx, y ≤ cy) within radius of the center, into
// a w×h alpha buffer (stride == w). Its two straight sides (the right edge x=cx and
// bottom edge y=cy) are full coverage, so the mask blends seamlessly into the
// adjacent straight fill; the arc is antialiased. Used for both fill corners
// (center at the buffer's bottom-right) and the outer/inner arcs of border rings.
func quarterDiskAt(w, h int, cx, cy, radius float32) []byte {
	if w <= 0 || h <= 0 || radius <= 0 {
		return nil
	}
	const k = 0.5522847498307936 // 4/3 * (sqrt(2)-1): cubic approx of a quarter circle
	ras := vector.NewRasterizer(w, h)
	ras.MoveTo(cx, cy-radius)
	ras.LineTo(cx, cy)
	ras.LineTo(cx-radius, cy)
	ras.CubeTo(cx-radius, cy-k*radius, cx-k*radius, cy-radius, cx, cy-radius) // arc back to start
	ras.ClosePath()
	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	ras.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})
	return dst.Pix
}
