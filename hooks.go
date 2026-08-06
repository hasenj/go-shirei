package shirei

// hooks allow UI builders to associate custom (arbitrary) state with the current view

// HookEntryKey identifies a piece of hooked side data by the object it is
// attached to and a caller-supplied item key.
type HookEntryKey struct {
	Data    any // container id
	ItemKey any
}

// Use returns a pointer to per-container state of type T, keyed by itemKey and
// retained across frames on the current container's identity node. It is
// zero-valued on first use (and re-initialized after any frame in which it went
// untouched). This is Shirei's React-like local component state; use
// UseWithInit to supply a custom initializer.
func Use[T any](itemKey any) *T {
	return UseWithInit[T](itemKey, nil)
}

func UseWithDefault[T any](itemKey any, initial T) *T {
	var initFn = func() *T {
		v := new(T)
		*v = initial
		return v
	}
	return UseWithInit(itemKey, initFn)
}

// UI hook state lives on the container's identity node (stage 3; see
// identity.go). Retention is prune-per-frame, preserving the old double-
// buffered map's semantics: a slot is live if it was used last frame (or
// created this frame); one full unused frame and it reads as absent, so
// the next use re-initializes it.
func UseWithInit[T any](itemKey any, initFn func() *T) *T {
	n := ui.current.node
	slot, found := n.hooks[itemKey] // nil-map read is safe
	if found && slot.frame >= ui.FrameNumber-1 {
		slot.frame = ui.FrameNumber
		n.hooks[itemKey] = slot
		return slot.value.(*T)
	}

	var newValue any
	if initFn != nil {
		newValue = initFn()
	} else {
		newValue = new(T)
	}
	if n.hooks == nil {
		n.hooks = make(map[any]hookSlot)
	}
	n.hooks[itemKey] = hookSlot{value: newValue, frame: ui.FrameNumber}
	return newValue.(*T)
}

// data hooks, unlike ui hooks, do not disappear when you don't use them in a frame
var dataHooks = make(map[HookEntryKey]any)

// UseData attaches side state of type T to an arbitrary object, keyed by the
// (data, itemKey) pair. Unlike Use, data hooks persist whether or not they are
// touched each frame; call DeleteHookedData to release one.
func UseData[T any](data any, itemKey any) *T {
	var key = HookEntryKey{Data: data, ItemKey: itemKey}
	value, found := dataHooks[key]
	if !found {
		newValue := new(T)
		value = newValue
		dataHooks[key] = value
	}
	return value.(*T)
}

// DeleteHookedData releases the data-hook state that UseData created for the
// given (data, itemKey) pair.
func DeleteHookedData(data any, itemKey any) {
	var key = HookEntryKey{Data: data, ItemKey: itemKey}
	delete(dataHooks, key)
}
