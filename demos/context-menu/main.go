package main

import (
	"fmt"
	"log"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
)

func main() {
	app.SetupWindow("Context Menu Demo", 500, 500)
	app.Run(func() {
		ModAttrs(FixSizeVec(WindowSize), Background(0, 0, 80, 1), Pad(20), Gap(20))
		ScrollOnInput()
		for a := range 20 {
			Container(Attrs(Row, Gap(20)), func() {
				for b := range 10 {
					label := fmt.Sprintf("%02d:%02d", a, b)
					Container(Attrs(Corners(10), Background(200, 50, 50, 1), Pad(10)), func() {
						if IsHovered() && FrameInput.Mouse == MouseClick {
							OpenMenu(SampleMenu1)
						}
						if menuTarget == CurrentId() {
							ModAttrs(Background(200, 50, 70, 1))
						}
						Label(label, TextColor(0, 0, 100, 0.7))
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
	Container(Attrs(Row, Expand, CrossAlign(AlignMiddle), Background(0, 0, 80, 1), Pad(12)), func() {
		var hovered = IsHovered()

		// hovering highlight
		sz := GetResolvedSize()
		sz[0] -= 5 * 2
		sz[1] -= 5 * 2
		var bg = Vec4{240, 100, 60, 0}
		if hovered {
			bg[3] = 0.9
		}
		Element(Attrs(Float(5, 5), Corners(4), MinSizeVec(sz), BackgroundVec(bg)))

		Label(label, FontSize(16), TextColor(0, 0, 10, 1))
		Element(Attrs(Grow(1), MinWidth(20)))
		Label(shortcut, FontSize(10), TextColor(0, 0, 10, 0.6))
		clicked = IsHovered() && FrameInput.Mouse == MouseClick
	})
	if clicked {
		CloseMenu()
	}
	return clicked
}

func SampleMenu1() {
	var attrs0 AttrSet
	attrs0.Corners = N4(6)
	attrs0.Shadow.Alpha = 0.3
	attrs0.Shadow.Blur = 30
	Container(attrs0, func() {
		Container(Attrs(MinWidth(100), Corners(6), Gap(1), MaxWidth(400), Background(0, 0, 10, 1), BorderWidth(2), BorderColor(0, 0, 10, 1), Clip), func() {
			ModAttrs(func(a *AttrSet) {
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
var menuTarget ContainerId
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

	Container(Attrs(), func() {
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

		ModAttrs(FloatVec(pos))

		menu()

		if !menuJustOpened && !IsHovered() && FrameInput.Mouse == MouseClick {
			CloseMenu()
		}
	})
	menuJustOpened = false
}
