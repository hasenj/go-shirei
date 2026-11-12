// Native popup lifecycle and isolation with real window presentation.
//
//	go -C behavior_test run ./popup-windows
//	go -C behavior_test run ./popup-windows --close
//	go -C behavior_test run ./popup-windows --manual
//	go -C behavior_test run ./popup-windows --owner-exit
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"
	"go.hasen.dev/shirei/ext/window"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

var (
	mode                           *btmode.Mode
	undecoratedMain                = flag.Bool("undecorated", false, "use an undecorated main window")
	requireLayerShell              = flag.Bool("require-layer-shell", false, "require layer-shell for undecorated Wayland windows")
	requireGPU                     = flag.Bool("require-gpu", false, "fail if a window uses software rendering")
	timeout                        = flag.Duration("timeout", 20*time.Second, "maximum automatic lifecycle duration")
	quiet                          = flag.Bool("quiet", false, "keep application focus unchanged")
	callbackExit                   = flag.Bool("close-owner-from-callback", false, "close the main window from a popup close callback")
	ownerExit                      = flag.Bool("owner-exit", false, "verify main-window close runs descendant callbacks and exits")
	started                        time.Time
	step, rounds, serial           int
	stepTime                       time.Time
	done, ok                       bool
	detail                         = "Opening all four styles"
	mainUI                         *UI
	mainFirstEpoch                 int64
	mainEpoch                      int64
	mainFrames                     int
	mainContext                    BackendContext
	panels                         []*panel
	batch                          []*panel
	parent, child, early, reopened *panel
	exitPanels                     []*panel
	exitRequested                  bool
	mainText                       = "main"
)

type panel struct {
	id                       app.PopupWindow
	tag                      int
	transparent, undecorated bool
	text                     string
	ui                       *UI
	context                  BackendContext
	ownerContext             BackendContext
	epoch                    int64
	frames, closed           int
	inputReadyAt             time.Time
	menuClicks               int
	checked                  bool
	spawnChild               bool
	onClose                  func()
}

func main() {
	mode = btmode.RegisterFlags(nil)
	flag.Parse()
	mode.AfterParse()
	if *callbackExit {
		*ownerExit = true
	}
	if *ownerExit {
		mode.Close = true
	}
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println("=== behavior_test: popup-windows ===")
	started, stepTime = time.Now(), time.Now()
	app.SetupWindow("behavior_test: popup-windows", 600, 350)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		app.SetupWindow("behavior_test: popup-windows", 400, 250)
	}
	window.SetMinSize(560, 310)
	window.SetSize(600, 350)
	window.Center()
	window.Position(120, 160)
	if *undecoratedMain {
		app.SetupUndecorated()
	}
	if *quiet {
		app.SetupQuiet()
	}
	g.AddExitCleanup(func() {
		if !exitRequested {
			return
		}
		for _, p := range exitPanels {
			if p.closed != 1 {
				fmt.Println("FAIL: main close skips panel callback")
				os.Exit(1)
			}
		}
		fmt.Println("PASS: main-window shutdown closes descendants exactly once")
	})
	app.Run(root)
}

func fail(message string) {
	if done && !ok {
		return
	}
	done, ok, detail = true, false, message
	fmt.Println("FAIL:", message)
	for _, p := range panels {
		app.ClosePopupWindow(p.id)
	}
}

func openPanel(transparent, undecorated bool) *panel {
	serial++
	p := &panel{tag: serial, transparent: transparent, undecorated: undecorated, ownerContext: GetHost().EscapeHatchBackendContext}
	id, err := app.OpenPopupWindow(app.PopupWindowOptions{
		Title: fmt.Sprintf("Panel %d", p.tag), Width: 320, Height: 240,
		Transparent: transparent, Undecorated: undecorated,
		Position: &Vec2{float32((serial%4)*100 - 160), float32((serial % 3) * 60)},
		OnClose: func() {
			p.closed++
			if p.onClose != nil {
				p.onClose()
			}
			RequestNextFrame()
		},
	}, p.view)
	if err != nil {
		fail(err.Error())
		return p
	}
	p.id = id
	panels = append(panels, p)
	return p
}

func openBatch() {
	batch = nil
	for i := 0; i < 4; i++ {
		batch = append(batch, openPanel(i&1 != 0, i&2 != 0))
	}
}

