package widgets

import (
	"fmt"

	g "go.hasen.dev/generic"
	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
)

var popups = make([]func(), 0, 128)
var _popupsFrameNumber int64

func Popup(fn func()) {
	if FrameNumber > _popupsFrameNumber+1 {
		// PopupsHost was not called; don't do anything (don't leak memory)
		return
	}
	popups = append(popups, fn)
}

func PopupsHost() {
	_popupsFrameNumber = FrameNumber
	for _, p := range popups {
		p()
	}
	g.ResetSlice(&popups)
}

func DebugSelf() {
	// a floating id rect
	myId := CurrentId()
	sz := GetResolvedSize()
	sz[0] -= 2
	sz[1] -= 2
	Layout(TW(Float(1, 1), DBG(120), ClickThrough, FixSizeV(sz)), func() {
		Layout(TW(BG(120, 100, 0, 0.6), BR(1), Pad(1)), func() {
			Label(fmt.Sprintf("%v", myId), Sz(10), Clr(0, 0, 100, 0.9))
		})
	})
}
