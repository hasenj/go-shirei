package shirei

// AttrsFn is a single attribute setter. The Attrs and AttrsWith builders take a
// list of these (Row, Pad(8), Gap(6), ...) and apply them in order; this is the
// blessed way to specify container attributes.
type AttrsFn func(*AttrSet)

// Attrs builds an AttrSet by applying the given setters in order.
func Attrs(fns ...AttrsFn) AttrSet {
	var a AttrSet
	for _, f := range fns {
		f(&a)
	}
	return a
}

// AttrsWith builds an AttrSet starting from a base, then applies the setters.
func AttrsWith(a AttrSet, fns ...AttrsFn) AttrSet {
	for _, f := range fns {
		f(&a)
	}
	return a
}

// ComposeAttrs bundles several setters into a single AttrsFn, so a reusable
// group of attributes can be passed around and applied as one.
func ComposeAttrs(fns ...AttrsFn) AttrsFn {
	return func(a *AttrSet) {
		for _, f := range fns {
			f(a)
		}
	}
}

// Row arranges children horizontally (left to right) instead of the default
// vertical column.
func Row(a *AttrSet) {
	a.Row = true
}

// Wrap lets children flow onto additional lines when they don't all fit along
// the main axis.
func Wrap(a *AttrSet) {
	a.Wrap = true
}

// Clip constrains children — both their drawing and their pointer events — to
// this container's bounds.
func Clip(a *AttrSet) {
	a.Clip = true
}

// NoAnimate disables layout and appearance animations for this container.
func NoAnimate(a *AttrSet) {
	a.NoAnimate = true
}

// YesAnimate clears NoAnimate, re-enabling animation for this container.
func YesAnimate(a *AttrSet) {
	a.NoAnimate = false
}

// RowF sets horizontal (row) layout when row is true, and vertical (column)
// when false — the parameterized form of Row.
func RowF(row bool) AttrsFn {
	return func(a *AttrSet) {
		a.Row = row
	}
}

// Pad sets equal padding on all four sides.
func Pad(v float32) AttrsFn {
	return func(a *AttrSet) {
		a.Padding = N4(v)
	}
}

// Pad2 sets vertical (top and bottom) and horizontal (left and right) padding.
func Pad2(v, h float32) AttrsFn {
	return func(a *AttrSet) {
		a.Padding = PaddingVH(v, h)
	}
}

// Pad4 sets per-side padding in top, right, bottom, left order.
func Pad4(t, r, b, l float32) AttrsFn {
	return func(a *AttrSet) {
		a.Padding = Vec4{t, r, b, l}
	}
}

// PadVec sets padding from a Vec4 in top, right, bottom, left order.
func PadVec(v Vec4) AttrsFn {
	return func(a *AttrSet) {
		a.Padding = v
	}
}

// Gap sets the spacing inserted between children along the main axis.
func Gap(v float32) AttrsFn {
	return func(a *AttrSet) {
		a.Gap = v
	}
}

// Spacing is a shorthand that sets both the gap between children and equal
// padding around them to the same value.
func Spacing(v float32) AttrsFn {
	return func(a *AttrSet) {
		a.Gap = v
		a.Padding = N4(v)
	}
}

// MinSize sets the minimum width and height.
func MinSize(w, h float32) AttrsFn {
	return func(a *AttrSet) {
		a.MinSize = Vec2{w, h}
	}
}

// MinSizeVec sets the minimum size from a Vec2.
func MinSizeVec(v Vec2) AttrsFn {
	return func(a *AttrSet) {
		a.MinSize = v
	}
}

// MinWidth sets the minimum width, leaving the minimum height unchanged.
func MinWidth(w float32) AttrsFn {
	return func(a *AttrSet) {
		a.MinSize[0] = w
	}
}

// MinHeight sets the minimum height, leaving the minimum width unchanged.
func MinHeight(h float32) AttrsFn {
	return func(a *AttrSet) {
		a.MinSize[1] = h
	}
}

