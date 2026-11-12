// Ordinary-window sizing and compositor constraints on two native windows.
//
// go -C shirei/behavior_test run ./wayland-window-sizing --close
package main

import (
	"flag"
	"fmt"
	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"
	"go.hasen.dev/shirei/behavior_test/hyprprobe"
	"go.hasen.dev/shirei/ext/window"
	. "go.hasen.dev/shirei/widgets"
	"os"
	"time"
)

var mode *btmode.Mode
var inspectHypr = os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != ""
var phase int
var since, started time.Time
var done, ok bool
var detail = "Opening ordinary windows"
var sizes [2]Vec2
var contexts [2]BackendContext
var popup app.PopupWindow
var closed int
var policyLimited bool
var policySizes [2]Vec2
var titles = [2]string{"Wayland sizing main", "Wayland sizing auxiliary"}

func main() {
	mode = btmode.RegisterFlags(nil)
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		panic(err)
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		fmt.Println("SKIP: requires Wayland")
		return
	}
	fmt.Println("=== behavior_test: wayland-window-sizing ===")
	started, since = time.Now(), time.Now()
	app.SetupWindow(titles[0], 400, 260)
	window.SetSize(420, 280)
	app.Run(func() {
		contexts[0] = GetHost().EscapeHatchBackendContext
		sizes[0] = GetHost().WindowSize
		Label(detail)
		if mode.Drive && !done {
			advance()
		}
		if !mode.Drive && Button(NoIcon, "Resize to 500 x 300") {
			window.SetSize(500, 300)
		}
		RequestNextFrame()
		btmode.VerdictBanner(done, ok, detail)
		mode.TickClose(done, ok)
	})
}

func finish(success bool, message string) {
	done, ok, detail = true, success, message
	if success {
		fmt.Println("PASS:", message)
	} else {
		fmt.Println("FAIL:", message)
	}
	app.ClosePopupWindow(popup)
}

func next(message string) {
	phase++
	since = time.Now()
	detail = message
	fmt.Println("PHASE:", phase, message)
}