func root() {
	mainUI, mainContext = ActiveUI(), GetHost().EscapeHatchBackendContext
	if mainEpoch != InputEpoch() {
		mainEpoch = InputEpoch()
		mainFrames++
	}
	ModAttrs(Pad(24), Gap(14), Background(220, 15, 96, 1), NoAnimate)
	Label("Native popup windows", FontSize(22))
	Label(detail, FontSize(13))
	TextInput(&mainText)
	Container(Attrs(Row, Gap(10)), func() {
		if Button(NoIcon, "Open four styles") {
			openBatch()
		}
		if Button(NoIcon, "Close panels") {
			for _, p := range panels {
				app.ClosePopupWindow(p.id)
			}
		}
	})
	Label(fmt.Sprintf("Panels created: %d · completed rounds: %d", serial, rounds), FontSize(13))
	if mode.Drive && !done {
		if !platformSupported {
			_, err := app.OpenPopupWindow(app.PopupWindowOptions{Width: 100, Height: 100}, func() {})
			if err == nil {
				fail("unsupported backend accepts popup")
			} else {
				done, ok, detail = true, true, "Backend reports unsupported popups"
			}
		} else {
			drive()
		}
		RequestNextFrame()
	}
	if mode.Drive {
		btmode.VerdictBanner(done, ok, detail)
		if *ownerExit && ok && platformSupported {
			if len(exitPanels) == 0 {
				parent = openPanel(false, false)
				parent.spawnChild = true
				child = nil
				exitPanels = append(exitPanels, parent)
			}
			if child != nil && child.checked && parent.checked && !exitRequested {
				exitPanels = append(exitPanels, child)
				exitRequested = true

				if *callbackExit {
					trigger := openPanel(false, true)
					trigger.onClose = func() { closeNative(mainContext) }
					exitPanels = append(exitPanels, trigger)
					app.ClosePopupWindow(trigger.id)
				} else {
					closeNative(mainContext)
				}
			}
			RequestNextFrame()
		} else {
			mode.TickClose(done, ok)
		}
	}
}

func drive() {
	if mainFrames < 3 {
		return // Let native configure/resize events settle before observing geometry.
	}
	if time.Since(started) > *timeout {
		fail("timed out waiting for popup lifecycle")
		return
	}
	if mainText != "main" || ToastCount() != 0 || !mainSizeOK() {
		fail("main window input, toast, size, or native role is incorrect")
		return
	}
	if *requireGPU && step > 0 && InputEpoch() != mainFirstEpoch && !GetHost().PaintGPU {
		fail("main window uses software rendering")
		return
	}
	switch step {
	case 0:
		mainFirstEpoch = InputEpoch()
		preferTabs()
		if _, err := app.OpenPopupWindow(app.PopupWindowOptions{Width: 0, Height: 20}, func() {}); err == nil {
			fail("invalid dimensions accepted")
			return
		}
		openBatch()
		parent = batch[0]
		parent.spawnChild = true
		early = openPanel(true, true)
		early.onClose = func() {
			reopened = openPanel(true, false)
			app.ClosePopupWindow(reopened.id)
		}
		app.ClosePopupWindow(early.id)
		app.ClosePopupWindow(early.id)
		step = 1
	case 1:
		if child == nil || !child.checked {
			return
		}
		for _, p := range batch {
			if !p.checked {
				return
			}
		}
		if early.frames != 0 || early.closed != 1 || reopened == nil || reopened.frames != 0 || reopened.closed != 1 {
			fail("close-before-first-frame fails")
			return
		}
		for i, p := range batch {
			if p.ui == mainUI {
				fail("panel shares main UI")
				return
			}
			for _, q := range batch[:i] {
				if p.ui == q.ui {
					fail("panels share UI state")
					return
				}
			}
		}
		fmt.Println("PASS:", nativeChecksDescription)
		fmt.Println("PASS: close before first frame, duplicate close")
		app.ClosePopupWindow(parent.id)
		closeNative(batch[1].context)
		go app.ClosePopupWindow(batch[2].id)
		step, stepTime = 2, time.Now()
	case 2:
		if time.Since(stepTime) < 150*time.Millisecond {
			return
		}
		for _, p := range []*panel{parent, child, batch[1], batch[2]} {
			if p.closed != 1 || !nativeClosed(p.context) {
				fail("closed panel retains native context or callback count is wrong")
				return
			}
		}
		if batch[3].closed != 0 || !batch[3].checked {
			fail("sibling closes with another owner")
			return
		}
		app.ClosePopupWindow(parent.id)
		app.ClosePopupWindow(child.id)
		app.ClosePopupWindow(batch[3].id)
		fmt.Println("PASS: owner closes descendants, siblings survive, native close, background close, stale context")
		step = 3
	case 3:
		for _, p := range panels {
			if p.closed != 1 {
				return
			}
		}
		if rounds == 8 {
			done, ok, detail = true, true, "Window styles, input isolation, and lifecycle pass"
			fmt.Println("PASS: 8 animated open/close rounds; stale handles remain harmless")
			return
		}
		openBatch()
		step = 4
	case 4:
		for _, p := range batch {
			if !p.checked {
				return
			}
		}
		for _, p := range panels {
			app.ClosePopupWindow(p.id)
		}
		rounds++
		step = 3
	}
}

