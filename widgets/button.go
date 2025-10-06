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