func advance() {
	if time.Since(started) > 25*time.Second {
		finish(false, fmt.Sprintf("phase %d timeout: content %v", phase, sizes))
		return
	}
	if phase == 0 {
		var err error
		popup, err = app.OpenPopupWindow(app.PopupWindowOptions{Title: titles[1], Width: 240, Height: 160, TopLevel: true, Undecorated: true, OnClose: func() { closed++ }}, func() {
			contexts[1] = GetHost().EscapeHatchBackendContext
			sizes[1] = GetHost().WindowSize
			Label("Independent ordinary window")
			RequestNextFrame()
		})
		if err != nil {
			finish(false, err.Error())
			return
		}
		next("Float only the two test windows")
		return
	}
	if time.Since(since) < 500*time.Millisecond {
		return
	}
	if contexts[1] == nil {
		return
	}
	var clients map[string]hyprprobe.Client
	if inspectHypr {
		var err error
		clients, err = hyprprobe.Clients()
		if err != nil {
			finish(false, err.Error())
			return
		}
	}
	a, b := clients[titles[0]], clients[titles[1]]
	if inspectHypr && phase < 12 && (!a.Mapped || !b.Mapped || a.Hidden || b.Hidden) {
		return
	}
	check := func(wantA, wantB Vec2) bool {
		// Hyprland negotiates server-side decoration on the main window.
		return sizes == ([2]Vec2{wantA, wantB}) && (!inspectHypr || (a.Size == ([2]int{int(wantA[0]), int(wantA[1])}) && b.Size == ([2]int{int(wantB[0]), int(wantB[1])})))
	}
	switch phase {
	case 1:
		if !inspectHypr {
			next("Startup size on a floating compositor")
			return
		}
		if err := hyprprobe.Dispatch("setfloating", a); err != nil {
			finish(false, err.Error())
			return
		}
		if err := hyprprobe.Dispatch("setfloating", b); err != nil {
			finish(false, err.Error())
			return
		}
		next("Inspect startup size after compositor placement")
	case 2:
		if inspectHypr && (!a.Floating || !b.Floating) {
			return
		}
		if sizeConstrained(contexts[0]) || sizeConstrained(contexts[1]) {
			policyLimited, policySizes = true, sizes
			for i, ctx := range contexts {
				if sizeConstrained(ctx) {
					resizeOther(ctx, sizes[i][0]+117, sizes[i][1]+53)
				}
			}
			fmt.Println("SKIP: floating client-sizing transitions; compositor reports controlled sizes")
			phase = 99
			next("Compositor-controlled sizes remain authoritative")
			return
		}
		if !inspectHypr && sizes[0] != (Vec2{420, 280}) {
			return
		}
		if inspectHypr {
			fmt.Printf("INFO: startup settled content=%v native=%v (compositor opening rules can override startup size)\n", sizes[0], a.Size)
		} else {
			fmt.Printf("PASS: startup content size %v\n", sizes[0])
		}
		window.SetMinSize(300, 180)
		window.SetSize(510, 330)
		resizeOther(contexts[1], 270, 190)
		next("Runtime size requests remain independent")
	case 3:
		if !check(Vec2{510, 330}, Vec2{270, 190}) {
			return
		}
		window.SetSize(100, 100)
		next("Minimum content dimensions clamp client requests")
	case 4:
		if !check(Vec2{300, 180}, Vec2{270, 190}) {
			return
		}
		constrain(contexts[0], "maximize")
		next("Maximize")
	case 5:
		if configuredState(contexts[0]) != 1 {
			return
		}
		window.SetSize(430, 290)
		next("SetSize is discarded while maximized")
	case 6:
		if configuredState(contexts[0]) != 1 || sizes[0] == (Vec2{430, 290}) {
			finish(false, "client size overrides maximize")
			return
		}
		constrain(contexts[0], "unmaximize")
		next("Restore without replaying discarded size")
	case 7:
		if configuredState(contexts[0]) != 0 {
			return
		}
		if sizes[0] == (Vec2{430, 290}) {
			finish(false, "discarded maximized size reappears after restore")
			return
		}
		window.SetSize(430, 290)
		next("Fresh size request after restore")
	case 8:
		if !check(Vec2{430, 290}, Vec2{270, 190}) {
			return
		}
		constrain(contexts[0], "fullscreen")
		next("Fullscreen")
	case 9:
		if configuredState(contexts[0]) != 2 {
			return
		}
		window.SetSize(440, 300)
		next("SetSize is discarded while fullscreen")
	case 10:
		if configuredState(contexts[0]) != 2 || sizes[0] == (Vec2{440, 300}) {
			finish(false, "client size overrides fullscreen")
			return
		}
		constrain(contexts[0], "unfullscreen")
		next("Restore from fullscreen")
	case 11:
		if configuredState(contexts[0]) != 0 {
			return
		}
		if sizes[0] == (Vec2{440, 300}) {
			finish(false, "discarded fullscreen size reappears after restore")
			return
		}
		window.SetSize(440, 300)
		next("Fresh size request after fullscreen")
	case 12:
		if !check(Vec2{440, 300}, Vec2{270, 190}) {
			return
		}
		if !inspectHypr {
			fmt.Println("INFO: tiling and independent compositor geometry inspection unavailable")
			app.ClosePopupWindow(popup)
			phase = 16
			next("Auxiliary closed")
			return
		}
		if err := hyprprobe.Dispatch("settiled", a); err != nil {
			finish(false, err.Error())
			return
		}
		next("Tiled window discards client size")
	case 13:
		if a.Floating {
			return
		}
		window.SetSize(450, 310)
		next("Check tiled constraint")
	case 14:
		if sizes[0] == (Vec2{450, 310}) {
			finish(false, "client size overrides tiling")
			return
		}
		if err := hyprprobe.Dispatch("setfloating", a); err != nil {
			finish(false, err.Error())
			return
		}
		next("Restore without replaying tiled size")
	case 15:
		if !a.Floating || sizeConstrained(contexts[0]) {
			return
		}
		if sizes[0] == (Vec2{450, 310}) {
			finish(false, "discarded tiled size reappears after restore")
			return
		}
		window.SetSize(450, 310)
		next("Fresh floating size and close callback")
	case 16:
		if !check(Vec2{450, 310}, Vec2{270, 190}) {
			return
		}
		app.ClosePopupWindow(popup)
		next("Auxiliary closed")
	case 17:
		if closed != 1 {
			return
		}
		coverage := "startup/runtime content sizes, minimum, independent windows, maximize/fullscreen and close callback"
		if policyLimited {
			coverage = "compositor-controlled sizes preserved and close callback; floating client sizing not exercised"
		} else if inspectHypr {
			coverage += "; tiling and compositor dimensions"
		}
		finish(true, coverage)
	case 100:
		if sizes != policySizes {
			finish(false, fmt.Sprintf("client request changes controlled size: %v -> %v", policySizes, sizes))
			return
		}
		app.ClosePopupWindow(popup)
		phase = 16
		next("Auxiliary closed")
	}
}
