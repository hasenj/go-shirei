package widgets

import (
	"fmt"
	"math"

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
	Hovered      bool
	Active       bool // pointer captured; value is tracking the pointer
	Disabled     bool
	HasFocus     bool
	FocusVisible bool // paint the keyboard focus indicator

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
// *value (clamped / stepped). Call inside the interactive box before any
// child (it may ModAttrs). Creates no children — paint the track and handle
// yourself from the returned state.
//
// Interaction:
//   - Focus: the container is Focusable; a pointer press takes keyboard
//     focus. Left/Right (and Up/Down) step the value; Home/End jump to min/max.
//     Step size is cfg.Step, or 1/10 of the range when Step is 0.
//   - Touch-drag: value follows the contact X (latched for the contact life
//     so the finger may leave the box while dragging)
//   - Mouse press-drag (Active): value follows pointer X; ignored while
//     MouseFromTouch so a delayed synthetic mouse-up cannot re-engage
//
// Typical custom slider:
//
//	Container(Attrs(FixWidth(w), FixHeight(h)), func() {
//	    st := ProcessSlider(&v, SliderConfig{Min: 0, Max: 1, Width: w, HandleInset: 10})
//	    if st.FocusVisible { ModAttrs(...) }
//	    // paint track / fill / handle from st.T, st.HandleX, st.Active, …
//	})
func ProcessSlider(value *float32, cfg SliderConfig) SliderState {
	var st SliderState
	st.Disabled = cfg.Disabled
	action, requested := ProcessAccessAction(AccessFocus|AccessIncrement|AccessDecrement|AccessSetValue, cfg.Disabled || value == nil)
	st.Min = cfg.Min
	st.Max = cfg.Max
	st.Hovered = IsHovered()
	origin := GetScreenRect().Origin
	st.Local = Vec2Sub(GetInputState().MousePoint, origin)
	if cfg.Disabled {
		ModAttrs(func(a *AttrSet) { a.Focusable = false })
	} else {
		ModAttrs(Focusable)
		FocusOnClick()
	}
	st.HasFocus = HasFocus()
	st.FocusVisible = HasVisibleFocus()

	width := cfg.Width
	if width <= 0 {
		if w := GetResolvedWidth(); w > 1 {
			width = w
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
		} else if st.HasFocus && span > 0 {
			step := cfg.Step
			if step <= 0 {
				step = span / 10
			}
			switch GetFrameInput().Key {
			case KeyLeft, KeyRight, KeyUp, KeyDown, KeyHome, KeyEnd:
				ShowFocusIndicator()
				st.FocusVisible = true
			}
			switch GetFrameInput().Key {
			case KeyLeft, KeyDown:
				*value -= step
			case KeyRight, KeyUp:
				*value += step
			case KeyHome:
				*value = cfg.Min
			case KeyEnd:
				*value = cfg.Max
			}
		}
	}

	if requested && span > 0 {
		step := cfg.Step
		if step <= 0 {
			step = span / 10
		}
		switch action.Kind {
		case AccessIncrement:
			*value += step
		case AccessDecrement:
			*value -= step
		case AccessSetValue:
			if !math.IsNaN(float64(action.Value)) && !math.IsInf(float64(action.Value), 0) {
				*value = action.Value
			}
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
	Accent Vec4 // zero value: use the scheme track color
}

// Slider renders a draggable horizontal slider that reads and writes *value,
// clamped to [Min, Max]. A nonzero Step snaps the value to that increment.
// Thin default chrome over ProcessSlider — for custom faces, call ProcessSlider
// yourself (see demos/custom-sliders).
func Slider(value *float32, attrs SliderAttrs) { SliderExt(value, attrs) }

// SliderExt resolves the scheme and an optional track accent.
func SliderExt(value *float32, attrs SliderAttrs) {
	style := CurrentColorScheme.Slider
	if attrs.Accent != (Vec4{}) {
		style.Track = attrs.Accent
	}
	SliderStyled(value, attrs, style, CurrentColorScheme.FocusRing)
}

// SliderStyled uses literal track, handle, and focus colors. Accent is ignored.
func SliderStyled(value *float32, attrs SliderAttrs, style SliderStyle, focusRing Vec4) {
	if attrs.Width == 0 {
		attrs.Width = 200
	}
	// Height metrics × comfort (track/handle hit size); width stays layout.
	barHeight := comfort(4)
	r := comfort(8) // handle radius
	const focusPad f32 = 3
	height := (r + focusPad) * 2
	Container(Attrs(Row, CrossMid, FixWidth(attrs.Width), FixHeight(height)), func() {
		st := ProcessSlider(value, SliderConfig{
			Min: attrs.Min, Max: attrs.Max, Step: attrs.Step,
			Width: attrs.Width, HandleInset: r + focusPad,
		})
		NextAccessRole("slider")
		if value != nil {
			NextAccessValue(fmt.Sprintf("%g", *value))
			NextAccessRange(*value, attrs.Min, attrs.Max, attrs.Step)
		}
		AssignAccess()
		// Both tracks meet at the handle center, including at the endpoints.
		Element(Attrs(CrossMid, FixSize(attrs.Width, barHeight), BackgroundVec(style.Remainder), Corners(barHeight/2)))
		Element(Attrs(Float(0, (height-barHeight)/2), FixSize(st.HandleX+r+focusPad, barHeight), BackgroundVec(style.Track), Corners(barHeight/2), ClickThrough))
		Container(Attrs(Float(st.HandleX+focusPad, focusPad), FixSize(r*2, r*2), NoClip, ClickThrough), func() {
			Element(Attrs(FixSize(r*2, r*2), Corners(r), BackgroundVec(style.Handle), GradVec(style.HandleGradient), BorderWidth(1), BorderColorVec(style.HandleBorder), ClickThrough))
			if st.FocusVisible {
				widgetFocusOutline(Vec2{r * 2, r * 2}, r, focusRing)
			}
		})
	})
}
