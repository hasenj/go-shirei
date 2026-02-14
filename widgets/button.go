package widgets

import (
	"github.com/cli/browser"
	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
)

const ButtonDefaultSize = DefaultTextSize
const ButtonSmallSize = 8

type f32 = float32

type ButtonAttrs struct {
	Ctrl     bool
	Disabled bool
	Primary  bool
	TextSize f32
	Icon     rune // if present, must be a micron symbol!
}

func Button(icon rune, label string) bool {
	return ButtonExt(label, ButtonAttrs{Icon: icon})
}

func CtrlButton(icon rune, label string, enabled bool) bool {
	return ButtonExt(label, ButtonAttrs{Ctrl: true, Icon: icon, Disabled: !enabled})
}

func ButtonExt(label string, attrs ButtonAttrs) bool {
	if attrs.TextSize == 0 {
		attrs.TextSize = ButtonDefaultSize
	}
	var action = false
	var pushDownDistance f32 = 1

	var padh = attrs.TextSize * 0.8
	var padv = padh / 2
	var br = attrs.TextSize * 0.3

	if attrs.Ctrl {
		pushDownDistance = 1
		padv *= 0.6
		padh *= 0.6
		br *= 0.6
	}

	Layout(TW(), func() {
		shadowColor := Vec4{0, 0, 0, 0.6}
		shadowPadding := Vec4{0}

		var light float32 = 95
		var highlight float32 = light + 3
		var presslight float32 = light - 3
		var lightDelta float32 = -8
		var hue float32 = 220
		var sat float32 = 20
		var textLight float32 = 20
		var textAlpha float32 = 1

		if attrs.Ctrl {
			lightDelta = -4
		}
		if attrs.Disabled {
			// make it flat
			light = 75
			highlight = light
			presslight = light
			lightDelta = 2
			sat = 5
			textLight = 40
			textAlpha = 0.5
		}

		background := Vec4{hue, sat, light, 1}

		// state management
		if !attrs.Disabled {
			action = PressAction()

			// appearance management
			if IsHovered() {
				background[2] = highlight
			}
		}

		if IsActive() {
			background[2] = presslight
			// increase padding on this outer container
			ModAttrs(func(attrs *Attrs) {
				attrs.Padding[PAD_TOP] = pushDownDistance
			})
		} else {
			shadowPadding[PAD_BOTTOM] = pushDownDistance
		}

		Layout(TW(BGV(shadowColor), PadV(shadowPadding), BR(br)), func() {
			var grad Vec4
			grad[LIGHT] = lightDelta
			var attrs1 = TW(Row, BR(br), Pad2(padv, padh), Gap(padh/2), BGV(background), GradV(grad), Bo(0, 0, 0, 0.4), BW(1))
			var shoff = shadowPadding[PAD_BOTTOM]
			attrs1.Shadow = Shadow{
				Offset: Vec2{0, shoff},
				Alpha:  0.4,
				Blur:   shoff,
			}
			Layout(attrs1, func() {
				if attrs.Icon != 0 {
					Icon(attrs.Icon, Sz(attrs.TextSize), Clr(240, 10, textLight, textAlpha))
				}
				if label != "" {
					Label(label, Sz(attrs.TextSize), Clr(240, 10, textLight, textAlpha))
				}
			})
		})
	})
	return action
}

func Link(label string, url string, fns ...TextAttrsFn) {
	Layout(TW(Row), func() {
		if IsClicked() {
			browser.OpenURL(url)
		}
		Label(label, fns...)
	})
}

type SliderAttrs struct {
	Min   f32
	Max   f32
	Step  f32
	Width f32
}

func Slider(value *float32, attrs SliderAttrs) {
	if attrs.Width == 0 {
		attrs.Width = 200
	}
	var r float32 = 10 // radius of circle
	var height = r * 2
	Layout(TW(Row, CrossMid, FixWidth(attrs.Width), FixHeight(height)), func() {
		var width float32 = attrs.Width - r*2

		xOffset := func(v float32) float32 {
			return width * (v - attrs.Min) / (attrs.Max - attrs.Min)
		}
		drawCircle := func(v *float32) {
			Layout(TW(BR(r), MinSize(r*2, r*2), BG(0, 0, 98, 1), Grad(0, 0, -18, 0), Shd(2), BW(1), Bo(0, 0, 0, 0.5)), func() {
				PressAction()
				if IsActive() {
					diff := FrameInput.Motion[0] // mouse movement along x-axis
					// translate the movement to the range given!
					*v += (diff / width) * (attrs.Max - attrs.Min)
					*v = max(attrs.Min, min(attrs.Max, *v))
					if attrs.Step > 0 {
						*v = Roundf32(*v/attrs.Step) * attrs.Step
					}
				}
				xoffset := xOffset(*v)
				ModAttrs(Float(xoffset, 0))
			})
		}

		// background line
		Element(TW(Float(0, (height/2)-1), MinSize(attrs.Width, 1), BG(0, 0, 50, 1)))

		// handle
		drawCircle(value)
	})
}

func Filler(g f32) {
	Element(TW(Grow(g)))
}

func Spacer(s f32) {
	var width f32
	var height f32

	var a = GetAttrs()
	if a.Row {
		width = s
	} else {
		height = s
	}

	Element(TW(FixWidth(width), FixHeight(height)))
}
