package tw

import . "go.hasen.dev/slay"

// TailWind style way to build up the attributes!

type AttrsFn func(*Attrs)
type f32 = float32

// TailWind
func TW(fns ...AttrsFn) Attrs {
	var a Attrs
	for _, f := range fns {
		f(&a)
	}
	return a
}

// TailWind With
func TWW(a Attrs, fns ...AttrsFn) Attrs {
	for _, f := range fns {
		f(&a)
	}
	return a
}

func Compose(fns ...AttrsFn) AttrsFn {
	return func(a *Attrs) {
		for _, f := range fns {
			f(a)
		}
	}
}

func Row(a *Attrs) {
	a.Row = true
}

func Wrap(a *Attrs) {
	a.Wrap = true
}

func Clip(a *Attrs) {
	a.Clip = true
}

func NoAnimate(a *Attrs) {
	a.NoAnimate = true
}

func RowF(row bool) AttrsFn {
	return func(a *Attrs) {
		a.Row = row
	}
}

func Pad(v float32) AttrsFn {
	return func(a *Attrs) {
		a.Padding = N4(v)
	}
}

func Pad2(v, h float32) AttrsFn {
	return func(a *Attrs) {
		a.Padding = PaddingVH(v, h)
	}
}

func Pad4(t, r, b, l float32) AttrsFn {
	return func(a *Attrs) {
		a.Padding = Vec4{t, r, b, l}
	}
}

func PadV(v Vec4) AttrsFn {
	return func(a *Attrs) {
		a.Padding = v
	}
}

func Gap(v float32) AttrsFn {
	return func(a *Attrs) {
		a.Gap = v
	}
}

func Spacing(v float32) AttrsFn {
	return func(a *Attrs) {
		a.Gap = v
		a.Padding = N4(v)
	}
}

func MinSize(w, h float32) AttrsFn {
	return func(a *Attrs) {
		a.MinSize = Vec2{w, h}
	}
}

func MinSizeV(v Vec2) AttrsFn {
	return func(a *Attrs) {
		a.MinSize = v
	}
}

func MinWidth(w float32) AttrsFn {
	return func(a *Attrs) {
		a.MinSize[0] = w
	}
}

func MinHeight(h float32) AttrsFn {
	return func(a *Attrs) {
		a.MinSize[1] = h
	}
}

func MaxWidth(w float32) AttrsFn {
	return func(a *Attrs) {
		a.MaxSize[0] = w
	}
}

func MaxHeight(h float32) AttrsFn {
	return func(a *Attrs) {
		a.MaxSize[1] = h
	}
}

func MaxSizeV(v Vec2) AttrsFn {
	return func(a *Attrs) {
		a.MaxSize = v
	}
}

func FixSizeV(v Vec2) AttrsFn {
	return func(a *Attrs) {
		a.MaxSize = v
		a.MinSize = v
	}
}

func FixSize(w, h float32) AttrsFn {
	return func(a *Attrs) {
		a.MaxSize = Vec2{w, h}
		a.MinSize = Vec2{w, h}
	}
}

func FixWidth(w float32) AttrsFn {
	return func(a *Attrs) {
		a.MinSize[0] = w
		a.MaxSize[0] = w
	}
}

func CA(a Alignment) AttrsFn {
	return func(at *Attrs) {
		at.CrossAlign = a
	}
}

func MA(a Alignment) AttrsFn {
	return func(at *Attrs) {
		at.MainAlign = a
	}
}

func SA(a Alignment) AttrsFn {
	return func(at *Attrs) {
		at.SelfAlign = a
	}
}

func CrossMid(a *Attrs) {
	a.CrossAlign = AlignMiddle
}

func Center(a *Attrs) {
	a.MainAlign = AlignMiddle
	a.CrossAlign = AlignMiddle
}

func BG(h, s, l, a float32) AttrsFn {
	return func(attrs *Attrs) {
		attrs.Background = Vec4{h, s, l, a}
	}
}

func DBG(hue float32) AttrsFn {
	return func(attrs *Attrs) {
		attrs.BorderColor = Vec4{hue, 50, 50, 1}
		attrs.BorderWidth = 1
	}
}

func BW(f float32) AttrsFn {
	return func(attrs *Attrs) {
		attrs.BorderWidth = f
	}
}

func Bo(h, s, l, a float32) AttrsFn {
	return func(attrs *Attrs) {
		attrs.BorderColor = Vec4{h, s, l, a}
	}
}

func BGV(v Vec4) AttrsFn {
	return func(attrs *Attrs) {
		attrs.Background = v
	}
}

func GradV(g Vec4) AttrsFn {
	return func(attrs *Attrs) {
		attrs.Gradient = g
	}
}

func Grad(dh, ds, dl, da f32) AttrsFn {
	// delta heu, delta saturation .. etc
	return func(attrs *Attrs) {
		attrs.Gradient = Vec4{dh, ds, dl, da}
	}
}

func Expand(a *Attrs) {
	a.ExpandAcross = true
}

func Grow(f float32) AttrsFn {
	return func(attrs *Attrs) {
		attrs.Grow = f
	}
}

func Shd(r float32) AttrsFn {
	return func(a *Attrs) {
		a.Shadow.Alpha = 0.5
		a.Shadow.Blur = r
		a.Shadow.Offset[1] = 1
	}
}

func Extrinsic(a *Attrs) {
	a.ExtrinsicSize = true
}

func Viewport(a *Attrs) {
	a.Clip = true
	a.ExtrinsicSize = true
	a.ExpandAcross = true
	a.Grow = 1
}

func Float(x, y float32) AttrsFn {
	return func(a *Attrs) {
		a.Floats = true
		a.Float = Vec2{x, y}
	}
}

func FloatV(v Vec2) AttrsFn {
	return func(a *Attrs) {
		a.Floats = true
		a.Float = v
	}
}

func Focusable(a *Attrs) {
	a.Focusable = true
}

// border radius
func BR(v float32) AttrsFn {
	return func(a *Attrs) {
		a.Corners = N4(v)
	}
}

func Trans(v float32) AttrsFn {
	return func(a *Attrs) {
		a.Transperancy = v
	}
}

func ClickThrough(a *Attrs) {
	a.ClickThrough = true
}

// text

type TextAttrsFn func(*TextAttrs)

func TTW(fns ...TextAttrsFn) TextAttrs {
	var a = DefaultTextAttrs()
	for _, fn := range fns {
		fn(&a)
	}
	return a
}

func Label(text string, fns ...TextAttrsFn) {
	Text(text, TTW(fns...))
}

func Clr(h, s, l, a float32) TextAttrsFn {
	return func(at *TextAttrs) {
		at.Color = Vec4{h, s, l, a}
	}
}

func ClrV(v Vec4) TextAttrsFn {
	return func(at *TextAttrs) {
		at.Color = v
	}
}

func Sz(h float32) TextAttrsFn {
	return func(a *TextAttrs) {
		a.Size = h
	}
}

func Fonts(fs ...string) TextAttrsFn {
	return func(a *TextAttrs) {
		a.Families = append(a.Families, fs...)
	}
}

func FontWeight(w Weight) TextAttrsFn {
	return func(a *TextAttrs) {
		a.Weight = w
	}
}

func FontStyle(w Style) TextAttrsFn {
	return func(a *TextAttrs) {
		a.Style = w
	}
}

func TextWidth(v float32) TextAttrsFn {
	return func(a *TextAttrs) {
		a.MaxWidth = v
	}
}

func TCompose(fns ...TextAttrsFn) TextAttrsFn {
	return func(a *TextAttrs) {
		for _, f := range fns {
			f(a)
		}
	}
}
