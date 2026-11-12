// Window-style demonstrates desktop window transparency and decorations.
//
//	go run ./shirei/demos/window-style
//	SHIREI_GPU=0 go run ./shirei/demos/window-style
//	go run ./shirei/demos/window-style -transparent=false
//	go run ./shirei/demos/window-style -undecorated=false
package main

import (
	"flag"
	"fmt"
	"time"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/ext/window"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

var (
	transparent  = flag.Bool("transparent", true, "enable the transparent window background (macOS, Windows, Wayland, and X11)")
	undecorated  = flag.Bool("undecorated", true, "omit the title bar and window buttons (macOS, Windows, Wayland, and X11)")
	quiet        = flag.Bool("quiet", false, "open without activating the app")
	closeAfter   = flag.Duration("close-after", 0, "automatically close after this duration (e.g. 5s)")
	started      time.Time
	panelAlpha   float32 = .85
	text                 = "Type here — 日本語"
	opaqueRoot   bool
	showSwatches = true
)

func main() {
	flag.Parse()
	app.SetupWindow("Window Style Demo", 640, 480)
	if *transparent {
		app.SetupTransparent()
	}
	if *undecorated {
		app.SetupUndecorated()
	}
	if *quiet {
		app.SetupQuiet()
	}
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
	window.SetMinSize(600, 460)
	SetDarkMode(true)
	ModAttrs(Pad(24), Gap(20), AmendTextStyle(TextColor(0, 0, 100, 1)))
	if opaqueRoot {
		ModAttrs(Background(220, 25, 18, 1))
	}

	Container(Attrs(Expand, Pad(20), Gap(14), Corners(20), Clip,
		Background(225, 30, 16, panelAlpha)), func() {
		Container(Attrs(Row, Expand, CrossMid, Gap(12)), func() {
			Label("Window styles", FontSize(23), FontWeight(WeightSemibold))
			Filler(1)
			if Button(NoIcon, "Close") {
				app.Quit()
			}
		})
		Label(fmt.Sprintf("Transparent: %t  ·  Undecorated: %t", *transparent, *undecorated),
			FontSize(12), TextColor(215, 15, 80, 1))
		if *transparent {
			Label("The gaps and rounded corners reveal the windows behind this one.", FontSize(13))
		} else {
			Label("Unpainted pixels have an opaque white background.", FontSize(13))
		}
		Container(Attrs(Expand, Gap(6)), func() {
			Label("Keyboard focus / text input", FontSize(12))
			input := DefaultTextInputAttrs()
			input.MinWidth = GetHost().WindowSize[0] - 88
			input.NoAutoFocus = true
			TextInputExt(&text, input)
		})
		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			Label("Panel opacity", FontSize(13))
			Slider(&panelAlpha, SliderAttrs{Min: .15, Max: 1, Step: .05, Width: 240})
			Label(fmt.Sprintf("%.0f%%", panelAlpha*100), FontSize(13))
		})
		Container(Attrs(Row, CrossMid, Gap(24)), func() {
			CheckBox(&opaqueRoot, "Paint an opaque background")
			CheckBox(&showSwatches, "Show swatches")
		})
		Container(Attrs(Row, CrossMid, Gap(10)), func() {
			if Button(NoIcon, "Center") {
				window.Center()
			}
			if Button(NoIcon, "Small") {
				window.SetSize(640, 480)
			}
			if Button(NoIcon, "Large") {
				window.SetSize(800, 580)
			}
			Filler(1)
			renderer := "Software"
			if GetHost().PaintGPU {
				renderer = "GPU"
			}
			Label(renderer, FontSize(12), TextColor(215, 15, 80, 1))
		})
	})
	Container(Attrs(Row, Expand, FixHeight(80), Gap(20)), func() {
		if showSwatches {
			for i, alpha := range []float32{.25, .6, 1} {
				ContainerWithKey(i, Attrs(Grow(1), Expand, Center, Corners(18),
					Background(float32(180+i*45), 70, 45, alpha)), func() {
					Label(fmt.Sprintf("%.0f%% fill", alpha*100), FontSize(17), FontWeight(WeightSemibold))
				})
			}
		}
	})
}
