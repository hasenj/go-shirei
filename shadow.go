package shirei

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"runtime"
	"sync"

	"golang.org/x/image/vector"
)

// ShadowMapKey is the cache key for a generated blur shadow. It is comparable
// so it can sit in the shared imageKeys map alongside string path/app keys
// (map[any] keeps the types distinct — no collision with paths).
type ShadowMapKey struct {
	w  int
	h  int
	c0 uint8
	c1 uint8
	c2 uint8
	c3 uint8
	r  uint8
	a  uint8
}

// returns an image handle via the shared image registry (getOrPutImage).
func _IMBlurShadow(size Vec2, corners Vec4, radius float32, alpha float32) ImageId {
	params := ShadowMapKey{
		w:  int(Roundf32(size[0])),
		h:  int(Roundf32(size[1])),
		c0: uint8(corners[0]),
		c1: uint8(corners[1]),
		c2: uint8(corners[2]),
		c3: uint8(corners[3]),
		r:  uint8(radius * 10),
		a:  uint8(alpha * 0xff),
	}
	id := getOrPutImage(params, func() *ImageData {
		return _GenerateBlurShadow(size, corners, radius, alpha)
	})
	// Stamp pixel size must match this card. A hit with a different bitmap
	// (recycled ImageId, stale slot) is replaced. Dimensions use the same
	// Roundf32(size+blur pad) as generate.
	wantW, wantH := shadowStampPx(size, radius)
	if img := LookupImage(id); img != nil {
		b := img.RGBA.Bounds()
		if b.Dx() == wantW && b.Dy() == wantH {
			return id
		}
	}
	return putImage(params, _GenerateBlurShadow(size, corners, radius, alpha))
}

func shadowStampPx(size Vec2, radius float32) (w, h int) {
	w = int(Roundf32(size[0] + radius*4))
	h = int(Roundf32(size[1] + radius*4))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

func _GenerateBlurShadow(size Vec2, corners Vec4, radius float32, alpha float32) *ImageData {
	width, height := shadowStampPx(size, radius)
	rect := rasterShadow(size, corners, radius, alpha, width, height)

	var image = new(ImageData)
	image.Config.ColorModel = rect.ColorModel()
	image.Config.Width = rect.Rect.Dx()
	image.Config.Height = rect.Rect.Dy()

	if radius <= 0 {
		image.RGBA = *rect
	} else {
		image.RGBA = *blurAlpha(rect, float64(radius))
	}

	return image
}

// rasterShadow fills the card's rounded rect, inset by the blur padding, in a width x height image.
func rasterShadow(size Vec2, corners Vec4, radius float32, alpha float32, width, height int) *image.RGBA {
	var rect = image.NewRGBA(image.Rect(0, 0, width, height))

	var p = vector.NewRasterizer(width, height)
	p.DrawOp = draw.Over
	// based on gio/op/clip/shapes.go
	// based on https://pomax.github.io/bezierinfo/#circles_cubic
	const q = 4 * (math.Sqrt2 - 1) / 3
	const iq = 1 - q
	// corners order: top-left | top-right | bottom-right | bottom-left
	// in other words: nw, ne, sw, se
	nw := corners[0]
	ne := corners[1]
	sw := corners[2]
	se := corners[3]
	// draw the rect at a location so that we can blur with the given radius!
	w := radius * 2
	n := radius * 2
	e := w + size[0]
	s := n + size[1]
	p.MoveTo(w+nw, n)
	p.LineTo(e-ne, n) // N
	p.CubeTo(         // NE
		e-ne*iq, n,
		e, n+ne*iq,
		e, n+ne)
	p.LineTo(e, s-se) // E
	p.CubeTo(         // SE
		e, s-se*iq,
		e-se*iq, s,
		e-se, s)
	p.LineTo(w+sw, s) // S
	p.CubeTo(         // SW
		w+sw*iq, s,
		w, s-sw*iq,
		w, s-sw)
	p.LineTo(w, n+nw) // W
	p.CubeTo(         // NW
		w, n+nw*iq,
		w+nw*iq, n,
		w+nw, n)
	p.ClosePath()

	src := image.NewUniform(color.RGBA{0, 0, 0, uint8(alpha * 0xff)})

	p.Draw(rect, rect.Bounds(), src, image.Point{})
	return rect
}

// blurAlpha gives what bild's blur.Gaussian gives for a black image: the same
// kernel, edge extension and rounding, applied to alpha only. A card's shadow
// is flat except near its edges, so a pixel whose window is flat copies a
// precomputed value instead of convolving; that turns a ~250 ms blur of a
// large card into a few ms.
func blurAlpha(img *image.RGBA, radius float64) *image.RGBA {
	n := int(math.Ceil(2*radius + 1))
	k := make([]float64, n)
	sum := 0.0
	for i, x := 0, -radius; i < n; i, x = i+1, x+1 {
		k[i] = math.Exp(-(x * x / 4 / radius))
		sum += math.Abs(k[i])
	}
	for i := range k {
		k[i] /= sum
	}
	var flat [256]uint8 // a flat window of value v, summed in bild's order
	for v := range flat {
		acc := 0.0
		for _, kv := range k {
			acc += float64(v) * kv
		}
		flat[v] = uint8(math.Max(math.Min(acc, 255), 0))
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	a := make([]uint8, w*h)
	for y := range h {
		for x := range w {
			a[y*w+x] = img.Pix[y*img.Stride+x*4+3]
		}
	}
	tmp := make([]uint8, w*h)
	blurLines(a, tmp, h, w, 1, w, k, &flat) // rows
	blurLines(tmp, a, w, h, w, 1, k, &flat) // columns
	out := image.NewRGBA(img.Rect)
	for y := range h {
		for x := range w {
			out.Pix[y*out.Stride+x*4+3] = a[y*w+x]
		}
	}
	return out
}

// blurLines convolves count lines of length size, element i of line j at
// j*lineStep+i*step, from src into dst. Lines are split across the CPUs.
func blurLines(src, dst []uint8, count, size, step, lineStep int, k []float64, flat *[256]uint8) {
	workers := min(runtime.GOMAXPROCS(0), max(1, count/64))
	var wg sync.WaitGroup
	for wk := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			blurSome(src, dst, count*wk/workers, count*(wk+1)/workers, size, step, lineStep, k, flat)
		}()
	}
	wg.Wait()
}

func blurSome(src, dst []uint8, from, to, size, step, lineStep int, k []float64, flat *[256]uint8) {
	n, rk := len(k), len(k)/2
	changes := make([]int32, size) // changes[i]: how many neighbours differ up to i
	line := make([]uint8, size)
	for j := from; j < to; j++ {
		base := j * lineStep
		for i := range size {
			line[i] = src[base+i*step]
			changes[i] = 0
			if i > 0 {
				changes[i] = changes[i-1]
				if line[i] != line[i-1] {
					changes[i]++
				}
			}
		}
		for i := range size {
			lo, hi := i-rk, i-rk+n-1
			if changes[min(max(hi, 0), size-1)] == changes[min(max(lo, 0), size-1)] {
				dst[base+i*step] = flat[line[min(max(lo, 0), size-1)]]
				continue
			}
			acc := 0.0
			if lo >= 0 && hi < size {
				for t, v := range line[lo : hi+1] {
					acc += float64(v) * k[t]
				}
			} else {
				for t := range n {
					acc += float64(line[min(max(lo+t, 0), size-1)]) * k[t]
				}
			}
			dst[base+i*step] = uint8(math.Max(math.Min(acc, 255), 0))
		}
	}
}
