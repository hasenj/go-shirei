//go:build darwin

package cocoabackend

import (
	"slices"
	"testing"

	"go.hasen.dev/shirei"
)

// TestMapVKeyLayoutIndependent pins the positional contract: the physical W
// key (kVK_ANSI_W = 0x0D) resolves to KeyW no matter what character the
// active layout would type — here the Arabic layout's "ص". Before the
// positional tables, the character fallback returned KeyCodeNone for any
// non-ASCII layout, which made letter keys dead for shortcuts and note keys.
func TestMapVKeyLayoutIndependent(t *testing.T) {
	cases := []struct {
		vk   uint16
		bare string // what charactersIgnoringModifiers yields in some layout
		want shirei.KeyCode
	}{
		{0x0D, "w", 'W'},  // US layout
		{0x0D, "ص", 'W'},  // Arabic
		{0x0D, ",", 'W'},  // Dvorak (physical W types a comma)
		{0x0D, "z", 'W'},  // AZERTY
		{0x29, "ك", ';'},  // Arabic on the ; key
		{0x12, "١", '1'},  // Arabic-Indic digit on the 1 key
		{0x2A, "ù", '\\'}, // some AZERTY variants
	}
	for _, c := range cases {
		if got := mapVKey(c.vk, c.bare); got != c.want {
			t.Errorf("mapVKey(%#x, %q) = %q, want %q", c.vk, c.bare, got, c.want)
		}
	}

	// keys outside the tables still fall back to the typed character
	if got := mapVKey(0x5E /* kVK_JIS_Underscore */, "_"); got != shirei.KeyCode('_') {
		t.Errorf("fallback for unmapped vk = %q, want '_'", got)
	}
}

// TestIsPrintableRejectsFunctionKeys pins the text-relay filter: NSEvent
// reports function keys (arrows, Home/End, page keys, F1-F12, forward
// delete) as private-use code points in the reserved 0xF700-0xF8FF range.
// isPrintable let those through once, so every arrow press ALSO inserted an
// invisible glyphless rune into the focused text input — corrupting the
// buffer and making the caret render at end of line whenever the cursor
// landed on one (the "arrow key jumps to end" bug).
func TestIsPrintableRejectsFunctionKeys(t *testing.T) {
	reject := []string{
		"\uF700",                   // up arrow
		"\uF702",                   // left arrow
		"\uF703",                   // right arrow
		"\uF704",                   // F1
		"\uF728",                   // forward delete
		"\uF729",                   // home
		"\uF72B",                   // end
		"\uF72C",                   // page up
		"",                         // empty
		"\r", "\t", "\x7f", "\x1b", // control chars (backspace/enter/tab/escape)
	}
	for _, s := range reject {
		if isPrintable(s) {
			t.Errorf("isPrintable(%q) = true, want false", s)
		}
	}
	accept := []string{"a", " ", "ص", "日", "🙂"}
	for _, s := range accept {
		if !isPrintable(s) {
			t.Errorf("isPrintable(%q) = false, want true", s)
		}
	}
}

func TestKeyDownDoesNotRelayPrintableText(t *testing.T) {
	shirei.ResetInputSession()
	keyDown(0x00, "a") // kVK_ANSI_A

	if shirei.FrameInput.Text != "" {
		t.Fatalf("keyDown relayed printable text %q; insertText should be the only committed-text path",
			shirei.FrameInput.Text)
	}
	if shirei.FrameInput.Key != shirei.KeyA {
		t.Fatalf("keyDown key = %q, want %q", shirei.FrameInput.Key, shirei.KeyA)
	}
	if !slices.Contains(shirei.InputState.DownKeys, shirei.KeyA) {
		t.Fatalf("keyDown did not add KeyA to DownKeys: %v", shirei.InputState.DownKeys)
	}
}

func TestCommittedTextAccumulatesBeforeFrame(t *testing.T) {
	shirei.ResetInputSession()
	pendingText = ""
	pendingPaste = ""
	hasPendingPaste = false

	queueCommittedText("a")
	queueCommittedText("日")
	queueCommittedText("\uF703") // private-use arrow key marker: not committed text
	flushPendingFrameText()

	if got := shirei.FrameInput.Text; got != "a日" {
		t.Fatalf("committed text = %q, want %q", got, "a日")
	}
	if pendingText != "" {
		t.Fatalf("pendingText after flush = %q, want empty", pendingText)
	}
}

func TestUTF16CompositionOffsetsBecomeRuneOffsets(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		startUTF16 int
		endUTF16   int
		want       [2]int
	}{
		{
			name:       "plain ascii",
			text:       "abc",
			startUTF16: 1,
			endUTF16:   3,
			want:       [2]int{1, 3},
		},
		{
			name:       "non bmp before clause",
			text:       "🍣入力",
			startUTF16: 2,
			endUTF16:   4,
			want:       [2]int{1, 3},
		},
		{
			name:       "combining mark remains separate rune",
			text:       "a\u0301入力",
			startUTF16: 2,
			endUTF16:   4,
			want:       [2]int{2, 4},
		},
		{
			name:       "emoji plus combining before clause",
			text:       "🍣a\u0301入力",
			startUTF16: 4,
			endUTF16:   6,
			want:       [2]int{3, 5},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shirei.ResetInputSession()
			setCompositionFromUTF16Offsets(c.text, c.startUTF16, c.endUTF16)
			if got := shirei.InputState.Composition; got != c.text {
				t.Fatalf("composition = %q, want %q", got, c.text)
			}
			if got := shirei.InputState.CompositionSel; got != c.want {
				t.Fatalf("CompositionSel = %v, want %v", got, c.want)
			}
		})
	}
}
