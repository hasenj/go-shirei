// Layout tutorial step 13: shared color schemes + real TextInput compose.
//
//	go run . --png out.png
package main

import (
	"flag"
	"fmt"
	"os"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 1100, 720

var darkMode bool

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
	{"devon", "UseSurface pairs background and text colors.", "10:05"},
	{"blair", "SetDarkMode selects the active color scheme.", "10:06"},
	{"casey", "Viewport keeps compose pinned under messages.", "10:07"},
	{"alex", "Compose uses a real TextInput widget.", "10:08"},
	{"devon", "Message lists at scale → VirtualList next step.", "10:09"},
	{"blair", "Compose strip stays at the bottom via column layout.", "10:10"},
	{"casey", "Header is intrinsic; middle is Viewport.", "10:11"},
	{"alex", "Members sit in a fixed-width column on the right.", "10:12"},
	{"devon", "Try resizing the live window interactively.", "10:13"},
	{"blair", "Fixed-width rails do not steal center space.", "10:14"},
	{"casey", "One shell works in light and dark mode.", "10:15"},
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
	flag.BoolVar(&darkMode, "dark", false, "use the dark color scheme")
	png := flag.String("png", "", "write one settled frame to PATH and exit")
	flag.Parse()
	if *png != "" {
		if err := RenderToPNG(*png, winW, winH, frame); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Layout shell — step 13", winW, winH)
	app.Run(frame)
}

func frame() {
	SetDarkMode(darkMode)
	scheme := CurrentColorScheme
	ModAttrs(UseSurface(SurfaceCanvas))

	Container(Attrs(Row, Expand, FixHeight(48), UseSurface(SurfacePanel), Pad2(0, 14), CrossMid), func() {
		Label("Layout shell", FontSize(15), FontWeight(WeightSemibold))
		Filler(1)
		Label("tutorial · step 13", FontSize(12), TextColorVec(scheme.List.Muted))
	})
	Element(Attrs(Expand, FixHeight(1), BackgroundVec(scheme.Surfaces.Panel.Border)))

	Container(Attrs(Row, Grow(1), Expand), func() {
		Container(Attrs(FixWidth(72), Expand, UseSurface(SurfaceCanvas), Pad(8), Gap(8)), func() {
			for _, s := range servers {
				Container(Attrs(FixSize(48, 48), Corners(16), Background(s.hue, 50, 55, 1), Center), func() {
					Label(s.letter, FontSize(18), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
				})
			}
		})
		Element(Attrs(FixWidth(1), Expand, BackgroundVec(scheme.Surfaces.Panel.Border)))

		Container(Attrs(Row, Grow(1), Expand), func() {
			Container(Attrs(FixWidth(240), Expand, UseSurface(SurfacePanel)), func() {
				Container(Attrs(Expand, FixHeight(44), Pad2(0, 12), Center), func() {
					Label("Channels", FontSize(12), FontWeight(WeightBold), TextColorVec(scheme.List.Muted))
				})
				Container(Attrs(Viewport, Pad2(4, 8), Gap(2)), func() {
					ScrollOnInput()
					for i, name := range channels {
						bg, text := Vec4{}, scheme.Surfaces.Panel.Text
						if i == 0 {
							bg, text = scheme.List.Selected.Background, scheme.List.Selected.Text
						}
						Container(Attrs(Expand, Pad2(6, 8), Corners(4), BackgroundVec(bg)), func() {
							Label("# "+name, FontSize(14), TextColorVec(text))
						})
					}
				})
			})
			Element(Attrs(FixWidth(1), Expand, BackgroundVec(scheme.Surfaces.Panel.Border)))

			Container(Attrs(Grow(1), Expand, UseSurface(SurfaceCanvas)), func() {
				Container(Attrs(Expand, FixHeight(48), Pad2(0, 14), Center), func() {
					Label("# general", FontSize(16), FontWeight(WeightSemibold))
				})
				Element(Attrs(Expand, FixHeight(1), BackgroundVec(scheme.Surfaces.Panel.Border)))
				Container(Attrs(Viewport, Pad(14), Gap(12)), func() {
					ScrollOnInput()
					for _, m := range messages {
						Container(Attrs(Expand, Gap(3)), func() {
							Container(Attrs(Row, Gap(8), CrossMid), func() {
								Label(m.author, FontSize(13), FontWeight(WeightBold))
								Label(m.time, FontSize(11), TextColorVec(scheme.List.Muted))
							})
							Label(m.body, FontSize(14))
						})
					}
				})
				Element(Attrs(Expand, FixHeight(1), BackgroundVec(scheme.Surfaces.Panel.Border)))
				// The stock field resolves its paint from the active scheme.
				Container(Attrs(Expand, Pad(10), Gap(8), Row, CrossMid, UseSurface(SurfacePanel)), func() {
					a := DefaultTextInputAttrs()
					a.NoAutoFocus = true
					TextInputExt(&draft, a)
					if Button(NoIcon, "Send") && draft != "" {
						// tutorial: no-op append; typing still works live
						draft = ""
					}
				})
			})
			Element(Attrs(FixWidth(1), Expand, BackgroundVec(scheme.Surfaces.Panel.Border)))

			Container(Attrs(FixWidth(220), Expand, UseSurface(SurfacePanel)), func() {
				Container(Attrs(Expand, FixHeight(44), Pad2(0, 12), Center), func() {
					Label("Online — "+fmt.Sprint(len(members)), FontSize(12), FontWeight(WeightBold), TextColorVec(scheme.List.Muted))
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
							Label(m.name, FontSize(13))
						})
					}
				})
			})
		})
	})
}
