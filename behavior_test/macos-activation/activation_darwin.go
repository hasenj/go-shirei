//go:build darwin && !ios && !x11darwin

package main

import (
	"fmt"
	"os"
	"time"
	"unsafe"

	"github.com/ebitengine/purego/objc"
	g "go.hasen.dev/generic"
	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"
	"go.hasen.dev/shirei/ext/window"
	. "go.hasen.dev/shirei/widgets"
)

func run(mode *btmode.Mode) {
	fmt.Println("=== behavior_test: macos-activation ===")
	var mainWindow objc.ID
	var opened, done, ok bool
	var phase int
	var watchdog *time.Timer
	detail := "Opening a utility window"
	phaseTime := time.Now()
	s := objc.RegisterName
	finish := func(success bool, message string) {
		done, ok, detail = true, success, message
		if watchdog != nil {
			watchdog.Stop()
		}
		if success {
			fmt.Println("PASS:", message)
		} else {
			fmt.Println("FAIL:", message)
		}
	}
	if mode.Drive {
		watchdog = time.AfterFunc(20*time.Second, func() {
			fmt.Println("FAIL: activation timed out")
			os.Exit(1)
		})
		g.AddExitCleanup(func() {
			if !done || !ok {
				fmt.Println("FAIL: app exits before background restoration completes")
				os.Exit(1)
			}
		})
	}
	app.SetupWindow("Activation: main window", 480, 300)
	app.SetupBackground()
	app.Run(func() {
		mainWindow = objc.ID(uintptr(GetHost().EscapeHatchBackendContext.(interface{ NSWindow() unsafe.Pointer }).NSWindow()))
		ModAttrs(Pad(20), Gap(12))
		Label("Main window", FontSize(22))
		Label("The utility window restores this window.")
		if !opened {
			opened = true
			_, err := app.OpenPopupWindow(app.PopupWindowOptions{
				Title: "Activation: utility window", Width: 500, Height: 320, TopLevel: true,
			}, func() {
				ModAttrs(Pad(20), Gap(12))
				Label("Activate main from this popup", FontSize(20))
				Label(detail)
				if !mode.Drive {
					if Button(NoIcon, "Close main into background") {
						mainWindow.Send(s("performClose:"), objc.ID(0))
					}
					if Button(NoIcon, "Minimize main") {
						mainWindow.Send(s("miniaturize:"), objc.ID(0))
					}
					if Button(NoIcon, "Activate main") {
						window.Activate()
					}
					if Button(NoIcon, "Activate from goroutine") {
						go window.Activate()
					}
					if Button(NoIcon, "Quit") {
						app.Quit()
					}
				} else {
					if !done && time.Since(phaseTime) > 350*time.Millisecond {
						visible := objc.Send[bool](mainWindow, s("isVisible"))
						mini := objc.Send[bool](mainWindow, s("isMiniaturized"))
						switch phase {
						case 0:
							if *requireFocus && !objc.Send[bool](mainWindow, s("isKeyWindow")) {
								finish(false, "utility window steals main-window focus")
								break
							}
							mainWindow.Send(s("performClose:"), objc.ID(0))
						case 1:
							if visible {
								finish(false, "background close leaves main visible")
								break
							}
							window.Activate()
						case 2, 4, 6:
							if !visible || mini {
								finish(false, "activation does not restore main")
								break
							}
							if *requireFocus && !objc.Send[bool](mainWindow, s("isKeyWindow")) {
								finish(false, "activation targets the popup instead of main")
								break
							}
							if phase == 2 {
								fmt.Println("PASS: popup restores background main window")
								mainWindow.Send(s("miniaturize:"), objc.ID(0))
							} else if phase == 4 {
								fmt.Println("PASS: popup restores minimized main window")
								mainWindow.Send(s("performClose:"), objc.ID(0))
							} else {
								finish(true, "background goroutine restores main window; popup remains live")
							}
						case 3:
							if !mini {
								finish(false, "main is not minimized before activation")
								break
							}
							window.Activate()
						case 5:
							if visible {
								finish(false, "second background close leaves main visible")
								break
							}
							go window.Activate()
						}
						phase++
						phaseTime = time.Now()
					}
					btmode.VerdictBanner(done, ok, detail)
					mode.TickClose(done, ok)
				}
				RequestNextFrame()
			})
			if err != nil {
				finish(false, err.Error())
				os.Exit(1)
			}
		}
		RequestNextFrame()
	})
}
