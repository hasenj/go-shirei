// color-picker: one shared HSLA color edited three ways — channel sliders
// whose tracks show the colors each slider can pick, a saturation×lightness
// grid at the current hue, and a hue/saturation wheel at the current
// lightness. Every control reads and writes the same value, so they all
// stay in sync.
//
// The gradient tracks, the grid, and the wheel are generated pixel buffers
// (UseImage/ImageView), content-keyed on the channel values they depend on:
// a control's image is rebuilt only when another channel moves it, and stale
// variants age out of the image registry on their own.
//
//	go run ./demos/color-picker
//	go run ./demos/color-picker --png out.png
package main

import (
	"fmt"
	"image"
	"math"
	"os"

	"go.hasen.dev/generic"
	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const (
	winW = 880
	winH = 600
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, root); err != nil {
			fmt.Println("render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Color Picker", winW, winH)
	app.Run(root)
}

// current is the one HSLA value every control below reads and writes.
// H 0..360, S 0..100, L 0..100, A 0..1.
var current = Vec4{210, 70, 55, 0.85}

func root() {
	ModAttrs(Viewport, Background(220, 10, 96, 1), Pad(24), Gap(16))
	ScrollOnInput()

	Label("Color picker", FontWeight(WeightBold), FontSize(18))
	Label("One HSLA value, three ways to edit it — every control reflects the others.",
		FontSize(13), TextColor(0, 0, 40, 1))

	Container(Attrs(Row, Gap(16)), func() {
		card("Channel sliders", "Tracks show what each slider picks.", slidersPanel)
		card("Saturation × Lightness", "The S/L plane at the current hue.", slGridPanel)
		card("Hue wheel", "Angle = hue, radius = saturation.", hueWheelPanel)
	})

	previewBar()
	ScrollBars()
}

func card(title, sub string, body func()) {
	Container(Attrs(Gap(12), Pad(14), Corners(10),
		Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 85, 1)), func() {
		Container(Attrs(Gap(2)), func() {
			Label(title, FontSize(14), FontWeight(WeightBold), TextColor(0, 0, 20, 1))
			Label(sub, FontSize(11), TextColor(0, 0, 50, 1))
		})
		body()
	})
}

// -----------------------------------------------------------------------------
//  Channel sliders
// -----------------------------------------------------------------------------

const (
	sliderW      f32 = 260
	sliderBoxH   f32 = 24
	sliderTrackH f32 = 16
	handleD      f32 = 20 // ring handle diameter
)

func slidersPanel() {
	channelSlider("Hue", HUE, 360, "%.0f")
	channelSlider("Saturation", SATURATION, 100, "%.0f")
	channelSlider("Lightness", LIGHT, 100, "%.0f")
	channelSlider("Alpha", ALPHA, 1, "%.2f")
}

func channelSlider(name string, channel int, max f32, valueFmt string) {
	Container(Attrs(Gap(3)), func() {
		Container(Attrs(Expand, Row, Gap(6)), func() {
			Label(name, FontSize(12), FontWeight(WeightSemibold), TextColor(0, 0, 30, 1))
			Element(Attrs(Grow(1)))
			Label(fmt.Sprintf(valueFmt, current[channel]), FontSize(12), TextColor(0, 0, 45, 1))
		})
		Container(Attrs(FixWidth(sliderW), FixHeight(sliderBoxH), Focusable), func() {
			st := ProcessSlider(&current[channel], SliderConfig{
				Min: 0, Max: max, Width: sliderW, HandleInset: handleD / 2,
			})
			trackY := (sliderBoxH - sliderTrackH) / 2
			Container(Attrs(Float(0, trackY), FixSize(sliderW, sliderTrackH),
				Corners(sliderTrackH/2), Clip, ClickThrough, NoAnimate), func() {
				ImageView(channelStrip(channel), Vec2{sliderW, sliderTrackH})
			})
			// Ring handle: the track gradient stays visible through the center.
			ring(st.HandleX+handleD/2, sliderBoxH/2, handleD-2)
		})
	})
}

