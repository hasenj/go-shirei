// Controls demo: buttons, checkboxes, sliders, and segmented controls.
//
//	go run .
//	go run . --dark --warm
//	go run . --png controls.png
package main

import (
	"flag"
	"fmt"
	"os"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

var warmScheme, darkScheme bool

var sampleOff, sampleOn = false, true
var sampleScale float32 = 60
var samplePeriod = "Week"
var sampleLayout = "List"
var sampleClicks int

func RootView() {
	if warmScheme {
		SetLightColorScheme(WarmColorScheme())
		SetDarkColorScheme(WarmDarkColorScheme())
	} else {
		SetLightColorScheme(LightColorScheme())
		SetDarkColorScheme(DarkColorScheme())
	}
	SetDarkMode(darkScheme)
	ModAttrs(UseSurface(SurfaceCanvas))
	Container(Attrs(Viewport, Pad(24), Gap(20)), func() {
		ScrollOnInput()
		ScrollBars()
		Label("Default controls", FontSize(22), FontWeight(WeightSemibold))
		Container(Attrs(Row, Gap(20), CrossMid), func() {
			NextAccessName("warm_scheme")
			CheckBox(&warmScheme, "Warm colors")
			NextAccessName("dark_scheme")
			CheckBox(&darkScheme, "Dark mode")
		})
		Container(Attrs(Expand, Gap(10)), func() {
			Label("Buttons", FontWeight(WeightSemibold))
			Container(Attrs(Row, CrossMid, Gap(12)), func() {
				NextAccessName("sample_button")
				if Button(SymRefresh, "Refresh") {
					sampleClicks++
				}
				NextButtonType(ButtonPrimary)
				if Button(NoIcon, "Apply") {
					sampleClicks++
				}
				NextButtonType(ButtonDestructive)
				if Button(NoIcon, "Delete") {
					sampleClicks++
				}
				NextButtonDisabled(true)
				Button(NoIcon, "Unavailable")
			})
			Label(fmt.Sprintf("%d button presses", sampleClicks), FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
		})
		Container(Attrs(Expand, Gap(10)), func() {
			Label("Checkboxes", FontWeight(WeightSemibold))
			Container(Attrs(Row, CrossMid, Gap(24)), func() {
				NextAccessName("sample_check")
				CheckBox(&sampleOff, "Show details")
				CheckBox(&sampleOn, "Include subfolders")
			})
		})
		Container(Attrs(Expand, Gap(10)), func() {
			Label("Slider", FontWeight(WeightSemibold))
			Container(Attrs(Row, CrossMid, Gap(16)), func() {
				Label("Scale")
				NextAccessName("sample_slider")
				Slider(&sampleScale, SliderAttrs{Max: 100, Step: 1, Width: 280})
				Label(fmt.Sprintf("%.0f%%", sampleScale))
			})
		})
		Container(Attrs(Expand, Gap(10)), func() {
			Label("Segmented control", FontWeight(WeightSemibold))
			NextAccessName("sample_segments")
			SegmentedControl(&samplePeriod, func() {
				for _, period := range []string{"Day", "Week", "Month"} {
					NextAccessName("sample_segment")
					NextAccessValue(period)
					SegmentedCell(period, period)
				}
			})
		})
		Container(Attrs(Expand, Gap(10)), func() {
			Label("Together in a toolbar", FontWeight(WeightSemibold))
			Container(Attrs(Expand, UseSurface(SurfacePanel), Pad(14), Gap(16), Corners(4), BorderWidth(1)), func() {
				Container(Attrs(Row, CrossMid, Gap(16)), func() {
					SegmentedControl(&sampleLayout, func() {
						SegmentedCell("List", "List")
						SegmentedCell("Grid", "Grid")
					})
					CheckBox(&sampleOn, "Details")
					if Button(SymRefresh, "Refresh") {
						sampleClicks++
					}
					NextButtonType(ButtonPrimary)
					if Button(NoIcon, "Apply") {
						sampleClicks++
					}
				})
				Container(Attrs(Row, CrossMid, Gap(16)), func() {
					Label("Scale")
					Slider(&sampleScale, SliderAttrs{Max: 100, Step: 1, Width: 280})
					Label(fmt.Sprintf("%.0f%%", sampleScale))
				})
			})
		})
		Label("Tab moves focus. Space presses controls. Arrow keys adjust sliders and segments.", FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
	})
}

func main() {
	pngPath := flag.String("png", "", "write one settled frame to PATH and exit")
	flag.BoolVar(&warmScheme, "warm", false, "start with warm colors")
	flag.BoolVar(&darkScheme, "dark", false, "start with a dark color scheme")
	flag.Parse()

	if *pngPath != "" {
		if err := RenderToPNG(*pngPath, 620, 580, RootView); err != nil {
			fmt.Println("render failed:", err)
			os.Exit(1)
		}
		return
	}

	app.SetupWindow("Controls demo", 620, 580)
	app.SetupDrive()
	app.Run(RootView)
}
