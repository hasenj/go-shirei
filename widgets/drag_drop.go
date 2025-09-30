package widgets

import (
	"fmt"

	. "go.hasen.dev/slay"
)

type DragDropData struct {
	Dragging     bool
	DraggingItem any
	DropTarget   any

	// for ghost item rendering!
	ItemSize  Vec2
	ItemFloat Vec2
}

var draggingState DragDropData

func GetDraggingState() DragDropData {
	if draggingState.Dragging {
		return draggingState
	} else {
		return DragDropData{}
	}
}

// returns true when dropping
func DragAndDrop() bool {
	var item = CurrentId()
	if !draggingState.Dragging && IsHovered() {
		if FrameInput.Mouse == MouseClick {
			draggingState = DragDropData{}
			draggingState.Dragging = true
			draggingState.DraggingItem = item
			draggingState.ItemFloat = GetRenderData().ResolvedOrigin
			draggingState.ItemSize = GetRenderData().ResolvedSize
		}
	} else if draggingState.Dragging && draggingState.DraggingItem == item {
		draggingState.ItemFloat = Vec2Add(draggingState.ItemFloat, FrameInput.Motion)
		if FrameInput.Mouse == MouseRelease {
			draggingState.Dragging = false

			// this block signifies the mouse was let go! see where we are being dropped
			return draggingState.DropTarget != nil
		}
	}
	return false
}

// returns true if something is being dragged over us!
func CanDropHere[T any]() bool {
	var target = CurrentId()
	if !draggingState.Dragging {
		return false
	}
	var _, valid = draggingState.DraggingItem.(T)
	if !valid {
		return false
	}

	if IsHovered() {
		draggingState.DropTarget = target
	} else {
		// unset ourselves as the target lane if we were set that way!
		if draggingState.DropTarget == target {
			draggingState.DropTarget = nil
		}
	}

	return draggingState.DropTarget == target
}

func IsDragging() bool {
	return draggingState.Dragging && draggingState.DraggingItem == CurrentId()
}

func GetDropTarget[T any]() T {
	var target, ok = draggingState.DropTarget.(T)
	if !ok {
		fmt.Println("WARNING: invalid drop target!")
	}
	return target
}

func GetDraggingItem[T any]() (T, bool) {
	var zero T
	if !draggingState.Dragging {
		return zero, false
	}
	value, ok := draggingState.DraggingItem.(T)
	return value, ok
}

func GetDraggingItemRect() Rect {
	if !draggingState.Dragging {
		return Rect{}
	}
	return Rect{
		Origin: draggingState.ItemFloat,
		Size:   draggingState.ItemSize,
	}
}
