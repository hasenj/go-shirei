// Overlay demonstrates an interactive desktop overlay using the same code on
// macOS, Windows, and Linux.
//
//	go run ./shirei/demos/overlay
//	go run ./shirei/demos/overlay -quiet
//	go run ./shirei/demos/overlay -normal
package main

import (
	"flag"
	"fmt"
	"time"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/ext/window"
	. "go.hasen.dev/shirei/widgets"
)

var (
	normal     = flag.Bool("normal", false, "open an ordinary decorated window for comparison")
	quiet      = flag.Bool("quiet", false, "open without requesting keyboard focus")
	closeAfter = flag.Duration("close-after", 0, "automatically exit after this duration (e.g. 5s)")
	started    time.Time
	positioned bool
	position           = Vec2{40, 80}
	alpha      float32 = .9
	text               = "Try typing here — 日本語"
	clicks     int
)

func main() {
	flag.Parse()
	app.SetupWindow("Desktop Overlay Demo", 520, 360)
	if !*normal {
		app.SetupOverlay()
	}
	if *quiet {
		app.SetupQuiet()
	}
	app.SetupDrive()
	started = time.Now()
	app.Run(root)
}

func root() {
	if *closeAfter > 0 {
		if time.Since(started) >= *closeAfter {
			app.Quit()
		}
		RequestNextFrame()
	}
	if !positioned {
		positioned = true
		window.SetMinSize(520, 360)
		window.Position(int(position[0]), int(position[1]))
	}
	SetDarkMode(true)
	ModAttrs(Pad(20), AmendTextStyle(TextColor(0, 0, 96, 1)))
	Container(Attrs(Expand, Pad(20), Gap(14), Corners(20),
		Background(225, 30, 16, alpha)), func() {
		Container(Attrs(Row, Expand, CrossMid, Gap(12)), func() {
			NextAccessName("drag_handle")
			Container(Attrs(Grow(1), Gap(2)), func() {
				AssignAccess()
				if !*normal {
					window.DragMove(&position)
					Label("Desktop overlay", FontSize(24), FontWeight(WeightSemibold))
					Label("Drag the title to move", FontSize(11), TextColor(215, 15, 76, 1))
				} else {
					Label("Normal window", FontSize(24), FontWeight(WeightSemibold))
					Label("Move with the window's title bar", FontSize(11))
				}
			})
			NextAccessName("close")
			if Button(NoIcon, "Close") {
				app.Quit()
			}
		})
		Label("Switch to another app and watch this card.", FontSize(14))
		Container(Attrs(Expand, Gap(6)), func() {
			Label("Keyboard input", FontSize(12))
			input := DefaultTextInputAttrs()
			input.MinWidth = GetHost().WindowSize[0] - 80
			input.NoAutoFocus = true
			NextAccessName("typing")
			TextInputExt(&text, input)
		})
		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			Label("Opacity", FontSize(13))
			NextAccessName("opacity")
			Slider(&alpha, SliderAttrs{Min: .2, Max: 1, Step: .05, Width: 220})
			Label(fmt.Sprintf("%.0f%%", alpha*100), FontSize(13))
		})
		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			NextAccessName("click_me")
			if Button(NoIcon, "Click me") {
				clicks++
			}
			Label(fmt.Sprintf("Clicks: %d", clicks), FontSize(14))
		})
		Container(Attrs(Row, CrossMid, Gap(10)), func() {
			NextAccessName("reset_position")
			if Button(NoIcon, "Reset position") {
				position = Vec2{40, 80}
				window.Position(int(position[0]), int(position[1]))
			}
		})
	})
}
