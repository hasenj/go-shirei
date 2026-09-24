package widgets

// SegmentedControl arranges mutually exclusive choices inside a shared frame.
// ProcessSegmentEvents supplies interaction independently of the rendered paint.

import (
	"time"

	. "go.hasen.dev/shirei"
)

// SegmentState is one cell's interaction snapshot for a segmented control.
// Call ProcessSegmentEvents inside that cell's Container (once per cell per
// frame). The function binds to the current container id.
//
// Typical custom segmented control:
//
//	for _, c := range cells {
//	    ContainerWithKey(c.Value, Attrs(...), func() {
//	        st := ProcessSegmentEvents(target, c.Value, false)
//	        if st.BecameSelected {
//	            // st.Prev is the previous selection; st.Local is click pos
//	            // in this cell; st.SelectedAt is when this value became on
//	        }
//	        // paint from st.Selected / st.Hovered / st.Active
//	    })
//	}
type SegmentState[T comparable] struct {
	Hovered      bool
	Active       bool // pointer captured on this cell
	Clicked      bool // completed press on this cell this frame
	Selected     bool // *target == value after this call
	Disabled     bool
	HasFocus     bool
	FocusVisible bool // paint the keyboard focus indicator
	// Local is the pointer relative to this cell's screen top-left.
	Local Vec2

	// BecameSelected is true on the frame this cell was chosen (target
	// changed from something else to value). Prev is *target just before
	// that change — useful for slide direction and other transitions.
	BecameSelected bool
	Prev           T

	// SelectedAt is when *target last became value (zero if this cell is
	// not selected, or was never chosen through ProcessSegmentEvents).
	SelectedAt time.Time
}

// ProcessSegmentEvents analyzes pointer and keyboard interaction on the
// current container as one segment of a mutually-exclusive group bound to
// *target. On a completed click (or Space/Enter while focused) that chooses
// value, it writes *target and returns BecameSelected with Prev set to the
// prior value.
//
// Pointer model matches ProcessButtonEvents. Call once per cell, inside that
// cell's container body. Arrow-key movement across the group is the stock
// SegmentedControl's job — custom skins that want it handle Left/Right on
// the frame after the cells build.
func ProcessSegmentEvents[T comparable](target *T, value T, disabled bool) SegmentState[T] {
	var st SegmentState[T]
	bst := ProcessButtonEvents(disabled)
	st.Disabled = bst.Disabled
	st.Hovered = bst.Hovered
	st.Active = bst.Active
	st.Clicked = bst.Clicked
	st.HasFocus = bst.HasFocus
	st.FocusVisible = bst.FocusVisible
	st.Local = bst.Local

	type hook struct {
		selectedAt time.Time
	}
	h := Use[hook]("seg-cell")

	if st.Clicked && *target != value {
		st.Prev = *target
		*target = value
		st.BecameSelected = true
		h.selectedAt = time.Now()
		RequestNextFrame()
	}
	st.Selected = *target == value
	if st.Selected {
		st.SelectedAt = h.selectedAt
	}
	return st
}

// SegmentedControlAttrs configures SegmentedControlExt. Start from
// DefaultSegmentedControlAttrs() and override fields — a zero layout field is
// intentional (e.g. CellPadH: 0, FrameCorners: 0), not “use stock default.”
// SegmentedControl() always passes DefaultSegmentedControlAttrs().
type SegmentedControlAttrs struct {
	Accent Vec4 // zero value: use the active scheme

	// Expand makes the control fill available width; each segment Grow(1)
	// so free space is shared evenly (MinCellWidth is still a floor).
	Expand bool

	// CellPadH is horizontal padding inside each segment (design units,
	// then × ComfortScale). DefaultSegmentedControlAttrs uses 12; 0 is flush.
	CellPadH f32

	// FrameCorners is the outer frame corner radius (design units, then ×
	// ComfortScale). Inset faces use a smaller radius when > 0.
	// DefaultSegmentedControlAttrs uses 6; 0 is square.
	FrameCorners f32

	// MinCellWidth is the minimum width of each segment (design units, then
	// × ComfortScale). DefaultSegmentedControlAttrs uses 56.
	MinCellWidth f32
}

const segmentBorderWidth = 1

// DefaultSegmentedControlAttrs returns the stock chrome layout (design units
// before ComfortScale). Copy and tweak for SegmentedControlExt.
func DefaultSegmentedControlAttrs() SegmentedControlAttrs {
	return SegmentedControlAttrs{
		CellPadH:     12,
		FrameCorners: 6,
		MinCellWidth: 56,
	}
}

// SegmentedControl renders the segments and keeps *target in sync with the
// clicked one. Returns true when the selection changed this frame (handy
// for reacting to the change, e.g. recomputing derived state). Values must
// be unique — they double as the segments' identity.
//
//	NextAccessName("voice")
//	SegmentedControl(&voice, func() {
//	    NextAccessName("oud")
//	    SegmentedCell("Oud", VoiceOud)
//	    NextAccessName("flute")
//	    SegmentedCell("Flute", VoiceFlute)
//	})
func SegmentedControl[T comparable](target *T, body func()) bool {
	return SegmentedControlExt(target, DefaultSegmentedControlAttrs(), body)
}

// SegmentedControlExt is SegmentedControl with per-instance accent and layout.
// Prefer DefaultSegmentedControlAttrs() as a starting point when overriding
// pad, corners, or Expand.
func SegmentedControlExt[T comparable](target *T, attrs SegmentedControlAttrs, body func()) bool {
	style := CurrentColorScheme.Segmented
	if attrs.Accent != (Vec4{}) {
		style = SelectionStyleWithAccent(style, attrs.Accent)
	}
	return SegmentedControlStyled(target, attrs, style, CurrentColorScheme.FocusRing, body)
}

