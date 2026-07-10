// Package qwerty maps hardware key positions to shirei KeyCodes named by
// their US-QWERTY legends, making key identity layout-independent: pressing
// the second key of the top letter row yields KeyW whether the OS layout is
// QWERTY, AZERTY, Dvorak, or Arabic. Layouts are a text concern —
// FrameInput.Text still honors them; key identity does not.
//
// Two code spaces cover all backends: Cocoa's kVK_ANSI_* virtual codes
// (positional by definition), and PS/2 set-1 scancodes — which the Linux
// evdev KEY_* constants deliberately equal for the whole writing block, so
// win32 (lParam scancode), wayland (evdev), and x11 (keycode−8) share one
// table.
package qwerty

import "go.hasen.dev/shirei"

// scanTable: PS/2 set-1 scancode / evdev KEY_* → KeyCode.
var scanTable = map[uint16]shirei.KeyCode{
	// number row
	0x02: '1', 0x03: '2', 0x04: '3', 0x05: '4', 0x06: '5',
	0x07: '6', 0x08: '7', 0x09: '8', 0x0A: '9', 0x0B: '0',
	0x0C: '-', 0x0D: '=',
	// top letter row
	0x10: 'Q', 0x11: 'W', 0x12: 'E', 0x13: 'R', 0x14: 'T',
	0x15: 'Y', 0x16: 'U', 0x17: 'I', 0x18: 'O', 0x19: 'P',
	0x1A: '[', 0x1B: ']',
	// home row
	0x1E: 'A', 0x1F: 'S', 0x20: 'D', 0x21: 'F', 0x22: 'G',
	0x23: 'H', 0x24: 'J', 0x25: 'K', 0x26: 'L',
	0x27: ';', 0x28: '\'', 0x29: '`',
	0x2B: '\\',
	// bottom row
	0x2C: 'Z', 0x2D: 'X', 0x2E: 'C', 0x2F: 'V', 0x30: 'B',
	0x31: 'N', 0x32: 'M',
	0x33: ',', 0x34: '.', 0x35: '/',
}

// macTable: Cocoa kVK_ANSI_* virtual key code → KeyCode.
var macTable = map[uint16]shirei.KeyCode{
	0x00: 'A', 0x01: 'S', 0x02: 'D', 0x03: 'F', 0x04: 'H',
	0x05: 'G', 0x06: 'Z', 0x07: 'X', 0x08: 'C', 0x09: 'V',
	0x0B: 'B', 0x0C: 'Q', 0x0D: 'W', 0x0E: 'E', 0x0F: 'R',
	0x10: 'Y', 0x11: 'T',
	0x12: '1', 0x13: '2', 0x14: '3', 0x15: '4', 0x16: '6',
	0x17: '5', 0x18: '=', 0x19: '9', 0x1A: '7', 0x1B: '-',
	0x1C: '8', 0x1D: '0',
	0x1E: ']', 0x1F: 'O', 0x20: 'U', 0x21: '[', 0x22: 'I',
	0x23: 'P', 0x25: 'L', 0x26: 'J', 0x27: '\'', 0x28: 'K',
	0x29: ';', 0x2A: '\\', 0x2B: ',', 0x2C: '/', 0x2D: 'N',
	0x2E: 'M', 0x2F: '.', 0x32: '`',
}

// FromScan maps a set-1 scancode (Windows) or evdev code (Linux) from the
// keyboard's writing block; KeyCodeNone for anything else.
func FromScan(sc uint16) shirei.KeyCode {
	return scanTable[sc]
}

// FromMacVK maps a Cocoa kVK_ANSI_* virtual key code; KeyCodeNone for
// anything else.
func FromMacVK(vk uint16) shirei.KeyCode {
	return macTable[vk]
}
