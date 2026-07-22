// Layout tutorial step 13: polish — light chrome + real TextInput compose.
//
//	go run . --png out.png
package main

import (
	"fmt"
	"os"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 1100, 720

var servers = []struct {
	letter string
	hue    float32
}{
	{"A", 10}, {"B", 40}, {"C", 120}, {"D", 200}, {"E", 280}, {"F", 320},
}

var channels = []string{
	"general", "random", "help", "showcase", "off-topic",
	"announcements", "voice-lobby", "dev-log", "design", "meta",
	"reading-group", "music",
}

type msg struct {
	author, body, time string
}

var messages = []msg{
	{"alex", "Welcome to the layout tutorial shell.", "10:01"},
	{"blair", "Panels are just nested Row / column containers.", "10:02"},
	{"casey", "Grow(1) takes leftover space on the main axis.", "10:03"},
	{"alex", "The engine root is already window-sized.", "10:04"},
	{"devon", "Debug colors made the boxes obvious at first.", "10:05"},
	{"blair", "Now the structure is the same without the rainbow.", "10:06"},
	{"casey", "Viewport keeps compose pinned under messages.", "10:07"},
	{"alex", "Compose uses a real TextInput widget.", "10:08"},
	{"devon", "Message lists at scale → VirtualList next step.", "10:09"},
	{"blair", "Compose strip stays at the bottom via column layout.", "10:10"},
	{"casey", "Header is intrinsic; middle is Viewport.", "10:11"},
	{"alex", "Members sit in a fixed-width column on the right.", "10:12"},
	{"devon", "Try resizing the live window interactively.", "10:13"},
	{"blair", "Fixed-width rails do not steal center space.", "10:14"},
	{"casey", "That's the polished light shell.", "10:15"},
}

var members = []struct {
	name string
	hue  float32
}{
	{"alex", 10},
	{"blair", 40},
	{"casey", 120},
	{"devon", 200},
	{"ellis", 260},
	{"frank", 300},
	{"gray", 330},
	{"harper", 180},
}

var draft string

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frame); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Layout shell — step 13", winW, winH)
	app.Run(frame)
}

func frame() {
	// Light app chrome (readable defaults for standard controls).
	const (
		bgMain    float32 = 97 // near-white main
		bgSide    float32 = 94
		bgRail    float32 = 92
		bgTop     float32 = 100
		borderA   float32 = 0.08
		textPrim  float32 = 18
		textMuted float32 = 45
	)

	ModAttrs(Background(220, 6, bgMain, 1))

	Container(Attrs(Expand, FixHeight(48), Background(0, 0, bgTop, 1), Pad2(0, 14), CrossMid), func() {
		Label("Layout shell", FontSize(15), FontWeight(WeightSemibold), TextColor(0, 0, textPrim, 1))
		Filler(1)
		Label("tutorial · step 13", FontSize(12), TextColor(0, 0, textMuted, 1))
	})
	Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, borderA)))

	Container(Attrs(Row, Grow(1), Expand), func() {
		Container(Attrs(FixWidth(72), Expand, Background(220, 6, bgRail, 1), Pad(8), Gap(8)), func() {
			for _, s := range servers {
				Container(Attrs(FixSize(48, 48), Corners(16), Background(s.hue, 50, 55, 1), Center), func() {
					Label(s.letter, FontSize(18), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
				})
			}
		})
		Element(Attrs(FixWidth(1), Expand, Background(0, 0, 0, borderA)))

		Container(Attrs(Row, Grow(1), Expand), func() {
			Container(Attrs(FixWidth(240), Expand, Background(220, 6, bgSide, 1)), func() {
				Container(Attrs(Expand, FixHeight(44), Pad2(0, 12), CrossMid), func() {
					Label("Channels", FontSize(12), FontWeight(WeightBold), TextColor(0, 0, textMuted, 1))
				})
				Container(Attrs(Viewport, Pad2(4, 8), Gap(2)), func() {
					ScrollOnInput()
					for i, name := range channels {
						bg := Vec4{0, 0, 0, 0}
						if i == 0 {
							bg = Vec4{220, 40, 92, 1} // light selection
						}
						Container(Attrs(Expand, Pad2(6, 8), Corners(4), BackgroundVec(bg)), func() {
							Label("# "+name, FontSize(14), TextColor(0, 0, textPrim, 1))
						})
					}
				})
			})
			Element(Attrs(FixWidth(1), Expand, Background(0, 0, 0, borderA)))

			Container(Attrs(Grow(1), Expand, Background(220, 6, bgMain, 1)), func() {
				Container(Attrs(Expand, FixHeight(48), Pad2(0, 14), CrossMid), func() {
					Label("# general", FontSize(16), FontWeight(WeightSemibold), TextColor(0, 0, textPrim, 1))
				})
				Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, borderA)))
				Container(Attrs(Viewport, Pad(14), Gap(12)), func() {
					ScrollOnInput()
					for _, m := range messages {
						Container(Attrs(Expand, Gap(3)), func() {
							Container(Attrs(Row, Gap(8), CrossMid), func() {
								Label(m.author, FontSize(13), FontWeight(WeightBold), TextColor(0, 0, textPrim, 1))
								Label(m.time, FontSize(11), TextColor(0, 0, textMuted, 1))
							})
							Label(m.body, FontSize(14), TextColor(0, 0, 28, 1))
						})
					}
				})
				Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, borderA)))
				// Real text field (underline + inner treatment from the widget).
				Container(Attrs(Expand, Pad(10), Gap(8), Row, CrossMid, Background(220, 6, 95, 1)), func() {
					a := DefaultTextInputAttrs()
					a.NoAutoFocus = true
					TextInputExt(&draft, a)
					if Button(0, "Send") && draft != "" {
						// tutorial: no-op append; typing still works live
						draft = ""
					}
				})
			})
			Element(Attrs(FixWidth(1), Expand, Background(0, 0, 0, borderA)))

			Container(Attrs(FixWidth(220), Expand, Background(220, 6, bgSide, 1)), func() {
				Container(Attrs(Expand, FixHeight(44), Pad2(0, 12), CrossMid), func() {
					Label("Online — "+fmt.Sprint(len(members)), FontSize(12), FontWeight(WeightBold), TextColor(0, 0, textMuted, 1))
				})
				Container(Attrs(Viewport, Pad2(6, 10), Gap(8)), func() {
					ScrollOnInput()
					for _, m := range members {
						Container(Attrs(Row, Expand, Gap(10), CrossMid), func() {
							Container(Attrs(FixSize(28, 28), Corners(14), Background(m.hue, 45, 55, 1), Center), func() {
								if len(m.name) > 0 {
									Label(string(m.name[0]), FontSize(12), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
								}
							})
							Label(m.name, FontSize(13), TextColor(0, 0, textPrim, 1))
						})
					}
				})
			})
		})
	})
}
