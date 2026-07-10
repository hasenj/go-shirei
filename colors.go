package shirei

import (
	"image/color"
	"math"
)

type f64 = float64

// HSLAColor converts an HSLA color — hue in 0..360, saturation and lightness in
// 0..100, alpha in 0..1 — to a Go image/color.NRGBA.
func HSLAColor(c Vec4) color.NRGBA {
	h := c[0] / 360
	s := c[1] / 100
	l := c[2] / 100

	r, g, b := FloatHSLToRGB(h, s, l)
	return color.NRGBA{
		R: uint8(r * 0xff),
		G: uint8(g * 0xff),
		B: uint8(b * 0xff),
		A: uint8(c[3] * 0xff),
	}
}

// FloatHSLToRGB converts HSL (each component in 0..1) to RGB (each in 0..1).
// Adapted from https://github.com/alessani/ColorConverter/blob/master/ColorSpaceUtilities.h
func FloatHSLToRGB(h f32, s f32, l f32) (f32, f32, f32) {
	// Check for saturation. If there isn't any just return the luminance value for each, which results in gray.
	if s == 0.0 {
		return l, l, l
	}

	var temp2 f32
	// Test for luminance and compute temporary values based on luminance and saturation
	if l < 0.5 {
		temp2 = l * (1.0 + s)
	} else {
		temp2 = l + s - l*s
	}
	temp1 := 2.0*l - temp2

	// Compute intermediate values based on hue
	temp := [3]f32{
		h + 1.0/3.0,
		h,
		h - 1.0/3.0,
	}

	for i := 0; i < 3; i++ {
		// Adjust the range
		if temp[i] < 0.0 {
			temp[i] += 1.0
		}
		if temp[i] > 1.0 {
			temp[i] -= 1.0
		}

		if 6.0*temp[i] < 1.0 {
			temp[i] = temp1 + (temp2-temp1)*6.0*temp[i]
		} else {
			if 2.0*temp[i] < 1.0 {
				temp[i] = temp2
			} else {
				if 3.0*temp[i] < 2.0 {
					temp[i] = temp1 + (temp2-temp1)*((2.0/3.0)-temp[i])*6.0
				} else {
					temp[i] = temp1
				}
			}
		}
	}

	return temp[0], temp[1], temp[2]
}

// ContrastingTextColor picks white or near-black — whichever has the
// higher WCAG contrast ratio — for text/icons drawn over an HSLA
// background color. Ignores bg's alpha: callers with a translucent
// background should pass the color it's actually blended to.
// https://www.w3.org/TR/WCAG20/#relativeluminancedef
func ContrastingTextColor(bg Vec4) Vec4 {
	r, g, b := FloatHSLToRGB(bg[0]/360, bg[1]/100, bg[2]/100)

	linear := func(c f32) f64 {
		cf := f64(c)
		if cf <= 0.03928 {
			return cf / 12.92
		}
		return math.Pow((cf+0.055)/1.055, 2.4)
	}

	luminance := 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)

	contrastWithWhite := 1.05 / (luminance + 0.05)
	contrastWithBlack := (luminance + 0.05) / 0.05

	const bias = 0.12
	if contrastWithWhite >= contrastWithBlack*bias {
		return Vec4{0, 0, 100, 1}
	}
	return Vec4{0, 0, 10, 1}
}