// channelStrip returns the track image for one channel: the color the slider
// would pick at each x, holding the other channels at their current values.
// The gradient is aligned to the handle's travel; the end caps (inside
// HandleInset) pin to the extreme values, so the handle center always sits
// on the exact color it selects. The alpha track composites over a
// checkerboard; the H/S/L tracks are shown opaque.
func channelStrip(channel int) ImageId {
	q := quantized(current)
	q[channel] = -1 // the swept channel does not affect its own strip
	if channel != ALPHA {
		q[ALPHA] = -1 // H/S/L tracks are drawn opaque; alpha is irrelevant
	}
	key := fmt.Sprintf("color-picker/strip/%d/%d-%d-%d-%d", channel, q[0], q[1], q[2], q[3])
	if id := GetImageId(key); id != 0 {
		return id
	}

	w := int(sliderW) * imgScale
	h := int(sliderTrackH) * imgScale
	inset := int(handleD/2) * imgScale
	track := f32(w - 2*inset)
	spans := [4]f32{360, 100, 100, 1}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		t := (f32(x) + 0.5 - f32(inset)) / track
		generic.Clamp(0, &t, 1)
		c := current
		c[channel] = t * spans[channel]
		if channel != ALPHA {
			c[ALPHA] = 1
		}
		col := HSLAColor(c)
		for y := 0; y < h; y++ {
			r, g, b := col.R, col.G, col.B
			if channel == ALPHA {
				bg := checkerShade(x, y)
				a := f32(col.A) / 255
				r = mix8(bg, r, a)
				g = mix8(bg, g, a)
				b = mix8(bg, b, a)
			}
			i := img.PixOffset(x, y)
			img.Pix[i+0] = r
			img.Pix[i+1] = g
			img.Pix[i+2] = b
			img.Pix[i+3] = 0xff
		}
	}
	return UseImage(key, img)
}

// -----------------------------------------------------------------------------
//  Saturation × Lightness grid
// -----------------------------------------------------------------------------

const gridSize f32 = 200

func slGridPanel() {
	Container(Attrs(FixSize(gridSize, gridSize), Focusable, Clip, Corners(6), NoAnimate), func() {
		if p, ok := dragPoint(); ok {
			s := p[0] / gridSize * 100
			l := (1 - p[1]/gridSize) * 100
			generic.Clamp(0, &s, 100)
			generic.Clamp(0, &l, 100)
			current[SATURATION] = s
			current[LIGHT] = l
		}
		ImageView(slGridImage(), Vec2{gridSize, gridSize})
		marker(current[SATURATION]/100*gridSize, (1-current[LIGHT]/100)*gridSize)
	})
}

// slGridImage is the saturation(→) × lightness(↑) plane at the current hue.
func slGridImage() ImageId {
	hue := int(current[HUE] + 0.5)
	key := fmt.Sprintf("color-picker/sl-grid/%d", hue)
	if id := GetImageId(key); id != 0 {
		return id
	}

	d := int(gridSize) * imgScale
	img := image.NewRGBA(image.Rect(0, 0, d, d))
	for y := 0; y < d; y++ {
		l := (1 - (f32(y)+0.5)/f32(d)) * 100
		for x := 0; x < d; x++ {
			s := (f32(x) + 0.5) / f32(d) * 100
			col := HSLAColor(Vec4{f32(hue), s, l, 1})
			i := img.PixOffset(x, y)
			img.Pix[i+0] = col.R
			img.Pix[i+1] = col.G
			img.Pix[i+2] = col.B
			img.Pix[i+3] = 0xff
		}
	}
	return UseImage(key, img)
}

// -----------------------------------------------------------------------------
//  Hue / saturation wheel
// -----------------------------------------------------------------------------

const wheelSize f32 = 220

