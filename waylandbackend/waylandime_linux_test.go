//go:build linux

package waylandbackend

import (
	"testing"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/wayland/textinput"
)

func resetIMEForTest() {
	shirei.ResetInputSession()
	pendingText = ""
	resetPendingIME()
	clearComposition()
	textInputEnabled = false
	textInputOnSurface = false
	textInputSerial = 0
	haveLastCursor = false
}

func TestUTF8ByteOffsetToRuneOffset(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		byteOffset int
		want       int
	}{
		{"bmp japanese", "日本語", 3, 1},             // 日 is 3 bytes
		{"after two jp", "日本語", 6, 2},             // 日本
		{"after non bmp", "a🍣b", 1 + 4, 2},        // a + sushi
		{"after combining", "e\u0301x", 1 + 2, 2}, // e + combining acute
		{"clamp negative", "abc", -1, 0},
		{"clamp past end", "abc", 99, 3},
		{"mid rune clamps back", "日本語", 4, 1}, // mid 本 → start of 本 → rune 1
		{"empty", "", 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := utf8ByteOffsetToRuneOffset(c.text, c.byteOffset); got != c.want {
				t.Fatalf("utf8ByteOffsetToRuneOffset(%q, %d) = %d, want %d",
					c.text, c.byteOffset, got, c.want)
			}
		})
	}
}

func TestUTF8ByteOffsetsToRuneOffsetsHideCaret(t *testing.T) {
	start, end := utf8ByteOffsetsToRuneOffsets("かな", -1, -1)
	if start != 2 || end != 2 {
		t.Fatalf("hide-caret offsets = [%d,%d], want [2,2]", start, end)
	}
}

func TestUTF8ByteOffsetsToRuneOffsetsRange(t *testing.T) {
	// "にほんご" — each hiragana is 3 bytes; select the middle two (ほん).
	text := "にほんご"
	// に=0..3, ほ=3..6, ん=6..9, ご=9..12
	start, end := utf8ByteOffsetsToRuneOffsets(text, 3, 9)
	if start != 1 || end != 3 {
		t.Fatalf("range offsets = [%d,%d], want [1,3]", start, end)
	}
}

func TestAppendPendingTextAccumulates(t *testing.T) {
	resetIMEForTest()

	appendPendingText("a")
	appendPendingText("日")
	if got := shirei.GetFrameInput().Text; got != "" {
		t.Fatalf("wrote FrameInput.Text before flush: %q", got)
	}
	flushPendingText()
	if got := shirei.GetFrameInput().Text; got != "a日" {
		t.Fatalf("flushed text = %q, want %q", got, "a日")
	}
}

func TestFlushPendingTextAppendsToExisting(t *testing.T) {
	resetIMEForTest()

	shirei.GetFrameInput().Text = "paste:"
	appendPendingText("typed")
	flushPendingText()
	if got := shirei.GetFrameInput().Text; got != "paste:typed" {
		t.Fatalf("flushed text = %q, want %q", got, "paste:typed")
	}
}

func TestSetCompositionUTF8(t *testing.T) {
	resetIMEForTest()

	setCompositionUTF8("にほんご", 3, 9) // ほん
	if got := shirei.GetInputState().Composition; got != "にほんご" {
		t.Fatalf("Composition = %q", got)
	}
	if got := shirei.GetInputState().CompositionSel; got != [2]int{1, 3} {
		t.Fatalf("CompositionSel = %v, want [1,3]", got)
	}

	setCompositionUTF8("", 0, 0)
	if shirei.GetInputState().Composition != "" {
		t.Fatalf("empty preedit did not clear composition")
	}
}

func TestDoneAppliesCommitThenPreedit(t *testing.T) {
	resetIMEForTest()

	// Continuous typing: commit the converted word and keep composing.
	h.HandleTextInputCommitString(textinput.CommitStringEvent{Text: "日本語"})
	h.HandleTextInputPreeditString(textinput.PreeditStringEvent{
		Text: "で", CursorBegin: 0, CursorEnd: 3,
	})
	h.HandleTextInputDone(textinput.DoneEvent{Serial: 1})

	if got := shirei.GetInputState().Composition; got != "で" {
		t.Fatalf("Composition = %q, want で", got)
	}
	if got := shirei.GetInputState().CompositionSel; got != [2]int{0, 1} {
		t.Fatalf("CompositionSel = %v, want [0,1]", got)
	}
	flushPendingText()
	if got := shirei.GetFrameInput().Text; got != "日本語" {
		t.Fatalf("committed text = %q, want 日本語", got)
	}
}

func TestDoneCancelClearsComposition(t *testing.T) {
	resetIMEForTest()
	setCompositionUTF8("かな", 0, 6)

	// Escape cancel: explicit empty preedit_string, then done.
	h.HandleTextInputPreeditString(textinput.PreeditStringEvent{Text: ""})
	h.HandleTextInputDone(textinput.DoneEvent{Serial: 2})

	if shirei.GetInputState().Composition != "" {
		t.Fatalf("cancel left composition %q", shirei.GetInputState().Composition)
	}
	flushPendingText()
	if shirei.GetFrameInput().Text != "" {
		t.Fatalf("cancel produced text %q", shirei.GetFrameInput().Text)
	}
}

func TestDoneWithoutTextEventsKeepsComposition(t *testing.T) {
	resetIMEForTest()
	setCompositionUTF8("にほんご", 0, 18)

	// Bare done (cursor-rectangle ack) must not wipe the preedit.
	h.HandleTextInputDone(textinput.DoneEvent{Serial: 3})

	if got := shirei.GetInputState().Composition; got != "にほんご" {
		t.Fatalf("cursor-ack done cleared composition to %q", got)
	}
}

func TestDoneCommitOnlyClearsComposition(t *testing.T) {
	resetIMEForTest()
	setCompositionUTF8("にほんご", 0, 18)

	h.HandleTextInputCommitString(textinput.CommitStringEvent{Text: "日本語"})
	h.HandleTextInputDone(textinput.DoneEvent{Serial: 4})

	if shirei.GetInputState().Composition != "" {
		t.Fatalf("commit-only done left composition %q", shirei.GetInputState().Composition)
	}
	flushPendingText()
	if got := shirei.GetFrameInput().Text; got != "日本語" {
		t.Fatalf("committed text = %q, want 日本語", got)
	}
}
