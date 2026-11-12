// Popup-windows demonstrates native window styles, placement, and nested owners.
//
//	go run ./shirei/demos/popup-windows
//	SHIREI_GPU=0 go run ./shirei/demos/popup-windows
//	go run ./shirei/demos/popup-windows -background
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
	quiet                = flag.Bool("quiet", false, "open without requesting app activation")
	background           = flag.Bool("background", false, "request that the app stay running after its windows close")
	openPair             = flag.Bool("open", true, "open a utility window and a floating panel at startup")
	closeAfter           = flag.Duration("close-after", 0, "automatically exit after this duration")
	started              time.Time
	initialized          bool
	options              = app.PopupWindowOptions{Transparent: true, Undecorated: true, Floating: true, Focus: true}
	positioned           = true
	screenCoordinates    = true
	position             = Vec2{120, 120}
	mainText             = "Main window"
	lastError            string
	popups               []*popup
	serial, sharedClicks int
)

const popupWidth, popupHeight = 480, 560

type popup struct {
	id       app.PopupWindow
	options  app.PopupWindowOptions
	text     string
	position Vec2
	alpha    float32
}

func main() {
	flag.Parse()
	started = time.Now()
	app.SetupWindow("Popup Windows Demo", 760, 660)
	app.SetupDrive()
	if *quiet {
		app.SetupQuiet()
	}
	if *background {
		app.SetupBackground()
	}
	app.Run(root)
}

func open(options app.PopupWindowOptions) {
	serial++
	kind := "Owned popup"
	if options.Floating {
		kind = "Floating panel"
	} else if options.TopLevel {
		kind = "Utility window"
	}
	options.Title = fmt.Sprintf("%s %d", kind, serial)
	options.Width, options.Height = popupWidth, popupHeight
	p := &popup{text: "Type here — 日本語", position: Vec2{120, 120}, alpha: .88}
	if options.ScreenPosition != nil {
		p.position = *options.ScreenPosition
		options.ScreenPosition = &p.position
	}
	options.OnClose = func() { p.id = 0; RequestNextFrame() }
	p.options = options
	id, err := app.OpenPopupWindow(options, p.view)
	if err != nil {
		lastError = err.Error()
		return
	}
	lastError = ""
	p.id = id
	popups = append(popups, p)
	RequestNextFrame()
}

func openUtility() {
	open(app.PopupWindowOptions{TopLevel: true, ScreenPosition: &Vec2{80, 80}})
}

func openFloating() {
	open(app.PopupWindowOptions{Transparent: true, Undecorated: true, Floating: true, ScreenPosition: &Vec2{600, 160}})
}

func quitWhenDue() {
	if *closeAfter > 0 {
		if time.Since(started) >= *closeAfter {
			app.Quit()
		}
		RequestNextFrame()
	}
}

func root() {
	quitWhenDue()
	window.SetMinSize(680, 520)
	SetDarkMode(true)
	if !initialized {
		initialized = true
		if *openPair {
			openUtility()
			openFloating()
		}
	}
	ModAttrs(Background(225, 25, 14, 1), Pad(24), Gap(12), AmendTextStyle(TextColor(0, 0, 95, 1)))
	Label("Popup windows", FontSize(24), FontWeight(WeightSemibold))
	Label("Compare utility windows, floating panels, and owned popups.", FontSize(14))
	Container(Attrs(Viewport, Gap(12)), func() {
		ScrollOnInput()
		TextInput(&mainText)
		Container(Attrs(Row, Gap(10)), func() {
			NextAccessName("open_utility")
			if Button(NoIcon, "Utility window") {
				openUtility()
			}
			NextAccessName("open_floating")
			if Button(NoIcon, "Floating panel") {
				openFloating()
			}
		})
		Label("Configure a popup", FontSize(17), FontWeight(WeightSemibold))
		Container(Attrs(Row, Gap(20), CrossMid), func() {
			CheckBox(&options.Transparent, "Transparent")
			CheckBox(&options.Undecorated, "Undecorated")
			CheckBox(&options.Focus, "Focus on open")
		})
		Container(Attrs(Row, Gap(20), CrossMid), func() {
			CheckBox(&options.Floating, "Floating")
			CheckBox(&options.TopLevel, "Top-level")
		})
		Label("Floating stays above other apps; top-level uses normal stacking.", FontSize(13))
		if options.Floating && options.TopLevel {
			Label("Floating takes priority when both are selected.", FontSize(13))
		}
		Container(Attrs(Row, Gap(20), CrossMid), func() {
			CheckBox(&positioned, "Set position")
			if positioned {
				CheckBox(&screenCoordinates, "Screen coordinates")
				if screenCoordinates {
					CheckBox(&options.ExactScreenPosition, "Exact position")
				}
			}
		})
		if positioned {
			positionControls(&position)
			if screenCoordinates {
				Label("Screen points from the desktop origin; exact placement skips clamping.", FontSize(13))
			} else {
				Label("Offset from the owner's content corner.", FontSize(13))
			}
		} else {
			Label("Without a position, the popup uses default placement.", FontSize(13))
		}
		Container(Attrs(Row, Gap(10)), func() {
			NextAccessName("open_configured")
			if Button(NoIcon, "Open configured") {
				opts := options
				if positioned {
					pos := position
					if screenCoordinates {
						opts.ScreenPosition = &pos
					} else {
						opts.Position = &pos
					}
				}
				open(opts)
			}
			NextAccessName("close_all")
			if Button(NoIcon, "Close all") {
				for _, p := range popups {
					app.ClosePopupWindow(p.id)
				}
			}
			if Button(NoIcon, "Quit") {
				app.Quit()
			}
		})
		live := 0
		for _, p := range popups {
			if p.id != 0 {
				live++
			}
		}
		NextAccessName("panel_count")
		NextAccessRole("status")
		NextAccessValue(fmt.Sprint(live))
		Container(Attrs(), func() {
			AssignAccess()
			Label(fmt.Sprintf("Open panels: %d   ·   Shared clicks: %d", live, sharedClicks), FontSize(14))
		})
		Label("Window placement, focus, and decorations follow desktop policy.", FontSize(13))
		if *background {
			Label("Background lifetime enabled. Panels can request Activate main.", FontSize(13))
		} else {
			Label("Run with -background to try keeping the app alive after closing windows.", FontSize(13))
		}
		if lastError != "" {
			Label(lastError, TextColor(0, 70, 70, 1))
		}
		ScrollBars()
	})
}

