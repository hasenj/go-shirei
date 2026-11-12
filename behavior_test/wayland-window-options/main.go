// Window roles, popup options, and background hide/restore with real windows.
//
// go -C behavior_test run ./wayland-window-options --close
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"
	"go.hasen.dev/shirei/behavior_test/hyprprobe"
	"go.hasen.dev/shirei/ext/window"
	. "go.hasen.dev/shirei/widgets"
)

var mode *btmode.Mode
var normalMain = flag.Bool("normal-main", false, "use a normal toplevel main window")
var overlayMain = flag.Bool("overlay", false, "anchor the main window as a full-output overlay")
var migrate = flag.Bool("migrate", false, "inspect moves in both directions between two Hyprland outputs")
var startX = flag.Int("start-x", 0, "initial layer-window desktop X coordinate")
var monitors []hyprprobe.Monitor
var moveRound int
var nativeDetail string
var targets [3]Vec2
var mainScale float32
var scales [3]float32
var started time.Time
var phase, ticks int
var phaseStarted time.Time

func nextPhase(n int) { phase, ticks = n, 0; phaseStarted = time.Now(); fmt.Println("PHASE:", n) }

var phaseDelay = flag.Duration("phase-delay", 200*time.Millisecond, "minimum duration of each driven phase")
var done, ok bool
var detail = "Opening utility and floating windows"
var mainContext BackendContext
var mainFrames, restoreEpoch int
var panels []app.PopupWindow
var contexts []BackendContext
var frames [3]int
var closed [3]int

func finish(success bool, message string) {
	if done {
		return
	}
	done, ok, detail = true, success, message
	if success {
		fmt.Println("PASS:", message)
	} else {
		fmt.Println("FAIL:", message)
	}
	for _, id := range panels {
		app.ClosePopupWindow(id)
	}
}

func main() {
	mode = btmode.RegisterFlags(nil)
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if !waylandAvailable() {
		fmt.Println("SKIP: requires a Wayland session")
		return
	}
	if *migrate {
		var err error
		monitors, err = hyprprobe.Monitors()
		if err != nil || len(monitors) < 2 || *normalMain {
			fmt.Println("FAIL: --migrate requires two Hyprland outputs and a layer main window", err)
			os.Exit(1)
		}
	}
	fmt.Println("=== behavior_test: wayland-window-options ===")
	started = time.Now()
	phaseStarted = started
	app.SetupWindow("behavior_test: Wayland window options", 460, 260)
	app.SetupBackground()
	if *overlayMain {
		app.SetupOverlay()
	} else if !*normalMain {
		app.SetupUndecorated()
	}
	app.SetupQuiet()
	if *startX != 0 {
		window.Position(*startX, 80)
	}
	app.Run(root)
}

func root() {
	mainFrames++
	mainScale = GetHost().WindowScale
	Label("Wayland window options")
	Label(detail)
	RequestNextFrame()
	if !mode.Drive && phase > 0 {
		if Button(NoIcon, "Hide main") {
			closeMain(mainContext)
		}
		return
	}
	advance()
	btmode.VerdictBanner(done, ok, detail)
	mode.TickClose(done, ok)
}

func advance() {
	if mode.Drive && time.Since(started) > 15*time.Second {
		finish(false, "window options timed out: "+nativeDetail)
	}
	if !done {
		ticks++
		switch phase {
		case 0:
			mainContext = GetHost().EscapeHatchBackendContext
			contexts = make([]BackendContext, 3)
			for i := range 3 {
				id, err := app.OpenPopupWindow(app.PopupWindowOptions{
					Title: fmt.Sprintf("Wayland option %d", i), Width: 240, Height: 160,
					Undecorated: i != 2, Transparent: i != 2,
					TopLevel: i == 0, Floating: i != 0, Focus: i == 1,
					ScreenPosition: &Vec2{float32(*startX + 40 + i*260), 80},
					OnClose:        func() { closed[i]++ },
				}, func() {
					contexts[i] = GetHost().EscapeHatchBackendContext
					scales[i] = GetHost().WindowScale
					frames[i]++
					if i > 0 && GetHost().WindowSize != (Vec2{240, 160}) {
						finish(false, fmt.Sprintf("layer content size %v", GetHost().WindowSize))
					}
					Label(fmt.Sprintf("Window %d", i))
					Label("Content area: 240 x 160")
					if Button(NoIcon, "Activate main") {
						window.Activate()
					}
					TextInput(&detail)
					RequestNextFrame()
					if mode.Drive {
						advance()
					}
				})
				if err != nil {
					finish(false, err.Error())
					break
				}
				panels = append(panels, id)
			}
			nextPhase(1)
		case 1:
			if frames[0] < 3 || frames[1] < 3 || frames[2] < 3 || time.Since(phaseStarted) < *phaseDelay {
				break
			}
			if err := checkRoles(mainContext, contexts); err != nil {
				finish(false, err.Error())
				break
			}
			if !inspectMapping(true, false) {
				break
			}
			closeMain(mainContext)
			nextPhase(2)
		case 2:
			if ticks < 8 || time.Since(phaseStarted) < *phaseDelay {
				break
			}
			if !inspectMapping(false, false) {
				break
			}
			for _, count := range closed {
				if count != 0 {
					finish(false, "hiding the owner closes a popup")
				}
			}
			restoreEpoch = mainFrames
			app.ClosePopupWindow(panels[0])
			window.Activate()
			nextPhase(3)
		case 3:
			if mainFrames < restoreEpoch+3 || ticks < 8 || time.Since(phaseStarted) < *phaseDelay {
				break
			}
			if !inspectMapping(true, false) {
				break
			}
			moveWindows()
			nextPhase(4)
		case 4:
			if ticks < 8 || time.Since(phaseStarted) < *phaseDelay {
				break
			}
			if !inspectMapping(true, true) {
				break
			}
			if *migrate && moveRound < 3 {
				moveRound++
				moveWindows()
				nextPhase(4)
				break
			}
			for _, id := range panels {
				app.ClosePopupWindow(id)
			}
			nextPhase(5)
		case 5:
			if closed != ([3]int{1, 1, 1}) {
				break
			}
			finish(true, "top-level/layer roles, content sizes, background UI frames, close callbacks; "+nativeDetail)
		}
	}
}

