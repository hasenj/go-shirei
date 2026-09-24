// Theme demo: switch widget and surface colors, try explicit styles and popups.
//
//	go run .                 # GUI
//	go run . --png out.png   # headless frame
//	go run . --warm --png warm.png
//	go run . --dark             # cool dark
//	go run . --dark --warm      # warm dark
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

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

// filterableMenuItems: open Filterable and type e.g. "piano".
var filterableMenuItems = []string{
	"piano",
	"shirei/examples/piano",
	"git_history",
	"fontviewer",
	"ferry",
	"theme",
	"layout",
	"kanban",
	"color-picker",
	"text-fields",
	"image-viewer",
	"split-panes",
	"toast",
	"synthpad",
}

var idleText = "Text, caret, and selection follow the scheme"
var focusedText = "focused, per-input meadow accent"
var pathText = "."
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
var warmScheme bool
var darkScheme bool
var sliderValue float32 = .65
var areaText = "A multiline field follows the same input style.\nSelect text, then switch schemes."
var logRing = NewTextRing(4096)

func init() {
	for i := range 20 {
		logRing.AppendLine(fmt.Sprintf("Log entry %02d — select text or hover to copy", i+1))
	}
}

var toolbarChecked = true
var customCheckStyle = SelectionStyleWithAccent(LightColorScheme().CheckBox, Vec4{280, 50, 45, 1})
var customButtonStyle = ButtonStyle{
	Normal: ButtonPaint{
		Background: Vec4{280, 35, 90, 1}, Text: Vec4{280, 40, 20, 1},
		Gradient: Vec4{0, 0, -6, 0}, Border: Vec4{280, 30, 50, 1}, Elevation: Vec4{280, 30, 60, 1},
	},
	Hovered: ButtonPaint{
		Background: Vec4{280, 40, 95, 1}, Text: Vec4{280, 40, 20, 1},
		Gradient: Vec4{0, 0, -6, 0}, Border: Vec4{280, 30, 50, 1}, Elevation: Vec4{280, 30, 60, 1},
	},
	Pressed: ButtonPaint{
		Background: Vec4{280, 35, 82, 1}, Text: Vec4{280, 40, 20, 1},
		Border: Vec4{280, 30, 50, 1}, Elevation: Vec4{280, 30, 60, 1},
	},
	Disabled: ButtonPaint{
		Background: Vec4{280, 10, 90, 1}, Text: Vec4{280, 10, 50, 1},
		Border: Vec4{280, 10, 70, 1}, Elevation: Vec4{280, 10, 80, 1},
	},
}

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
	Container(Attrs(Viewport, Pad(30), Gap(20)), func() {
		ScrollOnInput()
		ScrollBars()
		width := GetContentWidth()
		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			NextAccessName("warm_scheme")
			wasWarm, wasDark := warmScheme, darkScheme
			CheckBox(&warmScheme, "Warm colors")
			NextAccessName("dark_scheme")
			CheckBox(&darkScheme, "Dark mode")
			if warmScheme != wasWarm || darkScheme != wasDark {
				RequestNextFrame()
			}
			Label("Live color schemes", FontSize(11))
		})
		Container(Attrs(UseSurface(SurfaceToolbar), Expand, Pad(12), Gap(10)), func() {
			Container(Attrs(Row, CrossMid, Gap(8)), func() {
				Icon(SymGrid, FontSize(16))
				Label("Documents", FontWeight(WeightBold))
				CheckBox(&toolbarChecked, "Show archived")
			})
			Container(Attrs(Row, CrossMid, Gap(12)), func() {
				Button(NoIcon, "Default")
				NextButtonType(ButtonPrimary)
				Button(NoIcon, "Save")
				NextButtonType(ButtonDestructive)
				Button(NoIcon, "Delete")
				NextButtonType(ButtonPrimary)
				NextButtonDisabled(true)
				Button(NoIcon, "Unavailable")
			})
		})
		Container(Attrs(AmendTextStyle(FontWeight(WeightMedium)), UseSurface(SurfacePanel),
			AmendTextStyle(FontSize(13)), Expand, Pad(16), Gap(8), BorderWidth(1)), func() {
			Label("Panel text inherits its surface color")
			Container(Attrs(Gap(6)), func() {
				Label("A plain nested container keeps the same foreground.", FontSize(11))
				Label("This line uses an explicit text color.", FontSize(11), TextColor(285, 45, 40, 1))
			})
			Container(Attrs(Row, CrossMid, Gap(10)), func() {
				ButtonStyled("Explicit purple style", ButtonAttrs{}, DefaultButtonLook(), customButtonStyle, Vec4{280, 70, 40, 1})
				Label("Keeps its colors across schemes", FontSize(11))
			})
		})
		Container(Attrs(Row, Wrap, CrossMid, Gap(14), MaxWidth(width)), func() {
			NextAccessName("btn_lightsteel")
			ButtonWithAccent(NoIcon, "LightSteel", AccentLightSteel)
			NextAccessName("btn_slateblue")
			ButtonWithAccent(NoIcon, "SlateBlue", AccentSlateBlue)
			NextAccessName("btn_blue")
			ButtonWithAccent(NoIcon, "Blue", AccentBlue)
			NextAccessName("btn_meadow")
			ButtonWithAccent(NoIcon, "Meadow", AccentMeadow)
			NextAccessName("btn_sunshine")
			ButtonWithAccent(NoIcon, "Sunshine", AccentSunshine)
			NextAccessName("btn_plastic")
			ButtonWithAccent(NoIcon, "Plastic", AccentPlastic)

			NextAccessName("btn_disabled")
			NextButtonDisabled(true)
			Button(NoIcon, "Disabled")
			NextAccessName("btn_disabled_blue")
			NextButtonDisabled(true)
			NextButtonAccent(AccentBlue)
			Button(NoIcon, "Disabled Blue")
			NextAccessName("btn_disabled_meadow")
			NextButtonDisabled(true)
			NextButtonAccent(AccentMeadow)
			Button(NoIcon, "Disabled Meadow")
		})

		Container(Attrs(Row, CrossMid, Gap(20)), func() {
			NextAccessName("menu")
			MenuButton(MenuIcon, "Menu", func() {
				MenuItem(SymRefresh, "Refresh")
				MenuItem(SymCopy, "Copy")
				MenuItem(SymSearch, "Search")
			})
			NextAccessName("menu_list")
			MenuButtonExt("List", ButtonAttrs{Accent: AccentBlue, Icon: SymMenu}, DefaultButtonLook(), func() {
				MenuItemExt("Refresh", ButtonAttrs{Icon: SymRefresh, Accent: AccentMeadow})
				MenuItemExt("Copy", ButtonAttrs{Icon: SymCopy, Accent: AccentMeadow})
			})
			NextAccessName("menu_filter")
			MenuButton(MenuIcon, "Filterable", func() {
				_ = MenuFilterQuery()
				for _, name := range filterableMenuItems {
					if !MenuFilterMatches(name) {
						continue
					}
					MenuItem(NoIcon, name)
				}
			})

			NextAccessName("btn_data")
			CtrlButton(SymGrid, "Data", true)
			NextAccessName("btn_info")
			CtrlButton(SymInfo, "Info", true)
			NextAccessName("btn_enable")
			CtrlButton(NoIcon, "Enable", true)
		})

		idleAttrs := DefaultTextInputAttrs()
		idleAttrs.NoAutoFocus = true
		NextAccessName("field_idle")
		TextInputExt(&idleText, idleAttrs)

		focusedAttrs := DefaultTextInputAttrs()
		focusedAttrs.Accent = AccentMeadow
		NextAccessName("field_focused")
		TextInputExt(&focusedText, focusedAttrs)

		// height parity check: button and input at default sizes, side by side.
		// "Open modal" is also the focus-trap demo: Tab among the fields/buttons
		// above, open the modal, confirm focus jumps in and Tab stays inside.
		Container(Attrs(Row, CrossMid, Gap(10)), func() {
			NextAccessName("field_path")
			TextInput(&pathText)
			NextAccessName("open_modal")
			if Button(NoIcon, "Open modal") {
				modalName = ""
				modalEmail = ""
				showModal = true
			}
		})

		if showModal {
			const modalInner = 320 // Modal(360) minus 2×20 pad
			Modal(360, func() { showModal = false }, func() {
				ModAttrs(UseSurface(SurfacePanel))
				Label("Focus trap", FontSize(14), FontWeight(WeightBold))
				Container(Attrs(MaxWidth(modalInner)), func() {
					Label("Tab should stay in this card; Escape or outside click dismisses.", FontSize(12))
				})
				attrs := DefaultTextInputAttrs()
				attrs.MinWidth = modalInner
				Label("Your name", FontSize(11))
				NextAccessName("modal_name")
				TextInputExt(&modalName, attrs)
				Label("Email address", FontSize(11))
				NextAccessName("modal_email")
				TextInputExt(&modalEmail, attrs)
				Container(Attrs(Row, CrossMid, Gap(8)), func() {
					NextAccessName("modal_cancel")
					if Button(NoIcon, "Cancel") {
						showModal = false
					}
					NextAccessName("modal_ok")
					if ButtonExt("OK", ButtonAttrs{Accent: AccentMeadow}, DefaultButtonLook()) {
						showModal = false
					}
				})
			})
		}

		Container(Attrs(Row, CrossMid, Gap(20)), func() {
			NextAccessName("check_aqua")
			CheckBoxExt(&checkedAqua, "Themed", CheckBoxAttrs{Size: 28})
			NextAccessName("check_meadow")
			CheckBoxExt(&checkedMeadow, "Accent", CheckBoxAttrs{Accent: AccentMeadow, Size: 28})
			NextAccessName("check_a")
			CheckBoxExt(&uncheckedA, "Themed", CheckBoxAttrs{Size: 28})
			NextAccessName("check_b")
			CheckBoxStyled(&uncheckedB, "Styled", CheckBoxAttrs{Size: 28}, customCheckStyle, Vec4{280, 70, 40, 1})
		})

		Container(Attrs(Row, CrossMid, Gap(20)), func() {
			NextAccessName("toggle_off")
			ToggleSwitch(&toggleOff)
			NextAccessName("toggle_on")
			ToggleSwitchExt(&toggleOn, ToggleSwitchAttrs{Accent: AccentMeadow})
		})

		Container(Attrs(Row, CrossMid, Gap(30)), func() {
			OptionGroup(&radioOpt, func() {
				ModAttrs(Spacing(10))
				NextAccessName("radio_a")
				OptionButton("First", "A")
				NextAccessName("radio_b")
				OptionButton("Second", "B")
			})
			NextAccessName("seg")
			SegmentedControl(&segOpt, func() {
				NextAccessName("seg_x")
				SegmentedCell("X", 10)
				NextAccessName("seg_y")
				SegmentedCell("Y", 20)
				NextAccessName("seg_z")
				SegmentedCell("Z", 30)
			})
		})

		Container(Attrs(Row, CrossMid, Gap(20)), func() {
			Slider(&sliderValue, SliderAttrs{Max: 1, Step: .05, Width: 230})
			ProgressBarExt(sliderValue, ProgressBarAttrs{Width: 180, Label: fmt.Sprintf("%.0f%%", sliderValue*100)})
		})
		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			if Button(NoIcon, "Show toast") {
				ToastExt(ToastAttrs{Title: "Scheme-aware notification", Body: "Change the scheme while this toast is visible.", Duration: 20 * time.Second})
			}
			Label("Working")
			BusyDots()
		})
		TextArea(&areaText)
		DirectoryBrowse(&pathText)
		Container(Attrs(UseSurface(SurfacePanel), FixHeight(170), Expand, Clip), func() {
			Table("theme-table", 28, []TableColumn[string]{
				{Label: "Name", Cell: func(row string) { Label(row) }, Less: func(a, b string) bool { return a < b }},
			}, filterableMenuItems, func(row string) any { return row }, 0)
		})
		Container(Attrs(UseSurface(SurfacePanel), FixHeight(130), Expand, Clip), func() {
			LogView(logRing, TextStyle(FontSize(11)))
		})
		Container(Attrs(UseSurface(SurfacePanel), FixHeight(120), Expand, Clip, BorderWidth(1)), func() {
			ScrollOnInput()
			ScrollBars()
			scrollDemoRows()
		})
	})
}

func main() {
	pngPath := flag.String("png", "", "write one settled frame to PATH and exit")
	flag.BoolVar(&warmScheme, "warm", false, "start with warm colors")
	flag.BoolVar(&darkScheme, "dark", false, "start with a dark color scheme")
	flag.Parse()

	if *pngPath != "" {
		if err := RenderToPNG(*pngPath, 620, 900, RootView); err != nil {
			fmt.Println("render failed:", err)
			os.Exit(1)
		}
		return
	}

	app.SetupWindow("Theme demo", 620, 900)
	app.SetupDrive()
	app.Run(RootView)
}
