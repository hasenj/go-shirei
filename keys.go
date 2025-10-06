package shirei

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

type KeyCombo struct {
	Key KeyCode
	Mod Modifiers
}

func Combo(key KeyCode, mod Modifiers) KeyCombo {
	return KeyCombo{
		Key: key,
		Mod: mod,
	}
}

func ActiveCombo() KeyCombo {
	return KeyCombo{
		Key: FrameInput.Key,
		Mod: InputState.Modifiers,
	}
}
