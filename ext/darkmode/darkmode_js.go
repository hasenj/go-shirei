//go:build js

package darkmode

import (
	"syscall/js"
)

var (
	mql         js.Value
	mqlListener js.Func
)

func initPlatform() {
	window := js.Global().Get("window")
	if window.IsUndefined() || window.IsNull() {
		return
	}
	matchMedia := window.Get("matchMedia")
	if matchMedia.IsUndefined() || matchMedia.IsNull() {
		return
	}
	mql = window.Call("matchMedia", "(prefers-color-scheme: dark)")
	if mql.IsUndefined() || mql.IsNull() {
		return
	}

	setDarkMode(mql.Get("matches").Bool())

	mqlListener = js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			matches := args[0].Get("matches")
			if !matches.IsUndefined() && !matches.IsNull() {
				setDarkMode(matches.Bool())
				return nil
			}
		}
		if !mql.IsUndefined() && !mql.IsNull() {
			setDarkMode(mql.Get("matches").Bool())
		}
		return nil
	})

	if !mql.Get("addEventListener").IsUndefined() {
		mql.Call("addEventListener", "change", mqlListener)
	} else if !mql.Get("addListener").IsUndefined() {
		mql.Call("addListener", mqlListener)
	}
}