// MaxWidth sets the maximum width, leaving the maximum height unchanged.
func MaxWidth(w float32) AttrsFn {
	return func(a *AttrSet) {
		a.MaxSize[0] = w
	}
}

// MaxHeight sets the maximum height, leaving the maximum width unchanged.
func MaxHeight(h float32) AttrsFn {
	return func(a *AttrSet) {
		a.MaxSize[1] = h
	}
}

// MaxSizeVec sets the maximum size from a Vec2.
func MaxSizeVec(v Vec2) AttrsFn {
	return func(a *AttrSet) {
		a.MaxSize = v
	}
}

// FixSizeVec fixes the size to an exact Vec2 by setting min and max equal.
func FixSizeVec(v Vec2) AttrsFn {
	return func(a *AttrSet) {
		a.MaxSize = v
		a.MinSize = v
	}
}

// FixSize fixes the width and height to exact values (min equals max).
func FixSize(w, h float32) AttrsFn {
	return func(a *AttrSet) {
		a.MaxSize = Vec2{w, h}
		a.MinSize = Vec2{w, h}
	}
}

// FixWidth fixes the width to an exact value (min equals max), leaving height
// free.
func FixWidth(w float32) AttrsFn {
	return func(a *AttrSet) {
		a.MinSize[0] = w
		a.MaxSize[0] = w
	}
}

// FixHeight fixes the height to an exact value (min equals max), leaving width
// free.
func FixHeight(w float32) AttrsFn {
	return func(a *AttrSet) {
		a.MinSize[1] = w
		a.MaxSize[1] = w
	}
}

// CrossAlign sets how children are aligned along the cross axis.
func CrossAlign(a Alignment) AttrsFn {
	return func(at *AttrSet) {
		at.CrossAlign = a
	}
}

// MainAlign sets how children are aligned (and any extra space distributed)
// along the main axis.
func MainAlign(a Alignment) AttrsFn {
	return func(at *AttrSet) {
		at.MainAlign = a
	}
}

// SelfAlign overrides the parent's cross-axis alignment for this one child.
func SelfAlign(a Alignment) AttrsFn {
	return func(at *AttrSet) {
		at.SelfAlign = a
	}
}

// CrossMid centers children on the cross axis — shorthand for
// CrossAlign(AlignMiddle).
func CrossMid(a *AttrSet) {
	a.CrossAlign = AlignMiddle
}

// Center centers children on both the main and cross axes.
func Center(a *AttrSet) {
	a.MainAlign = AlignMiddle
	a.CrossAlign = AlignMiddle
}

// Background sets the fill color as HSLA (hue, saturation, lightness, alpha).
func Background(h, s, l, a float32) AttrsFn {
	return func(attrs *AttrSet) {
		attrs.Background = Vec4{h, s, l, a}
	}
}

// BorderWidth sets the border thickness.
func BorderWidth(f float32) AttrsFn {
	return func(attrs *AttrSet) {
		attrs.BorderWidth = f
	}
}

// BorderColor sets the border color as HSLA (hue, saturation, lightness, alpha).
func BorderColor(h, s, l, a float32) AttrsFn {
	return func(attrs *AttrSet) {
		attrs.BorderColor = Vec4{h, s, l, a}
	}
}

// BorderColorVec sets the border color color from an HSLA Vec4.
func BorderColorVec(v Vec4) AttrsFn {
	return func(attrs *AttrSet) {
		attrs.BorderColor = v
	}
}

// BackgroundVec sets the fill color from an HSLA Vec4.
func BackgroundVec(v Vec4) AttrsFn {
	return func(attrs *AttrSet) {
		attrs.Background = v
	}
}

// GradVec sets a background gradient from an HSLA Vec4 of per-channel deltas
// added to the background color across the fill.
func GradVec(g Vec4) AttrsFn {
	return func(attrs *AttrSet) {
		attrs.Gradient = g
	}
}