func moveWindows() {
	for i, id := range panels {
		targets[i] = Vec2{float32(60 + i*260), 120}
		if *migrate {
			m := monitors[(moveRound+1)%2]
			targets[i] = Vec2{float32(m.X + 40 + i*260), float32(m.Y + 100)}
			if moveRound >= 2 {
				targets[i] = Vec2{float32(m.X + m.Width - 20), float32(m.Y + m.Height - 20)}
			}
		}
		app.MovePopupWindow(id, &targets[i])
	}
	if *migrate {
		positionMain(mainContext, targets[0])
	}
}

// Mapping and geometry are read from the compositor, independently of queued
// backend requests and UI frame counts. Other compositors report this coverage absent.
func inspectMapping(mainMapped, moved bool) bool {
	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") == "" {
		nativeDetail = "compositor mapping/placement inspection unavailable"
		return true
	}
	layers, err := hyprprobe.Layers()
	if err != nil {
		nativeDetail = err.Error()
		return false
	}
	mainCount := 0
	for _, l := range layers {
		if l.W == 460 && l.H == 260 {
			mainCount++
		}
	}
	if *normalMain {
		clients, err := hyprprobe.Clients()
		if err != nil {
			nativeDetail = err.Error()
			return false
		}
		c := clients["behavior_test: Wayland window options"]
		if (c.Mapped && !c.Hidden) != mainMapped {
			nativeDetail = "main toplevel mapping mismatch"
			return false
		}
	} else if mainCount != map[bool]int{true: 1, false: 0}[mainMapped] {
		nativeDetail = fmt.Sprintf("main layer mapping mismatch: %v", layers)
		return false
	}
	if moved {
		for i := 1; i < 3; i++ {
			height := 160
			if i == 2 {
				height += 34
			}
			x, y := int(targets[i][0]), int(targets[i][1])
			output := ""
			if *migrate {
				m := monitors[(moveRound+1)%2]
				x = min(x, m.X+m.Width-240)
				y = min(y, m.Y+m.Height-height)
				output = m.Name
			}
			found := false
			for _, l := range layers {
				if l.W == 240 && l.H == height && l.X == x && l.Y == y && (output == "" || l.Output == output) {
					found = true
				}
			}
			if !found {
				nativeDetail = fmt.Sprintf("panel %d placement want %d,%d on %s; layers=%v", i, x, y, output, layers)
				return false
			}
		}
		if *migrate {
			m := monitors[(moveRound+1)%2]
			expectedScale := float32(m.Scale)
			if os.Getenv("SHIREI_FRACTIONAL_SCALE") == "0" {
				expectedScale = float32(math.Ceil(m.Scale))
			}
			if mainScale != expectedScale || scales[1] != expectedScale || scales[2] != expectedScale {
				nativeDetail = fmt.Sprintf("scale mismatch: main=%g panels=%v want=%g", mainScale, scales, expectedScale)
				return false
			}
			x, y := int(targets[0][0]), int(targets[0][1])
			if !*overlayMain {
				x = min(x, m.X+m.Width-460)
				y = min(y, m.Y+m.Height-260)
			}
			found := false
			for _, l := range layers {
				if l.W == 460 && l.H == 260 && l.X == x && l.Y == y && l.Output == m.Name {
					found = true
				}
			}
			if !found {
				nativeDetail = fmt.Sprintf("main migration placement mismatch: %v", layers)
				return false
			}
		}
	}
	nativeDetail = "Hyprland confirms hide/restore mapping and final placement"
	return true
}
