package shirei

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/anthonynsimon/bild/blur"
	"golang.org/x/image/vector"
)

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

var _shadowsMap = make(map[ShadowMapKey]ImageId)

// returns an image handle!
func _IMBlurShadow(size Vec2, corners Vec4, radius float32, alpha float32) ImageId {
	var params = ShadowMapKey{
		w:  int(size[0]),
		h:  int(size[1]),
		c0: uint8(corners[0]),
		c1: uint8(corners[1]),
		c2: uint8(corners[2]),
		c3: uint8(corners[3]),
		r:  uint8(radius * 10),
		a:  uint8(alpha * 0xff),
	}
	imageId, ok := _shadowsMap[params]
	if ok {
		return imageId
	} else {
		// fmt.Println("Generating shadow:", params) // DEBUG!
		img := _GenerateBlurShadow(size, corners, radius, alpha)
		imageId = ImageId(len(imageIds))
		imageIds = append(imageIds, img)
		_shadowsMap[params] = imageId
		return imageId
	}
}

func _GenerateBlurShadow(size Vec2, corners Vec4, radius float32, alpha float32) *ImageData {
	// the size is the size of the rect plus space for the blurring radius!
	width := size[0] + radius*4
	height := size[1] + radius*4
	// fmt.Println("Size:", size, "Blur:", radius, "width, height:", width, height)

	var rect = image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	// fmt.Println("image width:", rect.Bounds().Dx())

	var p = vector.NewRasterizer(int(width), int(height))
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

	var image = new(ImageData)
	image.Config.ColorModel = rect.ColorModel()
	image.Config.Width = rect.Rect.Dx()
	image.Config.Height = rect.Rect.Dy()

	if radius <= 0 {
		image.RGBA = *rect
	} else {
		image.RGBA = *blur.Gaussian(rect, float64(radius))
	}

	return image
}
