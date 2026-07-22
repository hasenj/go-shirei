package main

import (
	"fmt"
	"os"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("Misc Controls Demo", 600, 500)
	app.Run(frameFn)
}

var active = true

var from float32 = 0.4
var to float32 = 1.2
var rmin float32 = 0
var rmax float32 = 2.5

var dirpath, _ = os.UserHomeDir()

var label = "Test Label"
var passwd = "My!Pass1"

var color Vec4

var colors []Vec4

func init() {
	const count = 5
	for h := float32(0); h < 360; h += (360 / count) {
		colors = append(colors, Vec4{h, 60, 60, 1})
	}
	for h := float32(0); h < 360; h += (360 / count) {
		colors = append(colors, Vec4{h, 40, 40, 1})
	}

	color = colors[0]
}

type TAB_ID int

var tab TAB_ID

const TAB_A = 0
const TAB_B = 1

func frameFn() {
	var bg = Vec4{60, 10, 90, 1}

	// Tabs bar
	Container(Attrs(Row, Expand, Pad(10), Gap(4), Background(0, 0, 70, 1)), func() {
		ModAttrs(func(a *AttrSet) {
			a.Padding[PAD_BOTTOM] = 0
		})
		var props TabsProps
		props.Active = bg
		props.Inactive = Vec4Add(bg, Vec4{0, 0, -10, -0.1})
		TabExt(&tab, "Tab A", TAB_A, props)
		TabExt(&tab, "Tab B", TAB_B, props)
	})
	ContainerWithKey(tab, Attrs(Viewport, Pad(10), Gap(10), Background(60, 10, 90, 1)), func() {
		ScrollOnInput()
		switch tab {
		case TAB_A:
			Label(fmt.Sprintf("Active: %v", active))
			ToggleSwitch(&active)

			Nil()

			Label(fmt.Sprintf("Range:  from: %f   to: %f", from, to))
			RangePicker(&from, &to, rmin, rmax)

			Label("Regular Text Input")
			TextInput(&label)

			Label("Password Input")
			PasswordInput(&passwd)
			Label(passwd, FontSize(8), TextColor(0, 0, 80, 0.5))

			Label("Directory browse")
			DirectoryBrowse(&dirpath)

		case TAB_B:
			Label("Color", FontSize(20), FontWeight(WeightBold), TextColorVec(color))
			ColorInput(&color, colors)

			Label("Click for a tool tip:")
			Container(Attrs(Row, Spacing(10)), func() {
				TooltipDemo("Hello", "This is just a greeting")
				TooltipDemo("World", "It means 世界!!")
			})
		}
	})

	TooltipHost()
	DebugPanel(true)
}

func RangePicker(from *float32, to *float32, range_min float32, range_max float32) {
	var width float32 = 300
	if *to < *from {
		*to, *from = *from, *to
	}

	var r float32 = 10 // radius of circle

	xOffset := func(v float32) float32 {
		return width * (v - range_min) / (range_max - range_min)
	}

	Container(Attrs(Row, CrossAlign(AlignMiddle), MinWidth(width+r*2), MinHeight(r*2)), func() {
		drawCircle := func(v *float32) {
			Container(Attrs(Corners(r), MinSize(r*2, r*2), Background(0, 0, 98, 1), Grad(0, 0, -18, 0), BoxShadow(2), BorderWidth(1), BorderColor(0, 0, 0, 0.5)), func() {
				PressAction()
				if IsActive() {
					diff := GetFrameInput().Motion[0] // mouse movement along x-axis
					// translate the movement to the range given!
					*v += (diff / width) * (range_max - range_min)
					*v = max(range_min, min(range_max, *v))
				}
				xoffset := xOffset(*v)
				ModAttrs(Float(xoffset, 0))
			})
		}

		// background line
		sz := GetResolvedSize()
		if sz[0] == 0 {
			return // need size :/
		}
		Element(Attrs(Float(0, (sz[1]/2)-1), MinSize(sz[0], 1), Background(0, 0, 50, 1)))

		// selected line
		fromOffset := xOffset(*from)
		toOffset := xOffset(*to)
		Element(Attrs(Float(fromOffset+r, sz[1]/2-3), MinSize(toOffset-fromOffset, 6), Background(0, 0, 80, 1), Grad(0, 0, 10, 0), BorderWidth(1), BorderColor(0, 0, 0, 0.5)))

		drawCircle(to)
		drawCircle(from)

	})
}

func TooltipDemo(label string, tip string) {
	Container(Attrs(Row, Gap(10)), func() {
		Label(label, FontSize(30))
		if IsHovered() && GetFrameInput().Mouse == MouseClick {
			OpenTooltip(tip)
		}
	})
}

// tooltip state. Assuming only one tooltip at a time
var tipMsg string
var tipPos Vec2
var tipOn bool
var tipJustOpened bool

func OpenTooltip(msg string) {
	tipOn = true
	tipPos = GetInputState().MousePoint
	tipMsg = msg
	tipJustOpened = true
}

func TooltipHost() {
	if tipOn {
		Container(Attrs(FloatVec(tipPos), Pad(4), Background(0, 0, 10, 1), BorderColor(0, 0, 100, 0.9), BorderWidth(1)), func() {
			Label(tipMsg, TextColor(0, 0, 100, 1), FontSize(14))
			if GetFrameInput().Mouse == MouseClick && !IsHovered() && !tipJustOpened {
				tipOn = false
			}
		})
		tipJustOpened = false
	}
}

func ColorInput(target *Vec4, colors []Vec4) {
	Container(Attrs(Gap(10)), func() {
		Container(Attrs(Row, Wrap, Gap(10), MaxWidth(300)), func() {
			for _, color := range colors {
				Container(Attrs(Row, CrossMid, Pad(4), Gap(6), Corners(2)), func() {
					if PressAction() {
						*target = color
					}
					if IsHovered() {
						var bg = color
						bg[SATURATION] = 40
						bg[ALPHA] = 0.4
						bg[LIGHT] = 60
						ModAttrs(BackgroundVec(bg))
					}
					var sym = SymRadioOff
					if *target == color {
						sym = SymRadioFull
					}
					Icon(sym)
					Element(Attrs(FixSize(20, 20), Corners(2), BackgroundVec(color)))
				})
			}
		})
	})
}

type TabsProps struct {
	Active   Vec4
	Inactive Vec4
}

func TabExt[T comparable](target *T, label string, value T, props TabsProps) {
	Container(Attrs(Pad2(6, 12), Corners4(4, 4, 0, 0), BackgroundVec(props.Inactive)), func() {
		if PressAction() {
			*target = value
		}
		if *target == value {
			ModAttrs(BackgroundVec(props.Active), BoxShadow(2))
		}
		Label(label)
	})
}
