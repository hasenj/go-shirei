//go:build darwin && x11darwin

package x11backend

import (
	"github.com/jezek/xgb/xproto"

	"go.hasen.dev/shirei"
)

// X11 IME is Linux/IBus-only. Stubs keep the shared x11input/x11backend code
// compiling under the darwin x11darwin bringup tag. Pending-text helpers still
// work so paste/typed-text accumulation stays correct.

type imeState struct{}

func imeInit()                                        {}
func imeClose()                                       {}
func imeFocusIn()                                     {}
func imeFocusOut()                                    {}
func imeProcessKey(xproto.Keycode, uint16, bool) bool { return false }
func drainIMESignals()                                {}
func updateIMECursor()                                {}
func imeComposing() bool                              { return false }
func commitIMEBeforeClick()                           {}

func appendPendingText(s string) {
	if s != "" {
		currentWindow.pendingText += s
	}
}

func flushPendingText() {
	if currentWindow.pendingText == "" {
		return
	}
	shirei.GetFrameInput().Text += currentWindow.pendingText
	currentWindow.pendingText = ""
}
