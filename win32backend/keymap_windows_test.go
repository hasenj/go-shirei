//go:build windows

package win32backend

import (
	"testing"
	"unicode/utf16"

	"go.hasen.dev/shirei"
)

// lparamFor builds a WM_KEYDOWN lParam carrying the given scancode (bits
// 16-23) and extended flag (bit 24).
func lparamFor(scancode uint16, extended bool) uintptr {
	lp := uintptr(scancode) << 16
	if extended {
		lp |= 1 << 24
	}
	return lp
}

// TestMapKeyPositional pins the positional contract: the writing block
// resolves by scancode, so KeyW is the physical key at the US-QWERTY W
// position even when the layout remaps virtual keys (AZERTY's physical Q
// position sends VK_A but scancode 0x10 — it must resolve as KeyQ).
func TestMapKeyPositional(t *testing.T) {
	cases := []struct {
		vk       uint32
		scancode uint16
		want     shirei.KeyCode
	}{
		{'W', 0x11, 'W'},  // US layout: agreement
		{'Z', 0x11, 'W'},  // AZERTY: physical W position sends VK_Z
		{'A', 0x10, 'Q'},  // AZERTY: physical Q position sends VK_A
		{0xBA, 0x27, ';'}, // VK_OEM_1 varies by layout; the position doesn't
		{'5', 0x06, '5'},
	}
	for _, c := range cases {
		if got := mapKey(c.vk, lparamFor(c.scancode, false)); got != c.want {
			t.Errorf("mapKey(vk %#x, sc %#x) = %q, want %q", c.vk, c.scancode, got, c.want)
		}
	}

	// extended keys must skip the scancode table: the numpad Enter shares
	// scancode 0x1C-with-extended-bit and must resolve by VK, not position
	if got := mapKey(vkReturn, lparamFor(0x1C, true)); got != shirei.KeyEnter {
		t.Errorf("extended numpad enter = %q, want KeyEnter", got)
	}
	// arrows are extended too; VK path must still serve them
	if got := mapKey(vkLeft, lparamFor(0x4B, true)); got != shirei.KeyLeft {
		t.Errorf("extended left arrow = %q, want KeyLeft", got)
	}
}

func TestOnKeyIgnoresVKProcessKey(t *testing.T) {
	shirei.ResetInputSession()

	onKey(vkProcesskey, lparamFor(0x1C, true), true)

	if got := shirei.GetFrameInput().Key; got != shirei.KeyCodeNone {
		t.Fatalf("FrameInput.Key = %q, want none", got)
	}
	if len(shirei.GetInputState().DownKeys) != 0 {
		t.Fatalf("DownKeys = %v, want empty", shirei.GetInputState().DownKeys)
	}
}

func resetTextInputForTest() {
	shirei.ResetInputSession()
	pendingHi = 0
	pendingText = ""
}

func TestOnCharAccumulatesPendingText(t *testing.T) {
	resetTextInputForTest()

	onChar('a')
	onChar('b')

	if got := shirei.GetFrameInput().Text; got != "" {
		t.Fatalf("onChar wrote FrameInput.Text before flush: %q", got)
	}
	flushPendingText()
	if got := shirei.GetFrameInput().Text; got != "ab" {
		t.Fatalf("flushed text = %q, want %q", got, "ab")
	}
}

func TestOnCharReassemblesSurrogatePair(t *testing.T) {
	resetTextInputForTest()

	onChar(0xD83C)
	flushPendingText()
	if got := shirei.GetFrameInput().Text; got != "" {
		t.Fatalf("high surrogate flushed text = %q, want empty", got)
	}

	onChar(0xDF63)
	flushPendingText()
	if got := shirei.GetFrameInput().Text; got != "🍣" {
		t.Fatalf("surrogate pair flushed text = %q, want 🍣", got)
	}
}

func TestFlushPendingTextAppendsToExistingFrameText(t *testing.T) {
	resetTextInputForTest()

	shirei.GetFrameInput().Text = "paste:"
	appendPendingText("typed")
	flushPendingText()

	if got := shirei.GetFrameInput().Text; got != "paste:typed" {
		t.Fatalf("flushed text = %q, want %q", got, "paste:typed")
	}
}

func TestUTF16UnitOffsetToRuneOffset(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		unitOffset int
		want       int
	}{
		{"bmp japanese", "日本語", 2, 2},
		{"after non bmp", "a🍣b", 3, 2},
		{"after combining mark", "e\u0301x", 2, 2},
		{"clamp negative", "abc", -1, 0},
		{"clamp past end", "abc", 99, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u16 := utf16.Encode([]rune(c.text))
			if got := utf16UnitOffsetToRuneOffset(u16, c.unitOffset); got != c.want {
				t.Fatalf("utf16UnitOffsetToRuneOffset(%q, %d) = %d, want %d",
					c.text, c.unitOffset, got, c.want)
			}
		})
	}
}

func TestSetCompositionUTF16PlacesCursorAtEnd(t *testing.T) {
	resetTextInputForTest()

	u16 := utf16.Encode([]rune("a🍣b"))
	setCompositionUTF16(u16)

	if got := shirei.GetInputState().Composition; got != "a🍣b" {
		t.Fatalf("composition = %q, want %q", got, "a🍣b")
	}
	if got := shirei.GetInputState().CompositionSel; got != [2]int{3, 3} {
		t.Fatalf("composition sel = %v, want [3 3]", got)
	}
}

func TestCandidatePointScalesLogicalCaret(t *testing.T) {
	got := candidatePoint(shirei.Vec2{12.25, 7.5}, 2)
	if got != (win32Point{X: 25, Y: 15}) {
		t.Fatalf("candidatePoint = %+v, want {X:25 Y:15}", got)
	}

	got = candidatePoint(shirei.Vec2{2.6, 3.4}, 0)
	if got != (win32Point{X: 3, Y: 3}) {
		t.Fatalf("candidatePoint default scale = %+v, want {X:3 Y:3}", got)
	}
}