func (p *panel) view() {
	if p.closed != 0 {
		fail("root runs after close")
	}
	p.ui, p.context = ActiveUI(), GetHost().EscapeHatchBackendContext
	fresh := p.epoch != InputEpoch()
	if fresh {
		if p.frames == 0 {
			p.inputReadyAt = time.Now().Add(250 * time.Millisecond)
		}
		// Map-time focus and configure events settle before synthetic input.
		if !mode.Drive || p.frames != 3 || time.Now().After(p.inputReadyAt) {
			p.frames++
		}
		p.epoch = InputEpoch()
	}
	ModAttrs(Pad(16), Gap(10), NoAnimate)
	// A colored marker lets the native check detect another window's GPU data.
	Element(Attrs(FixSize(28, 28), Background(float32(p.tag%3)*120, 100, 50, 1)))
	Label(fmt.Sprintf("Panel %d · Frame %d", p.tag, p.frames), FontSize(18))
	Element(Attrs(Float(float32(16+p.frames%80), 205), FixSize(16, 12), Background(280, 100, 50, 1)))
	state := Use[int]("same-key-in-every-window")
	if *state == 0 {
		*state = p.tag
	}
	if *state != p.tag {
		fail("widget identity leaks between panels")
	}
	if fresh && p.frames == 4 && mode.Drive {
		GetFrameInput().Text = fmt.Sprintf("text-%d", p.tag)
	}
	if mode.Drive && fresh && p.frames == 5 {
		markNative(p.context, fmt.Sprintf("仮-%d", p.tag))
	}
	if mode.Drive && fresh && p.frames == 6 {
		if GetInputState().Composition != fmt.Sprintf("仮-%d", p.tag) {
			fail(fmt.Sprintf("panel %d composition=%q, want %q", p.tag, GetInputState().Composition, fmt.Sprintf("仮-%d", p.tag)))
		}
		finishNativeComposition(p.context)
	}
	TextInput(&p.text)
	if fresh && p.frames == 2 && mode.Drive {
		ToastExt(ToastAttrs{Body: fmt.Sprintf("Toast %d", p.tag), Duration: -1})
	}
	if Button(NoIcon, "Close this panel") {
		app.ClosePopupWindow(p.id)
	}
	if mode.Drive && fresh && p.frames == 8 {
		resizeNative(p.context, p.tag)
	}
	if mode.Drive && fresh && p.frames == 9 {
		TabFrom(GetLastId())
	}
	if mode.Drive && fresh && p.frames == 10 {
		GetFrameInput().Key = KeyDown
	}
	if mode.Drive && fresh && p.frames == 11 {
		GetFrameInput().Key = KeyEnter
	}
	MenuButton(NoIcon, "Menu", func() {
		if MenuItem(NoIcon, "Select locally") {
			p.menuClicks++
		}
	})
	if p.spawnChild && child == nil && p.frames >= 3 {
		child = openPanel(true, true)
	}
	if mode.Drive && p.frames >= 13 && !p.checked {
		if *requireGPU && !GetHost().PaintGPU {
			fail(fmt.Sprintf("panel %d uses software rendering", p.tag))
			return
		}
		if p.text != fmt.Sprintf("text-%d仮-%d", p.tag, p.tag) {
			fail(fmt.Sprintf("panel %d input: %q", p.tag, p.text))
			return
		}
		if p.menuClicks != 1 {
			fail(fmt.Sprintf("panel %d menu actions=%d", p.tag, p.menuClicks))
			return
		}
		if ToastCount() != 1 {
			fail("toast state leaks between panels")
			return
		}
		ready, err := checkNative(p)
		if err != nil {
			fail(err.Error())
			return
		}
		p.checked = ready
	}
	if mode.Drive {
		RequestNextFrame()
	}
}
