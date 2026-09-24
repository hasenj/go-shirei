// Custom-widget tutorial step 16: a live dark-mode switch for the shared shell.
//
//	go run . --dark=false
//	go run . --png out.png
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 1100, 720

var darkMode = true

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
	flag.BoolVar(&darkMode, "dark", true, "use the dark color scheme")
	png := flag.String("png", "", "write one settled frame to PATH and exit")
	flag.Parse()
	if *png != "" {
		if err := RenderToPNG(*png, winW, winH, frame); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Color schemes (step 16)", winW, winH)
	app.Run(frame)
}

func frame() {
	SetDarkMode(darkMode)
	scheme := CurrentColorScheme
	ModAttrs(UseSurface(SurfaceCanvas))

	Container(Attrs(Row, Expand, FixHeight(48), UseSurface(SurfacePanel), Pad2(0, 14), Gap(12), CrossMid), func() {
		Label("Layout shell", FontSize(15), FontWeight(WeightSemibold))
		Filler(1)
		NextAccessName("dark_mode")
		CheckBox(&darkMode, "Dark mode")
		Label(fmt.Sprintf("%d msgs · %d members", len(messages), len(members)),
			FontSize(12), TextColorVec(scheme.List.Muted))
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
				Container(Attrs(Grow(1), Expand), func() {
					VirtualListView(msgList, len(messages),
						func(i int) any { return messages[i].id },
						nil,
						func(i int, width f32) {
							m := messages[i]
							Container(Attrs(Expand, MaxWidth(width), Pad2(6, 14), Gap(3)), func() {
								Container(Attrs(Row, Gap(8), CrossMid), func() {
									Label(m.author, FontSize(13), FontWeight(WeightBold))
									Label(m.time, FontSize(11), TextColorVec(scheme.List.Muted))
								})
								Label(m.body, FontSize(14))
							})
						},
					)
				})
				Element(Attrs(Expand, FixHeight(1), BackgroundVec(scheme.Surfaces.Panel.Border)))
				// Custom compose: padded bar + borderless field + circular send.
				// See chatCompose below and docs/custom-widgets-tutorial.md.
				chatCompose(&draft, &messages)
			})
			Element(Attrs(FixWidth(1), Expand, BackgroundVec(scheme.Surfaces.Panel.Border)))

			Container(Attrs(FixWidth(220), Expand, UseSurface(SurfacePanel)), func() {
				Container(Attrs(Expand, FixHeight(44), Pad2(0, 12), Center), func() {
					Label(fmt.Sprintf("Online — %d", len(members)), FontSize(12), FontWeight(WeightBold), TextColorVec(scheme.List.Muted))
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
								Label(m.name, FontSize(13))
							})
						},
					)
				})
			})
		})
	})
}

// chatCompose draws a multiline field and send button inside a shared pill.
func chatCompose(draft *string, messages *[]msg) {
	const fieldPad float32 = 10
	scheme := CurrentColorScheme

	Container(Attrs(Expand, Pad(12), UseSurface(SurfaceCanvas)), func() {
		Container(Attrs(Expand, Row, CrossMid, Gap(8), Pad2(6, 8),
			Corners(12), UseSurface(SurfacePanel), BorderWidth(1)), func() {
			cfg := TextInputConfigWithStyle(TextInputConfig{
				FontSize:    DefaultTextSize,
				Padding:     N4(fieldPad),
				MaxLines:    0,
				Wrap:        true,
				Rows:        2,
				NoAutoFocus: true,
			}, scheme.TextInput)
			boxH := float32(cfg.Rows)*cfg.FontSize + PadSize(cfg.Padding)[1]

			Container(Attrs(Focusable, Clip, Grow(1), PadVec(cfg.Padding),
				MinSize(80, boxH), MaxSizeVec(Vec2{0, boxH}),
				Corners(6), BorderWidth(1)), func() {
				st := ProcessTextInput(draft, cfg)
				NextAccessRole("text")
				NextAccessLabel("Message")
				NextAccessEditable(true, true)
				NextAccessValue(*draft)
				AssignAccess()
				if st.HasFocus {
					ModAttrs(BorderColorVec(scheme.FocusRing))
				}
				DrawTextInputPlain(st, cfg)
			})

			canSend := *draft != ""
			if sendCircle(!canSend) {
				text := *draft
				*draft = ""
				*messages = append(*messages, msg{
					id: len(*messages) + 1, author: "you", body: text,
					time: time.Now().Format("15:04"),
				})
				RequestNextFrame()
			}
		})
	})
}

// sendCircle combines button interaction with a circular face from the active scheme.
func sendCircle(disabled bool) bool {
	const size float32 = 36
	var clicked bool
	Container(Attrs(FixSize(size, size), Corners(size/2), BorderWidth(2), Center), func() {
		st := ProcessButtonEvents(disabled)
		NextAccessRole("button")
		NextAccessLabel("Send message")
		NextAccessDisabled(disabled)
		AssignAccess()
		clicked = st.Clicked

		scheme := CurrentColorScheme
		style := scheme.Buttons.Primary
		paint := style.Normal
		switch {
		case st.Disabled:
			paint = style.Disabled
		case st.Active:
			paint = style.Pressed
		case st.Hovered:
			paint = style.Hovered
		}
		ModAttrs(BackgroundVec(paint.Background), BorderColorVec(paint.Border))
		if st.FocusVisible && !disabled {
			ModAttrs(BorderColorVec(scheme.FocusRing))
		}
		Icon(TypArrowUp, FontSize(18), TextColorVec(paint.Text))
	})
	return clicked
}
