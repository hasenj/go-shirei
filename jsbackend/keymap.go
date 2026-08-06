package jsbackend

import "go.hasen.dev/shirei"

// KeyboardEvent.code (UI Events) is positional US-QWERTY legend — maps
// directly onto shirei.KeyCode the way Cocoa kVK_ANSI_* does on macOS.
// Meta/OS keys are not in this table: mapCode picks KeyCommand vs KeySuper
// from the browser host OS (Apple → Command).
var codeToKey = map[string]shirei.KeyCode{
	"KeyA": shirei.KeyA, "KeyB": shirei.KeyB, "KeyC": shirei.KeyC, "KeyD": shirei.KeyD,
	"KeyE": shirei.KeyE, "KeyF": shirei.KeyF, "KeyG": shirei.KeyG, "KeyH": shirei.KeyH,
	"KeyI": shirei.KeyI, "KeyJ": shirei.KeyJ, "KeyK": shirei.KeyK, "KeyL": shirei.KeyL,
	"KeyM": shirei.KeyM, "KeyN": shirei.KeyN, "KeyO": shirei.KeyO, "KeyP": shirei.KeyP,
	"KeyQ": shirei.KeyQ, "KeyR": shirei.KeyR, "KeyS": shirei.KeyS, "KeyT": shirei.KeyT,
	"KeyU": shirei.KeyU, "KeyV": shirei.KeyV, "KeyW": shirei.KeyW, "KeyX": shirei.KeyX,
	"KeyY": shirei.KeyY, "KeyZ": shirei.KeyZ,

	"Digit0": shirei.Key0, "Digit1": shirei.Key1, "Digit2": shirei.Key2, "Digit3": shirei.Key3,
	"Digit4": shirei.Key4, "Digit5": shirei.Key5, "Digit6": shirei.Key6, "Digit7": shirei.Key7,
	"Digit8": shirei.Key8, "Digit9": shirei.Key9,

	"ArrowLeft": shirei.KeyLeft, "ArrowRight": shirei.KeyRight,
	"ArrowUp":   shirei.KeyUp, "ArrowDown": shirei.KeyDown,

	"Enter": shirei.KeyEnter, "NumpadEnter": shirei.KeyEnter,
	"Escape": shirei.KeyEscape, "Tab": shirei.KeyTab, "Space": shirei.KeySpace,
	"Backspace": shirei.KeyDeleteBackward, "Delete": shirei.KeyDeleteForward,
	"Home": shirei.KeyHome, "End": shirei.KeyEnd,
	"PageUp": shirei.KeyPageUp, "PageDown": shirei.KeyPageDown,
	"Insert": shirei.KeyInsert,

	"ShiftLeft": shirei.KeyShift, "ShiftRight": shirei.KeyShift,
	"ControlLeft": shirei.KeyCtrl, "ControlRight": shirei.KeyCtrl,
	"AltLeft": shirei.KeyAlt, "AltRight": shirei.KeyAlt,

	"F1": shirei.KeyF1, "F2": shirei.KeyF2, "F3": shirei.KeyF3, "F4": shirei.KeyF4,
	"F5": shirei.KeyF5, "F6": shirei.KeyF6, "F7": shirei.KeyF7, "F8": shirei.KeyF8,
	"F9": shirei.KeyF9, "F10": shirei.KeyF10, "F11": shirei.KeyF11, "F12": shirei.KeyF12,
}

// mapCode resolves KeyboardEvent.code to a positional KeyCode.
// appleHost selects Command (macOS/iOS browsers) vs Super (Windows/Linux)
// for MetaLeft/MetaRight/OSLeft/OSRight — matching Cocoa KeyCommand.
func mapCode(code string, appleHost bool) shirei.KeyCode {
	switch code {
	case "MetaLeft", "MetaRight", "OSLeft", "OSRight":
		if appleHost {
			return shirei.KeyCommand
		}
		return shirei.KeySuper
	}
	return codeToKey[code]
}
