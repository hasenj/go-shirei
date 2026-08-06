package widgets

import (
	"go.hasen.dev/generic"
	. "go.hasen.dev/shirei"
)

// SliderConfig is the interaction/math configuration for ProcessSlider.
// Presentation (track fill, handle shape) is the caller's job.
type SliderConfig struct {
	Min, Max f32 // value range; if Max <= Min, value is pinned to Min
	Step     f32 // snap increment; 0 means continuous

	// Width is the full control width in pixels (outer box). Zero: 200, or
	// the current container's resolved width when already known (> 1).
	Width f32

	// HandleInset is the padding from each end so the handle center stays on
	// the track (typically half the handle's width). Zero defaults to 8
	// (default circular handle radius). Travel length is Width - 2*HandleInset.
	HandleInset f32

	Disabled bool
}

// SliderState is one frame's snapshot from ProcessSlider for paint.
type SliderState struct {
	Hovered  bool
	Active   bool // pointer captured; value is tracking the pointer
	Disabled bool

	// Value is *value after step/clamp this frame.
	Value float32
	// T is Value normalized to [0, 1] along [Min, Max] (0 if span is zero).
	T float32

	// Local is the pointer position relative to the control's top-left.
	Local Vec2

	// TrackLen is the handle travel distance (Width - 2*HandleInset).
	TrackLen    float32
	HandleInset float32
	// HandleX is the left edge of the handle in local coordinates (use as
	// Float X for a handle of width 2*HandleInset, or center at HandleX+inset).
	HandleX float32

	// Width is the full control width used for math this frame.
	Width float32
	Min   float32
	Max   float32
}

// ProcessSlider runs slider interaction on the current container and writes
// *value (clamped / stepped). Call inside the interactive box (typically
// Focusable); it does not take keyboard focus. Creates no children — paint
// the track and handle yourself from the returned state.
//
// Interaction:
//   - Touch-drag: value follows the contact X (latched for the contact life
//     so the finger may leave the box while dragging)
//   - Mouse press-drag (Active): value follows pointer X; ignored while
//     MouseFromTouch so a delayed synthetic mouse-up cannot re-engage
//
// Typical custom slider:
//
//	Container(Attrs(FixWidth(w), FixHeight(h), Focusable), func() {
//	    st := ProcessSlider(&v, SliderConfig{Min: 0, Max: 1, Width: w, HandleInset: 10})
//	    // paint track / fill / handle from st.T, st.HandleX, st.Active, …
//	})
func ProcessSlider(value *float32, cfg SliderConfig) SliderState {
	var st SliderState
	st.Disabled = cfg.Disabled
	st.Min = cfg.Min
	st.Max = cfg.Max
	st.Hovered = IsHovered()
	origin := GetScreenRect().Origin
	st.Local = Vec2Sub(GetInputState().MousePoint, origin)

	width := cfg.Width
	if width <= 0 {
		if sz := GetResolvedSize(); sz[0] > 1 {
			width = sz[0]
		} else if sz := GetScreenRect().Size; sz[0] > 1 {
			width = sz[0]
		} else {
			width = 200
		}
	}
	inset := cfg.HandleInset
	if inset <= 0 {
		inset = 8
	}
	track := width - inset*2
	if track < 1 {
		track = 1
	}
	st.Width = width
	st.TrackLen = track
	st.HandleInset = inset

	if value == nil {
		return st
	}

	span := cfg.Max - cfg.Min

	// Latched contact id while a finger is dragging the slider (0 = none).
	// Lives on the container so tracking continues when the finger leaves
	// the box, until that contact ends.
	type dragHook struct {
		touchId uint32
	}
	drag := Use[dragHook](0)

	if !cfg.Disabled {
		// Prefer raw touch (multi-touch / mobile). Latch the first contact
		// that hits this control and track it until lift — no focus steal.
		if drag.touchId != 0 {
			if ti, ok := TouchById(drag.touchId); ok {
				st.Local = Vec2Sub(ti.Pos, origin)
				st.Active = true
			} else {
				drag.touchId = 0
			}
		}
		if drag.touchId == 0 && IsTouched() {
			ids := TouchingIds(nil)
			if len(ids) > 0 {
				if ti, ok := TouchById(ids[0]); ok {
					drag.touchId = ids[0]
					st.Local = Vec2Sub(ti.Pos, origin)
					st.Active = true
				}
			}
		}

		// Mouse path only when a real mouse (or not currently from a finger).
		if drag.touchId == 0 && !GetInputState().MouseFromTouch {
			PressAction()
			if IsActive() {
				st.Local = Vec2Sub(GetInputState().MousePoint, origin)
				st.Active = true
			}
		}

		if st.Active {
			x := st.Local[0] - inset
			t := x / track
			generic.Clamp(0, &t, 1)
			*value = cfg.Min + span*t
		}
	}

	if cfg.Step > 0 {
		*value = Roundf32(*value/cfg.Step) * cfg.Step
	}
	if span <= 0 {
		*value = cfg.Min
	} else {
		generic.Clamp(cfg.Min, value, cfg.Max)
	}

	st.Value = *value
	if span <= 0 {
		st.T = 0
		st.HandleX = 0
	} else {
		st.T = (*value - cfg.Min) / span
		generic.Clamp(0, &st.T, 1)
		st.HandleX = track * st.T
	}
	return st
}

// SliderAttrs configures the default Slider chrome.
type SliderAttrs struct {
	Min    f32  // value at the left end of the track
	Max    f32  // value at the right end of the track
	Step   f32  // snap increment; 0 means continuous
	Width  f32  // control width in pixels; 0 uses a default
	Accent Vec4 // zero value: use the package-level Accent
}

// Slider renders a draggable horizontal slider that reads and writes *value,
// clamped to [Min, Max]. A nonzero Step snaps the value to that increment.
// Thin default chrome over ProcessSlider — for custom faces, call ProcessSlider
// yourself (see demos/custom-sliders).
func Slider(value *float32, attrs SliderAttrs) {
	if attrs.Width == 0 {
		attrs.Width = 200
	}
	accent := AccentOrFallback(attrs.Accent, DefaultAccent)
	// Height metrics × comfort (track/handle hit size); width stays layout.
	barHeight := comfort(4)
	r := comfort(8) // handle radius
	height := r * 2
	Container(Attrs(Row, CrossMid, FixWidth(attrs.Width), Focusable, FixHeight(height)), func() {
		st := ProcessSlider(value, SliderConfig{
			Min: attrs.Min, Max: attrs.Max, Step: attrs.Step,
			Width: attrs.Width, HandleInset: r,
		})
		// Track (full width visual bar).
		Element(Attrs(CrossMid, MinSize(attrs.Width, barHeight), BackgroundVec(accent), Corners(barHeight/2)))
		// Handle (ClickThrough so drag is owned by the outer process box).
		Element(Attrs(
			Float(st.HandleX, 0),
			Corners(r),
			ClickThrough,
			FixSize(r*2, r*2),
			Background(0, 0, 100, 1),
			Grad(0, 0, -16, 0),
			BorderWidth(1),
			BorderColor(0, 0, 0, 0.5),
		))
	})
}
