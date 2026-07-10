package main

import (
	"fmt"
	"os"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func scrollDemoRows() {
	for i := 0; i < 30; i++ {
		Container(Attrs(Pad2(4, 6)), func() {
			Label(fmt.Sprintf("Row %d", i))
		})
	}
}

var idleText = "idle (package default: aqua)"
var focusedText = "focused, per-input meadow accent"
var pathText = "/Users/hasen"
var modalName = ""
var modalEmail = ""
var showModal = false
var checkedAqua = true
var checkedMeadow = true
var uncheckedA = false
var uncheckedB = false
var toggleOff = false
var toggleOn = true
var radioOpt = "A"
var segOpt = 10

func RootView() {

	Container(Attrs(Viewport, Background(220, 10, 97, 1), Pad(30), Gap(20)), func() {
		rect := GetContentRect()
		Container(Attrs(Row, Wrap, CrossMid, Gap(14), MaxWidth(rect.Size[0])), func() {
			ButtonExt("LightSteel", ButtonAttrs{Accent: AccentLightSteel})
			ButtonExt("SlateBlue", ButtonAttrs{Accent: AccentSlateBlue})
			ButtonExt("Blue", ButtonAttrs{Accent: AccentBlue})
			ButtonExt("Meadow", ButtonAttrs{Accent: AccentMeadow})
			ButtonExt("Sunshine", ButtonAttrs{Accent: AccentSunshine})
			ButtonExt("Plastic", ButtonAttrs{Accent: AccentPlastic})

			ButtonExt("Disabled", ButtonAttrs{Disabled: true})
			ButtonExt("Disabled Blue", ButtonAttrs{Disabled: true, Accent: AccentBlue})
			ButtonExt("Disabled Meadow", ButtonAttrs{Disabled: true, Accent: AccentMeadow})
		})

		Container(Attrs(Row, CrossMid, Gap(20)), func() {
			MenuButton("Menu", func() {
				MenuItem(SymRefresh, "Refresh")
				MenuItem(SymCopy, "Copy")
				MenuItem(SymSearch, "Search")
			})
			MenuButtonExt("List", ButtonAttrs{Accent: AccentBlue, Icon: SymMenu}, func() {
				MenuItemExt("Refresh", ButtonAttrs{Icon: SymRefresh, Accent: AccentMeadow})
				MenuItemExt("Copy", ButtonAttrs{Icon: SymCopy, Accent: AccentMeadow})
			})

			CtrlButton(SymGrid, "Data", true)
			CtrlButton(SymInfo, "Info", true)
			CtrlButton(0, "Enable", true)
		})

		idleAttrs := DefaultTextInputAttrs()
		idleAttrs.NoAutoFocus = true
		TextInputExt(&idleText, idleAttrs)

		focusedAttrs := DefaultTextInputAttrs()
		focusedAttrs.Accent = AccentMeadow
		TextInputExt(&focusedText, focusedAttrs)

		// height parity check: button and input at default sizes, side by side.
		// "Open modal" is also the focus-trap demo: Tab among the fields/buttons
		// above, open the modal, confirm focus jumps in and Tab stays inside.
		Container(Attrs(Row, CrossMid, Gap(10)), func() {
			TextInput(&pathText)
			if Button(0, "Open modal") {
				modalName = ""
				modalEmail = ""
				showModal = true
			}
		})

		if showModal {
			const modalInner = 320 // Modal(360) minus 2×20 pad
			Modal(360, func() { showModal = false }, func() {
				Label("Focus trap", FontSize(14), FontWeight(WeightBold), TextColor(220, 25, 25, 1))
				Label("Tab should stay in this card; Escape or outside click dismisses.", FontSize(12), TextWidth(modalInner), TextColor(220, 10, 45, 1))
				attrs := DefaultTextInputAttrs()
				attrs.MinWidth = modalInner
				Label("Your name", FontSize(11), TextColor(220, 10, 45, 1))
				TextInputExt(&modalName, attrs)
				Label("Email address", FontSize(11), TextColor(220, 10, 45, 1))
				TextInputExt(&modalEmail, attrs)
				Container(Attrs(Row, CrossMid, Gap(8)), func() {
					if Button(0, "Cancel") {
						showModal = false
					}
					if ButtonExt("OK", ButtonAttrs{Accent: AccentMeadow}) {
						showModal = false
					}
				})
			})
		}

		Container(Attrs(Row, CrossMid, Gap(20)), func() {
			CheckBoxExt(&checkedAqua, "", CheckBoxAttrs{Accent: AccentBlue, Size: 28})
			CheckBoxExt(&checkedMeadow, "", CheckBoxAttrs{Accent: AccentMeadow, Size: 28})
			CheckBoxExt(&uncheckedA, "", CheckBoxAttrs{Accent: Vec4{265, 60, 75, 1}, Size: 28})
			CheckBoxExt(&uncheckedB, "", CheckBoxAttrs{Accent: Vec4{5, 70, 70, 1}, Size: 28})
		})

		Container(Attrs(Row, CrossMid, Gap(20)), func() {
			ToggleSwitch(&toggleOff)
			ToggleSwitchExt(&toggleOn, ToggleSwitchAttrs{Accent: AccentMeadow})
		})

		Container(Attrs(Row, CrossMid, Gap(30)), func() {
			Container(Attrs(Spacing(10)), func() {
				OptionButton(&radioOpt, "First", "A")
				OptionButton(&radioOpt, "Second", "B")
			})
			SegmentedControl(&segOpt, Cell("X", 10), Cell("Y", 20), Cell("Z", 30))
		})

		Container(Attrs(FixHeight(120), Expand, Clip, Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 88, 1)), func() {
			ScrollOnInput()
			ScrollBars()
			scrollDemoRows()
		})
	})
}

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], 500, 570, RootView); err != nil {
			fmt.Println("render failed:", err)
		}
		return
	}

	app.SetupWindow("Theme demo", 540, 640)
	app.Run(RootView)
}
