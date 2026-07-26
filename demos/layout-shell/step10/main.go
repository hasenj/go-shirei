// Layout tutorial step 10: fix compose with Extrinsic.
//
// Same as step 08, but the message region is Extrinsic — height comes from the
// parent budget, not from listing every message. Compose stays fully on screen.
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
	{"devon", "Debug colors make the boxes obvious at first.", "10:05"},
	{"blair", "Later we drop the colors and keep the structure.", "10:06"},
	{"casey", "Watch the compose strip at the bottom of this column.", "10:07"},
	{"alex", "Step 09 used Grow+Expand+Clip only — compose broke.", "10:08"},
	{"devon", "Extrinsic: size from constraints, not from children.", "10:09"},
	{"blair", "Compose should pin under the messages — fully visible now.", "10:10"},
	{"casey", "Scroll still works inside the budgeted middle region.", "10:11"},
	{"alex", "Step 11 will package this as Viewport.", "10:12"},
	{"devon", "Try resizing the live window interactively.", "10:13"},
	{"blair", "Fixed-width rails do not steal center space.", "10:14"},
	{"casey", "That's the Extrinsic fix.", "10:15"},
}

var draft string // compose field buffer

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frame); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Layout shell — step 10 (Extrinsic)", winW, winH)
	app.Run(frame)
}

func frame() {
	Container(Attrs(Expand, FixHeight(48), Background(280, 45, 42, 1), Center), func() {
		Label("top bar · step 10 Extrinsic", FontSize(16), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
	})

	Container(Attrs(Row, Grow(1), Expand), func() {
		Container(Attrs(FixWidth(72), Expand, Background(260, 40, 32, 1), Pad(8), Gap(8)), func() {
			for _, s := range servers {
				Container(Attrs(FixSize(48, 48), Corners(16), Background(s.hue, 55, 50, 1), Center), func() {
					Label(s.letter, FontSize(18), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
				})
			}
		})

		Container(Attrs(Row, Grow(1), Expand), func() {
			Container(Attrs(FixWidth(240), Expand, Background(220, 30, 52, 1)), func() {
				Container(Attrs(Expand, FixHeight(44), Pad2(0, 12), CrossMid), func() {
					Label("Channels", FontSize(14), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
				})
				Container(Attrs(Grow(1), Expand, Clip, Extrinsic, Pad2(4, 8), Gap(2)), func() {
					ScrollOnInput()
					for _, name := range channels {
						Container(Attrs(Expand, Pad2(6, 8), Corners(4), Background(0, 0, 0, 0.12)), func() {
							Label("# "+name, FontSize(14), TextColor(0, 0, 98, 1))
						})
					}
				})
			})

			Container(Attrs(Grow(1), Expand, Background(200, 20, 78, 1)), func() {
				Container(Attrs(Expand, FixHeight(48), Pad2(0, 14), CrossMid, Background(0, 0, 0, 0.1)), func() {
					Label("# general", FontSize(16), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
				})
				// FIX: Extrinsic — middle height from parent, not message count.
				Container(Attrs(Grow(1), Expand, Clip, Extrinsic, Pad(12), Gap(10)), func() {
					ScrollOnInput()
					for _, m := range messages {
						Container(Attrs(Expand, Gap(2)), func() {
							Container(Attrs(Row, Gap(8), CrossMid), func() {
								Label(m.author, FontSize(13), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
								Label(m.time, FontSize(11), TextColor(0, 0, 90, 0.7))
							})
							Label(m.body, FontSize(14), TextColor(0, 0, 98, 1))
						})
					}
				})
				Container(Attrs(Expand, Pad(10), Gap(8), Row, CrossMid, Background(0, 0, 0, 0.12)), func() {
					a := DefaultTextInputAttrs()
					a.NoAutoFocus = true
					TextInputExt(&draft, a)
					Button(NoIcon, "Send")
				})
			})

			Container(Attrs(FixWidth(220), Expand, Background(150, 35, 48, 1), Center), func() {
				Label("members", FontSize(16), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
			})
		})
	})
}
