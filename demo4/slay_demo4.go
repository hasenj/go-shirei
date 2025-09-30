package main

import (
	"fmt"
	"log"

	. "go.hasen.dev/slay"
	app "go.hasen.dev/slay/giobackend"
	. "go.hasen.dev/slay/tw"
)

func main() {
	app.SetupWindow("Context Menu Demo", 500, 500)
	app.Run(func() {
		ModAttrs(FixSizeV(WindowSize), BG(0, 0, 80, 1), Pad(20), Gap(20))
		ScrollOnInput()
		for a := range 20 {
			Layout(TW(Row, Gap(20)), func() {
				for b := range 10 {
					label := fmt.Sprintf("%02d:%02d", a, b)
					Layout(TW(BR(10), BG(200, 50, 50, 1), Pad(10)), func() {
						if IsHovered() && FrameInput.Mouse == MouseClick {
							OpenMenu(SampleMenu1)
						}
						if menuTarget == CurrentId() {
							ModAttrs(BG(200, 50, 70, 1))
						}
						Label(label, Clr(0, 0, 100, 0.7))
					})
				}
			})
		}
		ContextMenu()
	})
}

func LogMessage(msg string) {
	log.Println(msg)
}

func MenuItem(label string, shortcut string) bool {
	var clicked bool
	Layout(TW(Row, Expand, CA(AlignMiddle), BG(0, 0, 80, 1), Pad(12)), func() {
		var hovered = IsHovered()

		// hovering highlight
		sz := GetResolvedSize()
		sz[0] -= 5 * 2
		sz[1] -= 5 * 2
		var bg = Vec4{240, 100, 60, 0}
		if hovered {
			bg[3] = 0.9
		}
		Element(TW(Float(5, 5), BR(4), MinSizeV(sz), BGV(bg)))

		Label(label, Sz(16), Clr(0, 0, 10, 1))
		Element(TW(Grow(1), MinWidth(20)))
		Label(shortcut, Sz(10), Clr(0, 0, 10, 0.6))
		clicked = IsHovered() && FrameInput.Mouse == MouseClick
	})
	if clicked {
		CloseMenu()
	}
	return clicked
}

func SampleMenu1() {
	var attrs0 Attrs
	attrs0.Corners = N4(6)
	attrs0.Shadow.Alpha = 0.3
	attrs0.Shadow.Blur = 30
	Layout(attrs0, func() {
		Layout(TW(MinWidth(100), BR(6), Gap(1), MaxWidth(400), BG(0, 0, 10, 1), BW(2), Bo(0, 0, 10, 1), Clip), func() {
			ModAttrs(func(a *Attrs) {
				a.Shadow.Blur = 4
				a.Shadow.Alpha = 0.7
				a.Shadow.Offset[1] = 2
			})
			if MenuItem("File", "cmd-f") {
				LogMessage("File Clicked!")
			}
			if MenuItem("Edit", "cmd-e") {
				LogMessage("Edit Clicked!")
			}
			if MenuItem("View", "cmd-f") {
				LogMessage("View Clicked!")
			}
		})
	})
}

var menu func()
var menuTarget any
var menuJustOpened = false

func OpenMenu(f func()) {
	menuTarget = CurrentId()
	menu = f
	menuJustOpened = true
}

func CloseMenu() {
	menu = nil
	menuTarget = nil
	menuJustOpened = false
}

func ContextMenu() {
	if menu == nil || menuTarget == nil {
		return
	}

	Layout(TW(), func() {
		var targetRect = GetResolvedRectOf(menuTarget)

		// naive: place it at the bottom of the target!
		const sp = 4
		var pos = targetRect.Origin
		pos[1] += targetRect.Size[1] + sp

		var selfSize = GetResolvedSize()
		if pos[0]+selfSize[0] > WindowSize[0] {
			pos[0] = WindowSize[0] - selfSize[0] - sp
		}
		if pos[1]+selfSize[1] > WindowSize[1] {
			pos[1] = WindowSize[1] - selfSize[1] - sp
		}
		pos[0] = max(0, pos[0])
		pos[1] = max(0, pos[1])

		ModAttrs(FloatV(pos))

		menu()

		if !menuJustOpened && !IsHovered() && FrameInput.Mouse == MouseClick {
			CloseMenu()
		}
	})
	menuJustOpened = false
}
