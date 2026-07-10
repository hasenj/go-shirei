package widgets

// SegmentedControl: adjacent flat option-buttons in a shared hairline frame,
// the shirei take on a radio row. Born in examples/piano's voice picker,
// promoted here once the design settled, then rethemed (2026-07-07) to match
// CheckBox/OptionButton's accent language: an accent-colored frame, and the
// selected segment reading as a solid accent fill (edge to edge, no margin)
// with a bold white label — exactly the filled-vs-outlined convention those
// widgets use.
//
// Dividers between segments are explicit accent-colored elements at the
// same thickness as the outer border (not a padding gap revealing the
// frame's background), so the selected segment's fill can run flush to the
// frame edge with no empty margin around it.

import (
	. "go.hasen.dev/shirei"
)

// SegmentedCell is one option in a SegmentedControl.
type SegmentedCell[T comparable] struct {
	Label string
	Value T
}

// Cell makes a SegmentedCell. A composite literal would need its type
// argument spelled out explicitly (SegmentedCell[Voice]{"Oud", VoiceOud}) —
// Go only infers generic type arguments through a function call, not a bare
// literal — so this constructor exists to let T be inferred from value:
//
//	SegmentedControl(&app.voice, Cell("Oud", VoiceOud), Cell("Flute", VoiceFlute))
func Cell[T comparable](label string, value T) SegmentedCell[T] {
	return SegmentedCell[T]{Label: label, Value: value}
}

// SegmentedControlAttrs configures SegmentedControlExt.
type SegmentedControlAttrs struct {
	Accent Vec4 // zero value: use the package-level Accent
}

const segmentBorderWidth = 1.5
const segmentHeight = 24

// SegmentedControl renders the segments and keeps *target in sync with the
// clicked one. Returns true when the selection changed this frame (handy
// for reacting to the change, e.g. recomputing derived state). Values must
// be unique — they double as the segments' identity.
func SegmentedControl[T comparable](target *T, segments ...SegmentedCell[T]) bool {
	return SegmentedControlExt(target, SegmentedControlAttrs{}, segments...)
}

// SegmentedControlExt is SegmentedControl with a per-instance accent.
func SegmentedControlExt[T comparable](target *T, attrs SegmentedControlAttrs, segments ...SegmentedCell[T]) bool {
	accent := AccentOrFallback(attrs.Accent, DefaultAccent)
	changed := false
	Container(Attrs(Row, Corners(6), BorderWidth(segmentBorderWidth), BorderColor(accent[0], accent[1], accent[2], accent[3]), Clip), func() {
		for i, s := range segments {
			var rl, rr f32
			if i == 0 {
				rl = 5
			}
			if i == len(segments)-1 {
				rr = 5
			}
			if segmentOption(accent, s.Value, s.Label, *target == s.Value, rl, rr) && *target != s.Value {
				*target = s.Value
				changed = true
			}
			if i < len(segments)-1 {
				Element(Attrs(FixWidth(segmentBorderWidth), FixHeight(segmentHeight), BackgroundVec(accent)))
			}
		}
	})
	return changed
}

// segmentOption is one segment; rl/rr round the outer corners of the end
// segments so they follow the frame's radius. Returns true on a full press.
func segmentOption(accent Vec4, id any, label string, selected bool, rl, rr f32) bool {
	clicked := false
	ContainerWithKey(id, Attrs(FixHeight(segmentHeight), MinWidth(56), CrossAlign(AlignMiddle), Pad2(0, 12), Corners4(rl, rr, rr, rl)), func() {
		if PressAction() {
			clicked = true
		}
		hovered := IsHovered()

		bg := Vec4{0, 0, 100, 1}
		grad := Vec4{0, 0, -12, 0}
		textClr := TextColor(0, 0, 25, 1)
		weight := WeightNormal
		if hovered && !selected {
			bg = Vec4{accent[0], accent[1] * 0.3, 96, 1}
		}
		if selected {
			bg = accent
			grad[2] = 12
			textClr = TextColor(0, 0, 100, 1)
			weight = WeightBold
			if hovered {
				bg[2] += 5
			}
		}
		ModAttrs(BackgroundVec(bg), GradVec(grad))

		Filler(1)
		Label(label, FontSize(12), textClr, FontWeight(weight))
		Filler(1)
	})
	return clicked
}
