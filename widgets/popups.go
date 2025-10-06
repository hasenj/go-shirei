package widgets

import (
	"fmt"
	"log"

	g "go.hasen.dev/generic"
	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
)

var popups = make([]func(), 0, 128)

func Popup(fn func()) {
	if len(popups) > 100 {
		log.Println("WARNING: make sure to call `PopupsHost` at the bottom of your main view function")
	}
	popups = append(popups, fn)
}

func PopupsHost() {
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
