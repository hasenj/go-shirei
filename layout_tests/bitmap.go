// Package layout_tests contains snapshot tests for the shirei layout engine.
//
// Each test renders a frame into an in-memory bitmap and compares it pixel by
// pixel against a saved PNG snapshot in testdata/. The bitmap renderer in this
// file is support code local to these tests: it only renders rectangles with
// solid colors — no text glyphs, no images, no gradients, no rounded corners.
// Glyph surfaces show up as solid boxes in the text color.
package layout_tests

import (
	"image"
	"image/color"

	"go.hasen.dev/shirei"
)

// renderFrame runs a shirei frame at the given window size and rasterizes
// the resulting surfaces into an RGBA image with a white background.
//
// The frame function is executed twice, and the second frame is the one
// rasterized: shirei resolves queries like GetAvailableSize, GetResolvedRectOf
// and scroll offset clamping against previous-frame layout data, so layouts
// using them produce a degenerate first frame and settle on the second. For
// static layouts both frames are identical.
//
// The scope must be unique per test. Frames rendered earlier in the same
// process leave behind per-container data (scroll offsets, resolved sizes)
// keyed by deterministic ids, so without a unique id namespace one test's
// state would bleed into the next.
func renderFrame(scope string, width int, height int, frameFn shirei.FrameFn) *image.RGBA {
	shirei.GetHost().WindowSize = shirei.Vec2{float32(width), float32(height)}
	scopeId := boxedScopeId(scope)
	var frameData shirei.FrameOutputData
	for range 2 {
		frameData = shirei.RunFrameFn(func() {
			// animations also interpolate from previous frame data; NoAnimate
			// on the root cascades down the whole tree
			shirei.ModAttrs(func(a *shirei.AttrSet) { a.Animations = 0 })
			// a bare container does not affect pixel output, but namespaces
			// all the auto-generated container ids inside
			shirei.ContainerWithKey(scopeId, shirei.AttrSet{}, frameFn)
		})
	}
	return renderSurfaces(width, height, frameData.Surfaces)
}

var boxedScopeIds = make(map[string]any)

// boxedScopeId interns the scope string as an `any` value, so the same boxed
// interface value is reused every frame. shirei derives the auto-generated
// ids of child containers by hashing the raw interface bytes of the parent id
// (pointers included), so re-boxing the string each frame would change every
// id inside, and previous-frame data lookups would always miss.
func boxedScopeId(scope string) any {
	id, ok := boxedScopeIds[scope]
	if !ok {
		id = scope
		boxedScopeIds[scope] = id
	}
	return id
}

func renderSurfaces(width int, height int, surfaces []shirei.Surface) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// opaque white canvas
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}

	var clipStack []image.Rectangle
	clip := img.Bounds()

	var alphaStack []float32
	var alpha float32 = 1

	for _, s := range surfaces {
		if s.Transparency > 0 {
			alphaStack = append(alphaStack, alpha)
			alpha *= 1 - s.Transparency
		}

		rect := pixelRect(s.Rect)

		if s.Clip == shirei.ClipPush {
			clipStack = append(clipStack, clip)
			clip = clip.Intersect(rect)
		}

		c := shirei.HSLAColor(s.Color1)
		c.A = uint8(float32(c.A) * alpha)

		if s.Stroke == 0 {
			blendFill(img, rect.Intersect(clip), c)
		} else {
			// the border is drawn inside the rectangle
			w := int(shirei.Roundf32(s.Stroke))
			inner := rect.Inset(w)
			if inner.Empty() {
				blendFill(img, rect.Intersect(clip), c)
			} else {
				edges := [4]image.Rectangle{
					{Min: rect.Min, Max: image.Point{rect.Max.X, inner.Min.Y}},                              // top
					{Min: image.Point{rect.Min.X, inner.Max.Y}, Max: rect.Max},                              // bottom
					{Min: image.Point{rect.Min.X, inner.Min.Y}, Max: image.Point{inner.Min.X, inner.Max.Y}}, // left
					{Min: image.Point{inner.Max.X, inner.Min.Y}, Max: image.Point{rect.Max.X, inner.Max.Y}}, // right
				}
				for _, e := range edges {
					blendFill(img, e.Intersect(clip), c)
				}
			}
		}

		if s.PopTransparency {
			if len(alphaStack) == 0 {
				panic("surface rendering: uneven push/pop opacity stack")
			}
			alpha = alphaStack[len(alphaStack)-1]
			alphaStack = alphaStack[:len(alphaStack)-1]
		}

		if s.Clip == shirei.ClipPop {
			if len(clipStack) == 0 {
				panic("surface rendering: uneven push/pop clip stack")
			}
			clip = clipStack[len(clipStack)-1]
			clipStack = clipStack[:len(clipStack)-1]
		}
	}

	return img
}

func pixelRect(r shirei.Rect) image.Rectangle {
	x0 := int(shirei.Roundf32(r.Origin[0]))
	y0 := int(shirei.Roundf32(r.Origin[1]))
	x1 := int(shirei.Roundf32(r.Origin[0] + r.Size[0]))
	y1 := int(shirei.Roundf32(r.Origin[1] + r.Size[1]))
	return image.Rect(x0, y0, x1, y1)
}

// fill the rectangle with the color, src-over blended
func blendFill(img *image.RGBA, r image.Rectangle, c color.NRGBA) {
	r = r.Intersect(img.Bounds())
	if c.A == 0 || r.Empty() {
		return
	}

	sa := uint32(c.A)

	// premultiplied source channels
	sr := uint32(c.R) * sa / 255
	sg := uint32(c.G) * sa / 255
	sb := uint32(c.B) * sa / 255

	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := img.PixOffset(r.Min.X, y)
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.Pix[i : i+4 : i+4]
			if sa == 255 {
				p[0] = c.R
				p[1] = c.G
				p[2] = c.B
				p[3] = 0xff
			} else {
				// image.RGBA is premultiplied, so blend in premultiplied space
				p[0] = uint8(sr + uint32(p[0])*(255-sa)/255)
				p[1] = uint8(sg + uint32(p[1])*(255-sa)/255)
				p[2] = uint8(sb + uint32(p[2])*(255-sa)/255)
				p[3] = uint8(sa + uint32(p[3])*(255-sa)/255)
			}
			i += 4
		}
	}
}
