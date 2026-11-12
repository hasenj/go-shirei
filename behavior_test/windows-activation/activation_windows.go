package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"

	g "go.hasen.dev/generic"
	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"
	"go.hasen.dev/shirei/ext/window"
	. "go.hasen.dev/shirei/widgets"
)

var user32 = syscall.NewLazyDLL("user32.dll")
var sendMessage = user32.NewProc("SendMessageW")
var showWindow = user32.NewProc("ShowWindow")
var isVisible = user32.NewProc("IsWindowVisible")
var isIconic = user32.NewProc("IsIconic")
var isZoomed = user32.NewProc("IsZoomed")
var getFocus = user32.NewProc("GetFocus")
var getForeground = user32.NewProc("GetForegroundWindow")

var mode *btmode.Mode
var mainHWND uintptr
var mainFrames, popupFrames, beforeActivate, phase int
var opened, done, ok bool
var phaseStarted time.Time
var watchdog *time.Timer
var detail = "Opening a popup"

func run(m *btmode.Mode) {
	if *overlayPeer {
		runOverlayPeer()
		return
	}
	mode = m
	fmt.Println("=== behavior_test: windows-activation ===")
	if *overlay {
		if err := startOverlayPeer(); err != nil {
			fmt.Println("FAIL:", err)
			os.Exit(1)
		}
		g.AddExitCleanup(stopOverlayPeer)
		phase = -2
	}
	app.SetupWindow("Activation: main window", 480, 240)
	app.SetupBackground()
	if *overlay {
		app.SetupOverlay()
	}
	if *quiet {
		app.SetupQuiet()
	}
	if mode.Drive {
		watchdog = time.AfterFunc(20*time.Second, func() {
			fmt.Println("FAIL: main-window activation timed out")
			stopOverlayPeer()
			os.Exit(1)
		})
	}
	phaseStarted = time.Now()
	app.Run(func() {
		mainHWND = uintptr(GetHost().EscapeHatchBackendContext.(interface{ HWND() unsafe.Pointer }).HWND())
		mainFrames++
		ModAttrs(Pad(20), Gap(12))
		Label("Main window", FontSize(22))
		Label("The popup restores this window after hiding or minimizing it.")
		if !opened && mainFrames >= 3 {
			opened = true
			_, err := app.OpenPopupWindow(app.PopupWindowOptions{
				Title: "Activation: popup", Width: 480, Height: 320,
				TopLevel: true, Focus: false,
			}, popupView)
			if err != nil {
				fmt.Println("FAIL:", err)
				stopOverlayPeer()
				os.Exit(1)
			}
		}
		RequestNextFrame()
	})
}

func popupView() {
	popupFrames++
	if popupFrames == 1 {
		phaseStarted = time.Now()
	}
	ModAttrs(Pad(20), Gap(12))
	Label("Activate main from this popup", FontSize(20))
	Label(detail)
	if !mode.Drive {
		if *overlay && Button(NoIcon, "Activate other application") {
			user32.NewProc("SetForegroundWindow").Call(peerHWND)
		}
		if Button(NoIcon, "Close main into background") {
			sendMessage.Call(mainHWND, 0x10, 0, 0) // WM_CLOSE
		}
		if Button(NoIcon, "Minimize main") {
			showWindow.Call(mainHWND, 6) // SW_MINIMIZE
		}
		if Button(NoIcon, "Maximize main") {
			showWindow.Call(mainHWND, 3) // SW_MAXIMIZE
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
		if !done && mainFrames >= 3 && popupFrames >= 3 && time.Since(phaseStarted) >= 200*time.Millisecond {
			advance()
		}
		btmode.VerdictBanner(done, ok, detail)
		mode.TickClose(done, ok)
	}
	RequestNextFrame()
}

func finish(success bool, message string) {
	done, ok, detail = true, success, message
	if watchdog != nil {
		watchdog.Stop()
	}
	stopOverlayPeer()
	if success {
		fmt.Println("PASS:", message)
	} else {
		fmt.Println("FAIL:", message)
	}
}

func advance() {
	visible, _, _ := isVisible.Call(mainHWND)
	minimized, _, _ := isIconic.Call(mainHWND)
	maximized, _, _ := isZoomed.Call(mainHWND)
	switch phase {
	case -2:
		if err := checkOverlay(); err != nil {
			finish(false, err.Error())
			return
		}
		if *quiet {
			popupHWND := uintptr(GetHost().EscapeHatchBackendContext.(interface{ HWND() unsafe.Pointer }).HWND())
			foreground, _, _ := getForeground.Call()
			if foreground == mainHWND || foreground == popupHWND {
				finish(false, fmt.Sprintf("quiet overlay startup activates a Shirei window (foreground=%d, main=%d, popup=%d)", foreground, mainHWND, popupHWND))
				return
			}
			fmt.Println("PASS: quiet overlay renders without taking foreground")
		}
		user32.NewProc("BringWindowToTop").Call(peerHWND)
		user32.NewProc("SetForegroundWindow").Call(peerHWND)
	case -1:
		if foreground, _, _ := getForeground.Call(); foreground != peerHWND {
			finish(false, "other application does not become foreground for the stacking check")
			return
		}
		if err := checkOverlay(); err != nil {
			finish(false, err.Error())
			return
		}
		fmt.Println("PASS: overlay stays above the foreground window of another process")
	case 0, 6:
		if phase == 0 && !*overlay && !*quiet {
			focus, _, _ := getFocus.Call()
			if focus != mainHWND {
				finish(false, "non-activating utility window takes keyboard focus")
				return
			}
			fmt.Println("PASS: non-activating utility window preserves main-window focus")
		}
		if phase == 6 && maximized == 0 {
			finish(false, "activation unmaximizes the main window")
			return
		}
		sendMessage.Call(mainHWND, 0x10, 0, 0) // WM_CLOSE exercises background lifetime.
	case 1, 7:
		if visible != 0 {
			finish(false, "background close leaves the main window visible")
			return
		}
		beforeActivate = mainFrames
		if phase == 1 {
			window.Activate() // Runs with the popup's UI/context active.
		} else {
			go window.Activate()
		}
	case 2, 4, 8:
		if visible == 0 || minimized != 0 {
			finish(false, fmt.Sprintf("phase %d: activation does not restore the main window", phase))
			return
		}
		if mainFrames <= beforeActivate {
			return // Require a new visible main-window frame, not just ShowWindow.
		}
		if *overlay {
			if err := checkOverlay(); err != nil {
				finish(false, err.Error())
				return
			}
		}
		if *requireFocus {
			focus, _, _ := getFocus.Call()
			foreground, _, _ := getForeground.Call()
			if focus != mainHWND || foreground != mainHWND {
				finish(false, fmt.Sprintf("phase %d: main window does not have native focus", phase))
				return
			}
		}
		if phase == 2 {
			fmt.Println("PASS: popup restores hidden main window")
			showWindow.Call(mainHWND, 6) // SW_MINIMIZE
		} else if phase == 4 {
			fmt.Println("PASS: popup restores minimized main window")
			showWindow.Call(mainHWND, 3) // SW_MAXIMIZE
		} else {
			finish(true, "main-window restore from popup and goroutine; maximized size preserved")
			return
		}
	case 3:
		if minimized == 0 {
			finish(false, "main window is not minimized before activation")
			return
		}
		beforeActivate = mainFrames
		window.Activate()
	case 5:
		if maximized == 0 {
			finish(false, "main window is not maximized before activation")
			return
		}
		window.Activate()
		// The next phase checks the result after the message loop handles it.
	}
	phase++
	phaseStarted = time.Now()
}