// SegmentedControlStyled supplies literal tray, cell, label, and focus colors.
// SelectionPaint.Indicator colors each label. Accent is ignored.
func SegmentedControlStyled[T comparable](target *T, attrs SegmentedControlAttrs, style SelectionStyle, focusRing Vec4, body func()) bool {
	inset := comfort(1)
	labelSize := comfort(ButtonDefaultSize)
	// The tray and cell padding together match a default button's padding.
	padV := (labelSize - 2*inset) / 2
	minW := comfort(attrs.MinCellWidth)
	padH := comfort(attrs.CellPadH)
	frameR := comfort(attrs.FrameCorners)
	// Each face fits inside the shared tray.
	endR := f32(0)
	if attrs.FrameCorners > 0 {
		endR = max(0, frameR-inset)
	}
	changed := false

	run := &segmentedRun[T]{
		target:  target,
		changed: &changed,
		style:   style, focusRing: focusRing,
		padV:      padV,
		minW:      minW,
		padH:      padH,
		endR:      endR,
		labelSize: labelSize,
		expand:    attrs.Expand,
	}
	prev := currentSegmented
	currentSegmented = run
	defer func() { currentSegmented = prev }()

	frame := Attrs(Row, Pad(inset), Gap(inset), BorderWidth(segmentBorderWidth), BorderColorVec(style.Unselected.Normal.Border), BackgroundVec(style.Unselected.Normal.Background))
	if frameR > 0 {
		frame = AttrsWith(frame, Corners(frameR))
	}
	if attrs.Expand {
		frame = AttrsWith(frame, Expand)
	}

	Container(frame, func() {
		NextAccessRole("radiogroup")
		AssignAccess()

		type hook struct {
			ids    []ContainerId
			values []T
		}
		hids := Use[hook]("seg-ids")
		n := len(hids.ids)
		if n > 0 && len(hids.values) == n {
			idx := -1
			for i, id := range hids.ids {
				if IdHasFocus(id) {
					idx = i
					break
				}
			}
			if idx >= 0 {
				delta := 0
				switch GetFrameInput().Key {
				case KeyLeft, KeyUp:
					delta = -1
				case KeyRight, KeyDown:
					delta = 1
				}
				if delta != 0 {
					next := idx + delta
					if next < 0 {
						next = n - 1
					} else if next >= n {
						next = 0
					}
					if next != idx {
						*target = hids.values[next]
						FocusImmediateOn(hids.ids[next])
						changed = true
						GetFrameInput().Key = KeyCodeNone
						RequestNextFrame()
					}
					ShowFocusIndicator()
				}
			}
		}
		if body != nil {
			body()
		}
		hids.ids = append(hids.ids[:0], run.nextIDs...)
		hids.values = append(hids.values[:0], run.nextVals...)
	})
	return changed
}

var currentSegmented any

type segmentedRun[T comparable] struct {
	target                            *T
	changed                           *bool
	style                             SelectionStyle
	focusRing                         Vec4
	padV, minW, padH, endR, labelSize f32
	expand                            bool
	nextIDs                           []ContainerId
	nextVals                          []T
}

// SegmentedCell paints one segment inside the current SegmentedControl body.
// Its selected face sits inside the shared tray.
func SegmentedCell[T comparable](label string, value T) {
	run, ok := currentSegmented.(*segmentedRun[T])
	if !ok || run == nil || run.target == nil {
		panic("widgets: SegmentedCell must be called from SegmentedControl")
	}
	ch, id := segmentOption(run.style, run.focusRing, run.target, value, label, run.endR, run.padV, run.minW, run.padH, run.labelSize, run.expand)
	if ch {
		*run.changed = true
	}
	run.nextIDs = append(run.nextIDs, id)
	run.nextVals = append(run.nextVals, value)
}

// segmentOption paints one inset face and reports selection and identity.
func segmentOption[T comparable](style SelectionStyle, focusRing Vec4, target *T, value T, label string, radius, padV, minW, padH, labelSize f32, expand bool) (bool, ContainerId) {
	changed := false
	var id ContainerId
	cell := Attrs(NoClip, MinWidth(minW), CrossAlign(AlignMiddle), Pad2(padV, padH), Corners(radius))
	if expand {
		cell = AttrsWith(cell, Grow(1))
	}
	ContainerWithKey(value, cell, func() {
		id = CurrentId()
		st := ProcessSegmentEvents(target, value, false)
		NextAccessRole("radio")
		NextAccessChecked(st.Selected)
		AssignAccess()
		changed = st.BecameSelected

		stateStyle := style.Unselected
		weight := WeightNormal
		if st.Selected {
			stateStyle = style.Selected
			weight = WeightBold
		}
		paint := stateStyle.Normal
		if st.Active {
			paint = stateStyle.Pressed
		} else if st.Hovered {
			paint = stateStyle.Hovered
		}
		ModAttrs(BackgroundVec(paint.Background), GradVec(paint.Gradient), func(a *AttrSet) {
			a.Shadow = paint.Shadow
			a.Shadow.Blur = comfort(a.Shadow.Blur)
			a.Shadow.Offset = Vec2{comfort(a.Shadow.Offset[0]), comfort(a.Shadow.Offset[1])}
		})

		if st.Selected {
			ModAttrs(BorderWidth(1), BorderColorVec(paint.Border))
		}
		if st.FocusVisible {
			ModAttrs(BorderWidth(0))
		}

		Filler(1)
		Label(label, FontSize(labelSize), TextColorVec(paint.Indicator), FontWeight(weight))
		Filler(1)
		if st.FocusVisible {
			widgetFocusBorder(GetResolvedSize(), radius, focusRing)
		}
	})
	return changed, id
}
