package window

import (
	"math"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/dragtrace"
)

type dragMoveKey struct{}

type dragMoveState struct {
	active, relative bool
	epoch            int64
	point, origin    shirei.Vec2
}

// DragMove makes the current container a primary-button window drag handle.
// Position is the caller's requested window origin in logical screen points;
// keep it in sync with other position controls. Call every frame inside the
// handle's Container, on the UI thread. It returns true when it requests a move.
//
// Wayland tracks pointer motion independently of asynchronous surface movement,
// including absolute tablet input. It requires a positioned layer window or
// relative-pointer support; ordinary toplevels ignore position requests. Other backends
// keep the grabbed local point under the pointer using their position APIs.
func DragMove(position *shirei.Vec2) bool {
	return dragMove(position, true)
}

// DragMoveNonactivating is DragMove for a window opened with
// PopupWindowOptions.Nonactivating. It starts the drag even while the app is
// inactive, so a floating widget can be moved without taking focus.
func DragMoveNonactivating(position *shirei.Vec2) bool {
	return dragMove(position, false)
}

func dragMove(position *shirei.Vec2, requireActive bool) (moved bool) {
	state := shirei.Use[dragMoveState](dragMoveKey{})
	shirei.PressAction()
	input, frame := shirei.GetInputState(), shirei.GetFrameInput()
	action, sample, source := "unchanged", input.MousePoint, "local"
	if dragtrace.Enabled() && (state.active || shirei.IsActive() || frame.Mouse.Has(shirei.MouseClick) || frame.Mouse.Has(shirei.MouseRelease)) {
		var before shirei.Vec2
		if position != nil {
			before = *position
		}
		activeBefore := state.active
		defer func() {
			dragtrace.Logf("drag.step", "epoch=%d action=%s moved=%t active_before=%t active_after=%t native_active=%t mouse=%v button=%v local=%v motion=%v source=%s sample=%v grabbed=%v start_origin=%v before=%v requested=%v", shirei.InputEpoch(), action, moved, activeBefore, state.active, shirei.IsActive(), frame.Mouse, input.MouseButton, input.MousePoint, frame.Motion, source, sample, state.point, state.origin, before, position)
		}()
	}
	if position == nil || (requireActive && !shirei.IsActive()) || input.MouseButton != shirei.MousePrimary || frame.Mouse.Has(shirei.MouseRelease) {
		state.active = false
		action = "inactive_or_release"
		return false
	}
	if !shirei.IsActive() {
		// Start only after PressAction captured a press on this handle. A newly
		// opened popup can inherit the button that opened it; that must not
		// begin a drag.
		state.active = false
		return false
	}
	if state.active && state.epoch == shirei.InputEpoch() {
		action = "same_epoch"
		return false
	}
	state.epoch = shirei.InputEpoch()
	point := input.MousePoint
	provider, relative := shirei.GetHost().EscapeHatchBackendContext.(interface {
		PointerTravel() (shirei.Vec2, bool)
	})
	if relative {
		source = "travel"
		var available bool
		point, available = provider.PointerTravel()
		sample = point
		if !available {
			action = "travel_unavailable"
			state.active = false
			return false
		}
	}
	if !state.active || frame.Mouse.Has(shirei.MouseClick) || state.relative != relative {
		state.active, state.relative = true, relative
		state.point, state.origin = point, *position
		action = "start"
		return false
	}
	origin := *position
	if relative {
		origin = state.origin
	} else if frame.Motion == (shirei.Vec2{}) {
		action = "no_motion"
		return false
	}
	next := shirei.Vec2Add(origin, shirei.Vec2Sub(point, state.point))
	for axis := range next {
		next[axis] = float32(math.Round(float64(next[axis])))
	}
	if next == *position {
		return false
	}
	action = "request"
	*position = next
	Position(int(next[0]), int(next[1]))
	return true
}
