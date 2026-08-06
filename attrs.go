package shirei

// AttrsFn is a single attribute setter. The Attrs and AttrsWith builders take a
// list of these (Row, Pad(8), Gap(6), ...) and apply them in order; this is the
// blessed way to specify container attributes.
type AttrsFn func(*AttrSet)

// AnimFlags selects which property channels ease between frames. Bits are
// enable flags: 1 = animate that channel, 0 = snap. Attrs() seeds AnimAll
// without marking the mask set, so open-time cascade still intersects with a
// NoAnimate/Viewport parent. Explicit NoAnimate / YesAnimate / AnimateOnly
// set animationsSet and block that cascade.
type AnimFlags uint16

const (
	AnimSize AnimFlags = 1 << iota // resolved size
	AnimPos                        // relative origin (layout / float position)
	AnimPad                        // padding
	AnimCorners                    // corner radii
	AnimBorder                     // border width
	AnimAlpha                      // Transparency (not Background alpha)
	// leave room for AnimColor etc.

	// AnimAll enables every channel. Named bits cover the channels the apply
	// site knows about; the rest of the 0xFFFF mask is reserved so YesAnimate
	// stays "everything" when new channels are added.
	AnimAll AnimFlags = 0xFFFF

	// AnimLayout is size + position + pad + corners + border (not alpha).
	AnimLayout = AnimSize | AnimPos | AnimPad | AnimCorners | AnimBorder
)

// Attrs builds an AttrSet by applying the given setters in order.
// Defaults: Clip = true, Animations = AnimAll, animationsSet = false (may
// inherit parent mask via cascade). Call NoAnimate / YesAnimate / AnimateOnly
// to pin the mask; call NoClip to opt out of clipping.
func Attrs(fns ...AttrsFn) AttrSet {
	a := AttrSet{Clip: true, Animations: AnimAll} // unset: cascade may still &= parent
	for _, f := range fns {
		f(&a)
	}
	return a
}

// AttrsWith builds an AttrSet starting from a base, then applies the setters.
// Does not re-apply defaults — base is used as-is (so a zero Animations on
// base stays "animate nothing").
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
// this container's bounds. Attrs() enables this by default; Clip is kept as an
// explicit no-op setter for call sites and for Viewport.
func Clip(a *AttrSet) {
	a.Clip = true
}

// NoClip lets children draw and receive pointer events outside this container's
// bounds. Prefer padding (for shadows) or Popup (for overlays) when possible.
func NoClip(a *AttrSet) {
	a.Clip = false
}

// NoAnimate disables all animation channels on this container (Animations = 0).
// Marks the mask set so open-time cascade does not rewrite it. Unset children
// still inherit zero via cascade (&= parent).
func NoAnimate(a *AttrSet) {
	a.Animations = 0
	a.animationsSet = true
}

// YesAnimate enables all animation channels (Animations = AnimAll).
// Marks the mask set so it wins under a NoAnimate/Viewport parent without
// needing ModAttrs after open.
func YesAnimate(a *AttrSet) {
	a.Animations = AnimAll
	a.animationsSet = true
}

// AnimateOnly enables exactly the given channels (others snap).
// Example: AnimateOnly(AnimPos) eases movement but snaps size changes.
// Marks the mask set (same as YesAnimate) so Attrs(AnimateOnly(...)) works
// under Viewport / NoAnimate parents.
func AnimateOnly(flags AnimFlags) AttrsFn {
	return func(a *AttrSet) {
		a.Animations = flags
		a.animationsSet = true
	}
}

// Animate enables the given channels without clearing others (bitwise OR).
// Marks the mask set. Compose with NoAnimate to start from none under a
// Viewport, e.g. Attrs(NoAnimate, Animate(AnimSize), Animate(AnimAlpha)).
// Alone after Attrs' default AnimAll it only pins the full mask (no-op on bits).
func Animate(flags AnimFlags) AttrsFn {
	return func(a *AttrSet) {
		a.Animations |= flags
		a.animationsSet = true
	}
}

