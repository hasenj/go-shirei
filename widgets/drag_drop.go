package widgets

import (
	"fmt"

	. "go.hasen.dev/shirei"
)

type _DragDropData struct {
	// Dragging is true only after the pointer has moved past the drag
	// threshold. Until then a press is "armed" (see Armed).
	Dragging bool
	Armed    bool

	// DraggingId is the dragged widget's identity (ContainerId). Used to
	// recognize the same container across frames while the pointer moves.
	DraggingId ContainerId

	// DraggingItem / DropTarget are caller-supplied payloads (see
	// DragAndDrop / CanDropHere), not reconciliation keys. Callers
	// type-assert them via GetDraggingItem[T] / GetDropTarget[T].
	DraggingItem any
	DropTarget   any

	// for ghost item rendering!
	ItemSize  Vec2
	ItemFloat Vec2

	// ArmPoint is the pointer position when the press was armed; used to
	// decide when motion counts as a drag rather than a click.
	ArmPoint Vec2
}

var draggingState _DragDropData

// Drag threshold in logical pixels (same order as DoubleClickSlop). A press
// that never exceeds this is a click / double-click, not a drag.
const dragThreshold float32 = 6

// DragAndDrop starts/continues a drag for the current container.
// payload is captured on mouse-down only; later frames may pass the same
// value for call-site convenience. Returns true on successful drop.
//
// Dragging only begins after the pointer moves past a small threshold, so
// plain clicks and double-clicks are not swallowed by DnD. Double-click
// presses (ClickCount >= 2) do not arm a drag at all.
func DragAndDrop(payload any) bool {
	var id = CurrentId()

	// Fresh press: arm a potential drag, unless this is a double-click.
	if !draggingState.Dragging && !draggingState.Armed && IsHovered() {
		if FrameInput.Mouse == MouseClick {
			if FrameInput.ClickCount >= 2 {
				// Leave the event for IsDoubleClicked handlers.
				return false
			}
			draggingState = _DragDropData{
				Armed:        true,
				DraggingId:   id,
				DraggingItem: payload,
				ItemFloat:    GetRenderData().ResolvedOrigin,
				ItemSize:     GetRenderData().ResolvedSize,
				ArmPoint:     InputState.MousePoint,
			}
			return false
		}
	}

	if draggingState.DraggingId != id {
		return false
	}

	// Armed but not yet dragging: promote on movement, cancel on release.
	if draggingState.Armed && !draggingState.Dragging {
		if FrameInput.Mouse == MouseRelease {
			draggingState = _DragDropData{}
			return false
		}
		d := Vec2Sub(InputState.MousePoint, draggingState.ArmPoint)
		if d[0]*d[0]+d[1]*d[1] >= dragThreshold*dragThreshold {
			draggingState.Dragging = true
			draggingState.Armed = false
			// Include the threshold motion in the ghost position.
			draggingState.ItemFloat = Vec2Add(draggingState.ItemFloat, FrameInput.Motion)
		}
		return false
	}

	if draggingState.Dragging {
		draggingState.ItemFloat = Vec2Add(draggingState.ItemFloat, FrameInput.Motion)
		if FrameInput.Mouse == MouseRelease {
			// DropTarget remains readable this frame for GetDropTarget after
			// a true return; the next press resets full state.
			draggingState.Dragging = false
			draggingState.Armed = false
			return draggingState.DropTarget != nil
		}
	}

	return false
}

// CanDropHere registers this container as a drop zone while hovered, but only
// if the active drag payload is of type Accept. target is what GetDropTarget
// will return.
func CanDropHere[Accept any](target any) bool {
	if !draggingState.Dragging {
		return false
	}
	var _, valid = draggingState.DraggingItem.(Accept)
	if !valid {
		return false
	}

	if IsHovered() {
		draggingState.DropTarget = target
	} else {
		// unset ourselves as the drop target if we were set that way!
		if draggingState.DropTarget == target {
			draggingState.DropTarget = nil
		}
	}

	return draggingState.DropTarget == target
}

// IsDragging reports whether the current container is the item currently being
// dragged (past the movement threshold).
func IsDragging() bool {
	return draggingState.Dragging && draggingState.DraggingId == CurrentId()
}

// IsDropTarget reports whether target is the active drop target this frame.
// Use it to draw insertion markers outside the container that called CanDropHere
// (e.g. a gap between list items) without re-registering the zone.
func IsDropTarget(target any) bool {
	return draggingState.Dragging && draggingState.DropTarget == target
}

// GetDropTarget returns the data registered by the drop zone currently under the
// dragged item, as T. It returns the zero T (with a warning) when there is no
// target or its type doesn't match.
func GetDropTarget[T any]() T {
	var target, ok = draggingState.DropTarget.(T)
	if !ok {
		fmt.Println("WARNING: invalid drop target!")
	}
	return target
}

// GetDraggingItem returns the payload of the item currently being dragged as T,
// along with whether a drag is in progress and the payload is of type T.
func GetDraggingItem[T any]() (T, bool) {
	var zero T
	if !draggingState.Dragging {
		return zero, false
	}
	value, ok := draggingState.DraggingItem.(T)
	return value, ok
}

// GetDraggingItemRect returns the screen rectangle (floating position and size)
// of the item currently being dragged, or the zero Rect if nothing is dragging.
func GetDraggingItemRect() Rect {
	if !draggingState.Dragging {
		return Rect{}
	}
	return Rect{
		Origin: draggingState.ItemFloat,
		Size:   draggingState.ItemSize,
	}
}