func hueWheelPanel() {
	Container(Attrs(FixSize(wheelSize, wheelSize), Focusable, NoAnimate), func() {
		radius := wheelSize / 2
		if p, ok := dragPoint(); ok {
			dx := p[0] - radius
			dy := p[1] - radius
			current[HUE] = hueOf(dx, dy)
			s := f32(math.Sqrt(float64(dx*dx+dy*dy))) / radius * 100
			generic.Clamp(0, &s, 100)
			current[SATURATION] = s
		}
		ImageView(wheelImage(), Vec2{wheelSize, wheelSize})
		angle := float64(current[HUE]) * math.Pi / 180
		r := current[SATURATION] / 100 * radius
		marker(radius+f32(math.Cos(angle))*r, radius-f32(math.Sin(angle))*r)
	})
}

// hueOf maps a vector from the wheel center to a hue angle in degrees:
// 0° points right (+x), increasing counterclockwise (y grows downward on
// screen, hence -dy).
func hueOf(dx, dy f32) f32 {
	hue := f32(math.Atan2(float64(-dy), float64(dx))) * 180 / math.Pi
	if hue < 0 {
		hue += 360
	}
	return hue
}

// wheelImage is the hue(angle) × saturation(radius) disc at the current
// lightness. Pixels are premultiplied; the rim is antialiased into
// transparency so the card background shows through outside the disc.
func wheelImage() ImageId {
	light := int(current[LIGHT] + 0.5)
	key := fmt.Sprintf("color-picker/wheel/%d", light)
	if id := GetImageId(key); id != 0 {
		return id
	}

	d := int(wheelSize) * imgScale
	img := image.NewRGBA(image.Rect(0, 0, d, d))
	radius := f32(d) / 2
	for y := 0; y < d; y++ {
		dy := f32(y) + 0.5 - radius
		for x := 0; x < d; x++ {
			dx := f32(x) + 0.5 - radius
			r := f32(math.Sqrt(float64(dx*dx + dy*dy)))
			cov := radius - r + 0.5 // ~1 device pixel of edge coverage
			generic.Clamp(0, &cov, 1)
			if cov == 0 {
				continue
			}
			s := r / radius * 100
			generic.Clamp(0, &s, 100)
			col := HSLAColor(Vec4{hueOf(dx, dy), s, f32(light), 1})
			i := img.PixOffset(x, y)
			img.Pix[i+0] = uint8(f32(col.R)*cov + 0.5)
			img.Pix[i+1] = uint8(f32(col.G)*cov + 0.5)
			img.Pix[i+2] = uint8(f32(col.B)*cov + 0.5)
			img.Pix[i+3] = uint8(255*cov + 0.5)
		}
	}
	return UseImage(key, img)
}

// -----------------------------------------------------------------------------
//  Preview
// -----------------------------------------------------------------------------

func previewBar() {
	Container(Attrs(Row, CrossMid, Gap(14), Pad(14), Corners(10),
		Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 85, 1)), func() {
		const sw, sh f32 = 96, 56
		// Swatch: the current color composited over a checkerboard by the
		// renderer itself, so alpha is honest.
		Container(Attrs(FixSize(sw, sh), Corners(6), Clip, NoAnimate), func() {
			ImageView(checkerImage(int(sw)*imgScale, int(sh)*imgScale), Vec2{sw, sh})
			Element(Attrs(Float(0, 0), FixSize(sw, sh), BackgroundVec(current), NoAnimate))
		})
		Container(Attrs(Gap(4)), func() {
			Label(fmt.Sprintf("hsla(%.0f, %.0f%%, %.0f%%, %.2f)",
				current[0], current[1], current[2], current[3]),
				FontSize(14), FontWeight(WeightSemibold), TextColor(0, 0, 20, 1))
			Label(hexRGB(current), FontSize(13), TextColor(0, 0, 45, 1))
		})
	})
}

