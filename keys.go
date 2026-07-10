package shirei

// KeyCode identifies a physical key by its US-QWERTY legend, independent of
// the active keyboard layout: KeyW is the second key of the top letter row
// whether the layout is QWERTY, AZERTY, Dvorak, or Arabic. Layouts are a
// text-input concern — they drive FrameInput.Text, not key identity — so
// note keys, game keys, and shortcut combos like Cmd+C stay on the physical
// positions users' hands know. Backends translate their native positional
// codes (Cocoa kVK_ANSI_*, Win32 scancodes, evdev) via internal/qwerty.
type KeyCode byte

const (
	Key0 KeyCode = '0' + iota
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9
)

const (
	// ascii table order
	KeyA KeyCode = 'A' + iota
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ
)

const (
	KeyCodeNone KeyCode = iota

	KeyLeft = 128 + iota
	KeyRight
	KeyUp
	KeyDown
	KeyEnter
	KeyEscape
	KeyHome
	KeyEnd
	KeyDeleteBackward
	KeyDeleteForward
	KeyPageUp
	KeyPageDown
	KeyTab
	KeySpace
	KeyCtrl
	KeyShift
	KeyAlt
	KeySuper
	KeyCommand

	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	KeyBack
)

// KeyCombo is a key together with the modifier keys held with it — the unit
// matched against keyboard shortcuts.
type KeyCombo struct {
	Key KeyCode
	Mod Modifiers
}

// Combo builds a KeyCombo from a key and its modifiers.
func Combo(key KeyCode, mod Modifiers) KeyCombo {
	return KeyCombo{
		Key: key,
		Mod: mod,
	}
}

// ActiveCombo returns the key pressed this frame together with the currently
// held modifiers, ready to compare against a shortcut Combo.
func ActiveCombo() KeyCombo {
	return KeyCombo{
		Key: FrameInput.Key,
		Mod: InputState.Modifiers,
	}
}
