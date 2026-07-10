package xkbcommon

// Context is an Opaque top level library context object.
type Context struct {
	cx uintptr
}

// Keymap is an Opaque compiled keymap object.
type Keymap struct {
	km uintptr
}

// State is an Opaque keyboard state object.
type State struct {
	st uintptr
}

// ComposeState is an Opaque Compose state object.
type ComposeState struct {
	cs uintptr
}

// ComposeTable is an Opaque Compose table object.
type ComposeTable struct {
	ct uintptr
}
