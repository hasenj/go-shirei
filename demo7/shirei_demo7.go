package main

import (
	"fmt"
	"os"

	app "go.hasen.dev/shirei/giobackend"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("Misc Controls Demo", 400, 400)
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

func frameFn() {
	ModAttrs(Pad(10), Gap(10), BG(60, 10, 90, 1))
	ScrollOnInput()

	Label(fmt.Sprintf("Active: %v", active))
	ToggleSwitch(&active)

	Nil()

	Label(fmt.Sprintf("Range:  from: %f   to: %f", from, to))
	RangePicker(&from, &to, rmin, rmax)

	Label("Regular Text Input")
	TextInput(&label)

	Label("Password Input")
	PasswordInput(&passwd)
	Label(passwd, Sz(8), Clr(0, 0, 80, 0.5))

	Label("Directory input")
	DirectoryInput(&dirpath)

	Label("Color", Sz(20), FontWeight(WeightBold), ClrV(color))
	ColorInput(&color, colors)

	Label("Click for a tool tip:")
	Layout(TW(Row, Spacing(10)), func() {
		TooltipDemo("Hello", "This is just a greeting")
		TooltipDemo("World", "It means 世界!!")
	})

	TooltipHost()
	PopupsHost()
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

	Layout(TW(Row, CA(AlignMiddle), MinWidth(width+r*2), MinHeight(r*2)), func() {
		drawCircle := func(v *float32) {
			Layout(TW(BR(r), MinSize(r*2, r*2), BG(0, 0, 98, 1), Grad(0, 0, -18, 0), Shd(2), BW(1), Bo(0, 0, 0, 0.5)), func() {
				PressAction()
				if IsActive() {
					diff := FrameInput.Motion[0] // mouse movement along x-axis
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
		Element(TW(Float(0, (sz[1]/2)-1), MinSize(sz[0], 1), BG(0, 0, 50, 1)))

		// selected line
		fromOffset := xOffset(*from)
		toOffset := xOffset(*to)
		Element(TW(Float(fromOffset+r, sz[1]/2-3), MinSize(toOffset-fromOffset, 6), BG(0, 0, 80, 1), Grad(0, 0, 10, 0), BW(1), Bo(0, 0, 0, 0.5)))

		drawCircle(to)
		drawCircle(from)

	})
}

func TooltipDemo(label string, tip string) {
	Layout(TW(Row, Gap(10)), func() {
		Label(label, Sz(30))
		if IsHovered() && FrameInput.Mouse == MouseClick {
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
	tipPos = InputState.MousePoint
	tipMsg = msg
	tipJustOpened = true
}

func TooltipHost() {
	if tipOn {
		Layout(TW(FloatV(tipPos), Pad(4), BG(0, 0, 10, 1), Bo(0, 0, 100, 0.9), BW(1)), func() {
			Label(tipMsg, Clr(0, 0, 100, 1), Sz(14))
			if FrameInput.Mouse == MouseClick && !IsHovered() && !tipJustOpened {
				tipOn = false
			}
		})
		tipJustOpened = false
	}
}

func ColorInput(target *Vec4, colors []Vec4) {
	Layout(TW(Gap(10)), func() {
		Layout(TW(Row, Wrap, Gap(10), MaxWidth(300)), func() {
			for _, color := range colors {
				Layout(TW(Row, CrossMid, Pad(4), Gap(6), BR(2)), func() {
					if PressAction() {
						*target = color
					}
					if IsHovered() {
						var bg = color
						bg[SATURATION] = 40
						bg[ALPHA] = 0.4
						bg[LIGHT] = 60
						ModAttrs(BGV(bg))
					}
					var sym = SymRadioOff
					if *target == color {
						sym = SymRadioFull
					}
					Icon(sym)
					Element(TW(FixSize(20, 20), BR(2), BGV(color)))
				})
			}
		})
	})
}
