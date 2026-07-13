//go:build !linux

package x11backend

import (
	"github.com/jezek/xgb/xproto"

	"go.hasen.dev/shirei"
)

// X11 IME is Linux/IBus-only. Stubs keep the shared x11input/x11backend code
// compiling under the darwin x11darwin bringup tag. Pending-text helpers still
// work so paste/typed-text accumulation stays correct.

var pendingText string

func imeInit()  {}
func imeClose() {}
func imeFocusIn()  {}
func imeFocusOut() {}
func imeProcessKey(xproto.Keycode, uint16, bool) bool { return false }
func imeNeedsFrame() bool                             { return false }
func updateIMECursor()                                {}
func imeComposing() bool                              { return false }
func commitIMEBeforeClick()                           {}

func appendPendingText(s string) {
	if s != "" {
		pendingText += s
	}
}

func flushPendingText() {
	if pendingText == "" {
		return
	}
	shirei.FrameInput.Text += pendingText
	pendingText = ""
}