// Grad sets a background gradient expressed as per-channel HSLA deltas (delta
// hue, saturation, lightness, alpha) from the background color.
func Grad(dh, ds, dl, da f32) AttrsFn {
	return func(attrs *AttrSet) {
		attrs.Gradient = Vec4{dh, ds, dl, da}
	}
}

// Expand stretches this container to fill the parent's cross axis.
func Expand(a *AttrSet) {
	a.ExpandAcross = true
}

// Grow sets the flex-grow factor: how much of the leftover main-axis space this
// container claims relative to its growing siblings.
func Grow(f float32) AttrsFn {
	return func(attrs *AttrSet) {
		attrs.Grow = f
	}
}

// BoxShadow adds a drop shadow with the given blur radius and a slight downward
// offset.
func BoxShadow(r float32) AttrsFn {
	return func(a *AttrSet) {
		a.Shadow.Alpha = 0.5
		a.Shadow.Blur = r
		a.Shadow.Offset[1] = 1
	}
}

// Glow adds a soft, faint shadow with the given blur radius and no offset,
// producing a glow rather than a drop shadow.
func Glow(r float32) AttrsFn {
	return func(a *AttrSet) {
		a.Shadow.Alpha = 0.1
		a.Shadow.Blur = r
	}
}

// Extrinsic makes the container's size independent of its content, so it takes
// its size from its constraints rather than growing to fit what's inside.
func Extrinsic(a *AttrSet) {
	a.ExtrinsicSize = true
}

// Viewport is a convenience preset for a scrolling/clipping region: it clips its
// content, sizes extrinsically, expands across, grows to fill the available
// space, and disables animation.
func Viewport(a *AttrSet) {
	a.Clip = true
	a.ExtrinsicSize = true
	a.ExpandAcross = true
	a.Grow = 1
	a.NoAnimate = true
}

// Float takes this container out of the normal layout flow and positions it at
// an explicit (x, y) offset.
func Float(x, y float32) AttrsFn {
	return func(a *AttrSet) {
		a.Floats = true
		a.Float = Vec2{x, y}
	}
}

// Z sets the draw order (z-index); higher values draw on top of lower ones.
func Z(z f32) AttrsFn {
	return func(a *AttrSet) {
		a.Z = z
	}
}

// Behind draws this container behind its siblings (z = -1).
func Behind(a *AttrSet) {
	a.Z = -1
}

// InFront draws this container in front of its siblings (z = 1).
func InFront(a *AttrSet) {
	a.Z = 1
}

// FloatVec takes this container out of the normal flow and positions it at the
// given offset — the Vec2 form of Float.
func FloatVec(v Vec2) AttrsFn {
	return func(a *AttrSet) {
		a.Floats = true
		a.Float = v
	}
}

// Focusable allows this container to receive keyboard focus.
func Focusable(a *AttrSet) {
	a.Focusable = true
}

func FocusTrap(a *AttrSet) {
	a.FocusTrap = true
}

// Corners sets a uniform border radius on all four corners.
func Corners(v float32) AttrsFn {
	return func(a *AttrSet) {
		a.Corners = N4(v)
	}
}

// Corners4 sets a per-corner border radius in top-left, top-right,
// bottom-right, bottom-left order.
func Corners4(tl, tr, br, bl f32) AttrsFn {
	return func(a *AttrSet) {
		a.Corners = Vec4{tl, tr, br, bl}
	}
}

// Trans sets transparency, from 0 (fully opaque) to 1 (fully transparent),
// applied to this container and inherited by its children.
func Trans(v float32) AttrsFn {
	return func(a *AttrSet) {
		a.Transparency = v
	}
}

// ClickThrough lets pointer events pass through this container to whatever is
// beneath it.
func ClickThrough(a *AttrSet) {
	a.ClickThrough = true
}

// text

// TextAttrsFn is a single text-attribute setter — the text counterpart to
// AttrsFn, applied by TextAttrs and TextAttrsWith.
type TextAttrsFn func(*TextAttrSet)

