package slay

// hooks allow UI builders to associate custom (arbitrary) state with the current view

type HookEntryKey struct {
	Data    any // container id
	ItemKey any
}

// any hook key that is not used this frame will be removed to avoid the accumulation of garbage

var hooksMap = make(map[HookEntryKey]any)
var hooksMapNext = make(map[HookEntryKey]any)

func Use[T any](itemKey any) *T {
	return UseWithInit[T](itemKey, nil)
}

func UseWithInit[T any](itemKey any, initFn func() *T) *T {
	var key = HookEntryKey{Data: CurrentId(), ItemKey: itemKey}
	var value, found = hooksMap[key]
	if found {
		hooksMapNext[key] = value
		return value.(*T)
	} else {
		var zero = new(T)
		var newValue any
		if initFn != nil {
			newValue = initFn()
		} else {
			newValue = zero
		}
		hooksMap[key] = newValue
		hooksMapNext[key] = newValue
		return newValue.(*T)
	}
}

// data hooks, unlike ui hooks, do not disappear when you don't use them in a frame
var dataHooks = make(map[HookEntryKey]any)

// Hook side data to any object
// FIXME perhaps this does not really belong to SLAY
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

func DeleteHookedData(data any, itemKey any) {
	var key = HookEntryKey{Data: data, ItemKey: itemKey}
	delete(dataHooks, key)
}