// hexRGB formats the color's opaque RGB value as #rrggbb.
func hexRGB(c Vec4) string {
	col := HSLAColor(Vec4{c[0], c[1], c[2], 1})
	return fmt.Sprintf("#%02x%02x%02x", col.R, col.G, col.B)
}

// -----------------------------------------------------------------------------
//  Shared machinery
// -----------------------------------------------------------------------------

// imgScale is how many pixels of generated imagery back one logical point.
// ImageView never scales up, so buffers are built at 2× and displayed at
// logical size — sharp on HiDPI displays, cheaply downscaled on 1×.
const imgScale = 2

// dragPoint reports the pointer position local to the current container while
// a drag is engaged: mouse press capture, or a latched touch contact (the
// finger may wander outside the box; tracking holds until lift — the same
// approach as widgets.ProcessSlider, in two dimensions).
func dragPoint() (Vec2, bool) {
	origin := GetScreenRect().Origin
	type dragHook struct{ touchId uint32 }
	drag := Use[dragHook](0)

	if drag.touchId != 0 {
		if ti, ok := TouchById(drag.touchId); ok {
			return Vec2Sub(ti.Pos, origin), true
		}
		drag.touchId = 0
	}
	if IsTouched() {
		ids := TouchingIds(nil)
		if len(ids) > 0 {
			if ti, ok := TouchById(ids[0]); ok {
				drag.touchId = ids[0]
				return Vec2Sub(ti.Pos, origin), true
			}
		}
	}
	if !GetInputState().MouseFromTouch {
		PressAction()
		if IsActive() {
			return Vec2Sub(GetInputState().MousePoint, origin), true
		}
	}
	return Vec2{}, false
}

// marker draws the position ring used by the grid and the wheel, centered
// on (x, y) in the current container.
func marker(x, y f32) {
	ring(x, y, 14)
}

// ring draws a white band of diameter d with a dark hairline just outside,
// centered on (cx, cy). The center stays open so the color underneath shows
// through, and the hairline keeps the band readable over light colors.
func ring(cx, cy, d f32) {
	Element(Attrs(Float(cx-d/2, cy-d/2), FixSize(d, d), Corners(d/2),
		BorderWidth(3), BorderColor(0, 0, 100, 1), ClickThrough, NoAnimate))
	od := d + 2
	Element(Attrs(Float(cx-od/2, cy-od/2), FixSize(od, od), Corners(od/2),
		BorderWidth(1), BorderColor(0, 0, 0, 0.35), ClickThrough, NoAnimate))
}

// quantized rounds a color to the resolution used in image cache keys:
// whole degrees / percents, alpha in hundredths.
func quantized(c Vec4) [4]int {
	return [4]int{
		int(c[0] + 0.5),
		int(c[1] + 0.5),
		int(c[2] + 0.5),
		int(c[3]*100 + 0.5),
	}
}

// checkerShade returns the transparency-checkerboard shade under a device
// pixel (squares of 6 logical points).
func checkerShade(x, y int) uint8 {
	const sq = 6 * imgScale
	if (x/sq+y/sq)%2 == 0 {
		return 0xff
	}
	return 0xc8
}

// mix8 blends fg over bg with coverage a (straight alpha, 8-bit channels).
func mix8(bg, fg uint8, a f32) uint8 {
	return uint8(f32(fg)*a + f32(bg)*(1-a) + 0.5)
}

// checkerImage returns a static opaque checkerboard (transparency backdrop).
func checkerImage(w, h int) ImageId {
	key := fmt.Sprintf("color-picker/checker/%dx%d", w, h)
	if id := GetImageId(key); id != 0 {
		return id
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			shade := checkerShade(x, y)
			i := img.PixOffset(x, y)
			img.Pix[i+0] = shade
			img.Pix[i+1] = shade
			img.Pix[i+2] = shade
			img.Pix[i+3] = 0xff
		}
	}
	return UseImage(key, img)
}