// UnsetMaxCross clears MaxSize on the parent's cross axis — the axis that
// MaxSize cascade writes (width under a column parent, height under a row) —
// and marks maxCrossUnset so open-time cascade does not write it back.
// Safe in Attrs(...) (same idea as YesAnimate under Viewport) or ModAttrs.
func UnsetMaxCross(a *AttrSet) {
	// Orientation of the parent that cascades into this container:
	//   Attrs(...): ui.current is still that parent.
	//   ModAttrs(...): ui.current is this container; parent is ui.current.parent.
	row := false
	if ui.current != nil {
		if a == &ui.current.AttrSet && ui.current.parent != nil {
			row = ui.current.parent.Row
		} else {
			row = ui.current.Row
		}
	}
	_, cross := MainCrossAxes(row)
	a.MaxSize[cross] = 0
	a.maxCrossUnset = true
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
// space, and disables animation (NoAnimate — unset descendants inherit zero
// so scroll does not ease every child's relativeOrigin).
func Viewport(a *AttrSet) {
	a.Clip = true
	a.ExtrinsicSize = true
	a.ExpandAcross = true
	a.Grow = 1
	NoAnimate(a)
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

// TextStyleFn amends a fully resolved TextStyle (copy+mods). Same mods work
// for container AmendTextStyle, call-local Styles, and Span ranges.
type TextStyleFn func(*TextStyleAttrs)

// TextStyle returns the current container text style with mods applied. Pass the
// result as Text's second argument. Does not mutate the current style for siblings.
// Offline code with no open container gets DefaultTextStyle() as the base.
func TextStyle(mods ...TextStyleFn) TextStyleAttrs {
	return TextStyleWith(ui.current.TextStyle, mods...)
}

// AmendTextStyle inherits the text style from the parent container while
// applying modifications to it.
//
// Expected to be called inside `Attrs(...)` while building a new container
//
//	Container(Attrs(AmendTextStyle(FontSize(20), ...), func() {
//		// content
//	})
func AmendTextStyle(mods ...TextStyleFn) AttrsFn {
	return func(a *AttrSet) {
		a.TextStyle = TextStyleWith(ui.current.TextStyle, mods...)
	}
}

// SetTextStyle sets the text style for the container (without inherting anything from parent)
func SetTextStyle(base TextStyleAttrs, mods ...TextStyleFn) AttrsFn {
	return func(a *AttrSet) {
		a.TextStyle = TextStyleWith(base, mods...)
	}
}

// Span builds a deferred range style for Text / ShapeText: when the call runs,
// mods are applied to that call's paragraph base for [from, to).
func Span(from, to int, mods ...TextStyleFn) TextSpan {
	return TextSpan{From: from, To: to, mods: mods}
}

// ResolveSpan builds a fully resolved StyleSpan: copy base, apply mods.
// Prefer Span + Text/ShapeText for UI; use this when a StyleSpan value is
// needed directly (tests, flatten helpers).
func ResolveSpan(from, to int, base TextStyleAttrs, mods ...TextStyleFn) StyleSpan {
	return StyleSpan{From: from, To: to, Style: TextStyleWith(base, mods...)}
}

// Label renders text with the current text style plus optional call-local mods —
// sugar for Text(text, TextStyle(mods...)).
func Label(text string, mods ...TextStyleFn) {
	Text(text, TextStyle(mods...))
}

// TextColor sets the text color as HSLA (hue, saturation, lightness, alpha).
func TextColor(h, s, l, a float32) TextStyleFn {
	return func(st *TextStyleAttrs) {
		st.TextColor = Vec4{h, s, l, a}
	}
}

// TextColorVec sets the text color from an HSLA Vec4.
func TextColorVec(v Vec4) TextStyleFn {
	return func(st *TextStyleAttrs) {
		st.TextColor = v
	}
}

// FontSize sets the font size.
func FontSize(h float32) TextStyleFn {
	return func(st *TextStyleAttrs) {
		st.FontSize = h
	}
}

// Fonts sets preferred font families, tried in order ahead of the defaults.
func Fonts(fs ...string) TextStyleFn {
	return func(st *TextStyleAttrs) {
		st.FontFamilies = append(fs, st.FontFamilies...)
	}
}

// FontWeight sets the font weight (e.g. regular, bold).
func FontWeight(w Weight) TextStyleFn {
	return func(st *TextStyleAttrs) {
		st.Weight = w
	}
}

// FontStyle sets the font style (e.g. normal, italic).
func FontStyle(w Style) TextStyleFn {
	return func(st *TextStyleAttrs) {
		st.Style = w
	}
}

// TextBackground sets a highlight color painted behind glyphs (HSLA).
func TextBackground(h, s, l, a float32) TextStyleFn {
	return func(st *TextStyleAttrs) {
		st.Background = Vec4{h, s, l, a}
	}
}

// TextBackgroundVec sets a highlight color painted behind glyphs.
func TextBackgroundVec(v Vec4) TextStyleFn {
	return func(st *TextStyleAttrs) {
		st.Background = v
	}
}

// TextUnderline enables or disables underline on the text style.
func TextUnderline(on bool) TextStyleFn {
	return func(st *TextStyleAttrs) {
		st.Underline = on
	}
}

// TextStrike enables or disables strikethrough on the text style.
func TextStrike(on bool) TextStyleFn {
	return func(st *TextStyleAttrs) {
		st.Strike = on
	}
}

// ComposeTextStyles bundles several text style setters into one TextStyleFn.
func ComposeTextStyles(fns ...TextStyleFn) TextStyleFn {
	return func(st *TextStyleAttrs) {
		for _, f := range fns {
			f(st)
		}
	}
}
