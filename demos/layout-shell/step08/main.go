// Layout tutorial step 08: channel list (header + scrollable rows).
//
//	go run . --png out.png
package main

import (
	"fmt"
	"os"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
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

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frame); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Layout shell — step 08", winW, winH)
	app.Run(frame)
}

func frame() {
	Container(Attrs(Expand, FixHeight(48), Background(280, 45, 42, 1), Center), func() {
		Label("top bar", FontSize(16), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
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
			// Grow+Clip+Scroll — fine for a short channel list (step 09 shows
			// when this recipe fails for a taller message column).
			Container(Attrs(FixWidth(240), Expand, Background(220, 30, 52, 1)), func() {
				Container(Attrs(Expand, FixHeight(44), Pad2(0, 12), CrossMid), func() {
					Label("Channels", FontSize(14), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
				})
				Container(Attrs(Grow(1), Expand, Clip, Pad2(4, 8), Gap(2)), func() {
					ScrollOnInput()
					for _, name := range channels {
						Container(Attrs(Expand, Pad2(6, 8), Corners(4), Background(0, 0, 0, 0.12)), func() {
							Label("# "+name, FontSize(14), TextColor(0, 0, 98, 1))
						})
					}
				})
			})

			// Main still has header | messages | compose slots (filled in step 09).
			Container(Attrs(Grow(1), Expand, Background(200, 20, 78, 1)), func() {
				Container(Attrs(Expand, FixHeight(48), Background(200, 25, 70, 1), Center), func() {
					Label("header", FontSize(14), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
				})
				Container(Attrs(Grow(1), Expand, Background(200, 15, 82, 1), Center), func() {
					Label("messages", FontSize(14), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
				})
				Container(Attrs(Expand, FixHeight(56), Background(200, 30, 65, 1), Center), func() {
					Label("compose", FontSize(14), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
				})
			})
			Container(Attrs(FixWidth(220), Expand, Background(150, 35, 48, 1), Center), func() {
				Label("members", FontSize(16), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
			})
		})
	})
}
