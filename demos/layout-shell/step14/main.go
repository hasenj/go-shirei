// Layout tutorial step 14: VirtualList for messages + members (scale).
//
// Light shell as step 13; long lists use VirtualListView. ItemHeight nil
// → Measure(ItemView). Details: docs/virtual-list.md
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

type f32 = float32

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
	id           int
	author, body string
	time         string
}

type member struct {
	id   int
	name string
	hue  float32
}

var (
	messages   []msg
	members    []member
	msgList    = new(int)
	memberList = new(int)
	draft      string
)

func init() {
	authors := []string{"alex", "blair", "casey", "devon", "ellis", "frank", "gray", "harper"}
	bodies := []string{
		"Grow(1) takes leftover space on the main axis.",
		"The engine root is already window-sized.",
		"VirtualList only builds visible rows.",
		"ItemHeight nil means Measure runs the same ItemView.",
		"Viewport taught us extrinsic scroll panes; VL scales them.",
		"Stable ItemKey (id), not the slice index, survives reordering.",
	}
	messages = make([]msg, 800)
	for i := range messages {
		messages[i] = msg{
			id: i + 1, author: authors[i%len(authors)],
			body: fmt.Sprintf("#%d — %s", i+1, bodies[i%len(bodies)]),
			time: fmt.Sprintf("%02d:%02d", 10+(i/60)%12, i%60),
		}
	}
	members = make([]member, 250)
	hues := []float32{10, 40, 120, 200, 260, 300, 330, 180}
	for i := range members {
		members[i] = member{
			id: i + 1, name: fmt.Sprintf("%s-%03d", authors[i%len(authors)], i+1),
			hue: hues[i%len(hues)],
		}
	}
}

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frame); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Layout shell — step 14", winW, winH)
	app.Run(frame)
}

func frame() {
	const (
		bgMain    float32 = 97
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
		Label(fmt.Sprintf("step 14 · %d msgs · %d members", len(messages), len(members)),
			FontSize(12), TextColor(0, 0, textMuted, 1))
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
							bg = Vec4{220, 40, 92, 1}
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
				Container(Attrs(Grow(1), Expand), func() {
					VirtualListView(msgList, len(messages),
						func(i int) any { return messages[i].id },
						nil,
						func(i int, width f32) {
							m := messages[i]
							Container(Attrs(Expand, MaxWidth(width), Pad2(6, 14), Gap(3)), func() {
								Container(Attrs(Row, Gap(8), CrossMid), func() {
									Label(m.author, FontSize(13), FontWeight(WeightBold), TextColor(0, 0, textPrim, 1))
									Label(m.time, FontSize(11), TextColor(0, 0, textMuted, 1))
								})
								Label(m.body, FontSize(14), TextColor(0, 0, 28, 1))
							})
						},
					)
				})
				Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, borderA)))
				Container(Attrs(Expand, Pad(10), Gap(8), Row, CrossMid, Background(220, 6, 95, 1)), func() {
					a := DefaultTextInputAttrs()
					a.NoAutoFocus = true
					TextInputExt(&draft, a)
					if Button(0, "Send") && draft != "" {
						draft = ""
					}
				})
			})
			Element(Attrs(FixWidth(1), Expand, Background(0, 0, 0, borderA)))

			Container(Attrs(FixWidth(220), Expand, Background(220, 6, bgSide, 1)), func() {
				Container(Attrs(Expand, FixHeight(44), Pad2(0, 12), CrossMid), func() {
					Label(fmt.Sprintf("Online — %d", len(members)), FontSize(12), FontWeight(WeightBold), TextColor(0, 0, textMuted, 1))
				})
				Container(Attrs(Grow(1), Expand), func() {
					VirtualListView(memberList, len(members),
						func(i int) any { return members[i].id },
						nil,
						func(i int, width f32) {
							m := members[i]
							Container(Attrs(Row, Expand, MaxWidth(width), Gap(10), CrossMid, Pad2(4, 10)), func() {
								Container(Attrs(FixSize(28, 28), Corners(14), Background(m.hue, 45, 55, 1), Center), func() {
									if len(m.name) > 0 {
										Label(string(m.name[0]), FontSize(12), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
									}
								})
								Label(m.name, FontSize(13), TextColor(0, 0, textPrim, 1))
							})
						},
					)
				})
			})
		})
	})
}