// TextAttrs builds a TextAttrSet from the default text attributes, then applies
// the given setters in order.
func TextAttrs(fns ...TextAttrsFn) TextAttrSet {
	var a = DefaultTextAttrs()
	for _, fn := range fns {
		fn(&a)
	}
	return a
}

// TextAttrsWith builds a TextAttrSet starting from a base, then applies the
// setters.
func TextAttrsWith(base TextAttrSet, fns ...TextAttrsFn) TextAttrSet {
	var a = base
	for _, fn := range fns {
		fn(&a)
	}
	return a
}

// Span builds a fully resolved StyleSpan: copy base, apply mods, store the
// resulting TextStyle for [from, to). Mods that only touch MaxWidth/Spans
// are ignored for the stored style. Phase 1: each span is independent
// (always relative to base, not to other spans).
func Span(from, to int, base TextStyle, mods ...TextAttrsFn) StyleSpan {
	a := TextAttrSet{TextStyle: base}
	for _, m := range mods {
		m(&a)
	}
	return StyleSpan{From: from, To: to, Style: a.TextStyle}
}

// WithSpans returns a copy of base with Spans set to the given list
// (replacing any previous Spans).
func WithSpans(base TextAttrSet, spans ...StyleSpan) TextAttrSet {
	base.Spans = spans
	return base
}

// Label renders text with the given text attributes — a convenience over
// calling Text with TextAttrs.
func Label(text string, fns ...TextAttrsFn) {
	Text(text, TextAttrs(fns...))
}

// TextColor sets the text color as HSLA (hue, saturation, lightness, alpha).
func TextColor(h, s, l, a float32) TextAttrsFn {
	return func(at *TextAttrSet) {
		at.Color = Vec4{h, s, l, a}
	}
}

// TextColorVec sets the text color from an HSLA Vec4.
func TextColorVec(v Vec4) TextAttrsFn {
	return func(at *TextAttrSet) {
		at.Color = v
	}
}

// FontSize sets the font size.
func FontSize(h float32) TextAttrsFn {
	return func(a *TextAttrSet) {
		a.Size = h
	}
}

// Fonts sets preferred font families, tried in order ahead of the defaults.
func Fonts(fs ...string) TextAttrsFn {
	return func(a *TextAttrSet) {
		a.Families = append(fs, a.Families...)
	}
}

// FontWeight sets the font weight (e.g. regular, bold).
func FontWeight(w Weight) TextAttrsFn {
	return func(a *TextAttrSet) {
		a.Weight = w
	}
}

// FontStyle sets the font style (e.g. normal, italic).
func FontStyle(w Style) TextAttrsFn {
	return func(a *TextAttrSet) {
		a.Style = w
	}
}

// TextWidth sets the maximum width at which the text wraps.
func TextWidth(v float32) TextAttrsFn {
	return func(a *TextAttrSet) {
		a.MaxWidth = v
	}
}

// TextBackground sets a highlight color painted behind glyphs (HSLA).
func TextBackground(h, s, l, a float32) TextAttrsFn {
	return func(at *TextAttrSet) {
		at.Background = Vec4{h, s, l, a}
	}
}

// TextBackgroundVec sets a highlight color painted behind glyphs.
func TextBackgroundVec(v Vec4) TextAttrsFn {
	return func(at *TextAttrSet) {
		at.Background = v
	}
}

// TextUnderline enables or disables underline on the text style.
func TextUnderline(on bool) TextAttrsFn {
	return func(a *TextAttrSet) {
		a.Underline = on
	}
}

// TextStrike enables or disables strikethrough on the text style.
func TextStrike(on bool) TextAttrsFn {
	return func(a *TextAttrSet) {
		a.Strike = on
	}
}

// ComposeTextAttrs bundles several text setters into a single TextAttrsFn.
func ComposeTextAttrs(fns ...TextAttrsFn) TextAttrsFn {
	return func(a *TextAttrSet) {
		for _, f := range fns {
			f(a)
		}
	}
}