func positionControls(position *Vec2) {
	for axis, name := range []string{"X", "Y"} {
		ContainerWithKey(axis, Attrs(Row, Gap(10), CrossMid), func() {
			Label(name, FontSize(13))
			Slider(&position[axis], SliderAttrs{Min: -400, Max: 1600, Step: 20, Width: 240})
			Label(fmt.Sprintf("%.0f", position[axis]), FontSize(13))
		})
	}
}

func (p *popup) view() {
	quitWhenDue()
	window.SetMinSize(420, 440)
	ModAttrs(Pad(16), AmendTextStyle(TextColor(0, 0, 95, 1)))
	Container(Attrs(Viewport, Pad(20), Gap(12), Corners(20), Background(225, 30, 19, p.alpha)), func() {
		ScrollOnInput()
		Label(p.options.Title, FontSize(21), FontWeight(WeightSemibold))
		Label(fmt.Sprintf("Transparent: %t · Undecorated: %t", p.options.Transparent, p.options.Undecorated), FontSize(12))
		Label(fmt.Sprintf("Floating: %t · Top-level: %t · Focus: %t", p.options.Floating, p.options.TopLevel, p.options.Focus), FontSize(12))
		TextInput(&p.text)
		if p.options.Transparent {
			Slider(&p.alpha, SliderAttrs{Min: .2, Max: 1, Step: .05, Width: 260})
		}
		Container(Attrs(Row, Gap(10)), func() {
			if Button(NoIcon, "Shared +1") {
				sharedClicks++
				RequestNextFrame()
			}
			MenuButton(NoIcon, "Menu", func() {
				if MenuItem(NoIcon, "Show toast here") {
					ToastMessage(p.options.Title + " only")
				}
			})
		})
		Label(fmt.Sprintf("Shared clicks: %d", sharedClicks), FontSize(13))
		Label("Requested screen position", FontSize(14))
		positionControls(&p.position)
		Container(Attrs(Row, Gap(10)), func() {
			if Button(NoIcon, "Move panel") {
				app.MovePopupWindow(p.id, &p.position)
			}
			if Button(NoIcon, "Center") {
				window.Center()
			}
		})
		Container(Attrs(Row, Gap(10)), func() {
			if Button(NoIcon, "Smaller") {
				window.SetSize(420, 440)
			}
			if Button(NoIcon, "Larger") {
				window.SetSize(560, 640)
			}
			if Button(NoIcon, "Activate main") {
				window.Activate()
			}
		})
		Container(Attrs(Row, Gap(10)), func() {
			if Button(NoIcon, "Open child") {
				opts := p.options
				opts.ScreenPosition = nil
				opts.Position = &Vec2{80, 80}
				open(opts)
			}
			if Button(NoIcon, "Close panel") {
				app.ClosePopupWindow(p.id)
			}
			if Button(NoIcon, "Quit") {
				app.Quit()
			}
		})
		ScrollBars()
	})
}
