// Custom-widgets tutorial sample: dark chat shell + light scrollbar tint.
//
// Same structure as demos/layout-shell/step15 (compose + VirtualLists). The
// package default scrollbar is already a modern overlay; this step skins the
// product palette to dark surfaces and registers a light thumb once so bars
// stay readable on dark panels (SetDefaultScrollBar).
//
// Tutorial: docs/custom-widgets-tutorial.md (§5)
// Previous sample: demos/layout-shell/step15
//
//	go run . --png out.png
package main

import (
	"fmt"
	"os"
	"time"

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
	// Package default is already a modern overlay (gray pill, transparent
	// track). On a dark product shell that gray can be too subtle — register
	// a light thumb once so every VirtualList / ScrollBars() call matches.
	SetDefaultScrollBar(darkShellScrollBar)

	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frame); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Dark shell (step 16)", winW, winH)
	app.Run(frame)
}

// darkShellScrollBar keeps the modern overlay geometry but paints a light
// thumb for dark panels. Interaction stays inside ScrollBarExt.
func darkShellScrollBar() ContainerId {
	return ScrollBarExt(ScrollBarAttrs{
		// Width/pad match the package default (SCROLLBAR_WIDTH hit target).
		TrackBG:        Vec4{}, // transparent
		ThumbMinHeight: 24,
		Thumb: func(size Vec2) {
			r := size[0] / 2
			if r < 1 {
				r = 1
			}
			Element(Attrs(
				FixSizeVec(size),
				Corners(r),
				Background(0, 0, 100, 0.28), // light pill on dark chrome
			))
		},
	})
}

func frame() {
	// Dark product palette (HSLA). Not a theme system — just named constants.
	const (
		bgApp     float32 = 16 // deepest chrome
		bgRail    float32 = 14
		bgSide    float32 = 18
		bgMain    float32 = 20
		bgTop     float32 = 18
		bgHeader  float32 = 18
		borderA   float32 = 0.22 // light lines on dark need more alpha
		textPrim  float32 = 92
		textMuted float32 = 62
		textBody  float32 = 82
	)

	ModAttrs(Background(220, 10, bgApp, 1))

	// Window title strip: Row + CrossMid centers labels vertically in the bar.
	Container(Attrs(Row, Expand, FixHeight(48), Background(220, 10, bgTop, 1), Pad2(0, 14), CrossMid), func() {
		Label("Chat shell", FontSize(15), FontWeight(WeightSemibold), TextColor(0, 0, textPrim, 1))
		Filler(1)
		Label(fmt.Sprintf("dark · %d msgs · %d members", len(messages), len(members)),
			FontSize(12), TextColor(0, 0, textMuted, 1))
	})
	Element(Attrs(Expand, FixHeight(1), Background(0, 0, 100, borderA)))

	Container(Attrs(Row, Grow(1), Expand), func() {
		Container(Attrs(FixWidth(72), Expand, Background(220, 12, bgRail, 1), Pad(8), Gap(8)), func() {
			for _, s := range servers {
				Container(Attrs(FixSize(48, 48), Corners(16), Background(s.hue, 45, 42, 1), Center), func() {
					Label(s.letter, FontSize(18), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
				})
			}
		})
		Element(Attrs(FixWidth(1), Expand, Background(0, 0, 100, borderA)))

		Container(Attrs(Row, Grow(1), Expand), func() {
			// --- channels -------------------------------------------------
			Container(Attrs(FixWidth(240), Expand, Background(220, 10, bgSide, 1)), func() {
				sectionTitle("Channels", 12, textMuted)
				Container(Attrs(Viewport, Pad2(4, 8), Gap(2)), func() {
					ScrollOnInput()
					// Small list: ScrollBars() uses DefaultScrollBar → darkShellScrollBar.
					ScrollBars()
					for i, name := range channels {
						bg := Vec4{0, 0, 0, 0}
						if i == 0 {
							bg = Vec4{220, 35, 28, 1}
						}
						Container(Attrs(Expand, Pad2(6, 8), Corners(4), BackgroundVec(bg)), func() {
							Label("# "+name, FontSize(14), TextColor(0, 0, textPrim, 1))
						})
					}
				})
			})
			Element(Attrs(FixWidth(1), Expand, Background(0, 0, 100, borderA)))

			// --- messages + compose ---------------------------------------
			Container(Attrs(Grow(1), Expand, Background(220, 8, bgMain, 1)), func() {
				sectionTitle("# general", 16, textPrim)
				Element(Attrs(Expand, FixHeight(1), Background(0, 0, 100, borderA)))
				Container(Attrs(Grow(1), Expand), func() {
					// VirtualList calls ScrollBars() internally → darkShellScrollBar.
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
								Label(m.body, FontSize(14), TextColor(0, 0, textBody, 1))
							})
						},
					)
				})
				Element(Attrs(Expand, FixHeight(1), Background(0, 0, 100, borderA)))
				chatCompose(&draft, &messages, textPrim)
			})
			Element(Attrs(FixWidth(1), Expand, Background(0, 0, 100, borderA)))

			// --- members --------------------------------------------------
			Container(Attrs(FixWidth(220), Expand, Background(220, 10, bgSide, 1)), func() {
				sectionTitle(fmt.Sprintf("Online — %d", len(members)), 12, textMuted)
				Container(Attrs(Grow(1), Expand), func() {
					VirtualListView(memberList, len(members),
						func(i int) any { return members[i].id },
						nil,
						func(i int, width f32) {
							m := members[i]
							Container(Attrs(Row, Expand, MaxWidth(width), Gap(10), CrossMid, Pad2(4, 10)), func() {
								Container(Attrs(FixSize(28, 28), Corners(14), Background(m.hue, 40, 45, 1), Center), func() {
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

// sectionTitle is a fixed-height pane header with the label centered in the
// bar (MainAlign + CrossAlign middle). CrossMid alone only centers on the
// horizontal axis in a column parent — labels still sat on the top edge.
func sectionTitle(title string, fontSize, textL float32) {
	Container(Attrs(Expand, FixHeight(44), Pad2(0, 12), Center,
		Background(220, 10, 18, 1),
	), func() {
		Label(title, FontSize(fontSize), FontWeight(WeightBold), TextColor(0, 0, textL, 1))
	})
}

// chatCompose — same process/paint idea as step 15, colors tuned for dark UI.
func chatCompose(draft *string, messages *[]msg, textPrim float32) {
	const (
		sendSize float32 = 36
		fieldPad float32 = 10
		barPad   float32 = 12
		accentH  float32 = 220
	)

	Container(Attrs(Expand, Pad(barPad), Background(220, 8, 18, 1)), func() {
		// Pill: slightly raised vs the strip.
		Container(Attrs(Expand, Row, CrossMid, Gap(8),
			Pad2(6, 8),
			Corners(12),
			Background(220, 8, 24, 1),
			BorderWidth(1),
			BorderColor(0, 0, 100, 0.12),
		), func() {
			cfg := TextInputConfig{
				FontSize:    DefaultTextSize,
				Padding:     N4(fieldPad),
				MaxLines:    0,
				Wrap:        true,
				Rows:        2,
				NoAutoFocus: true,
				TextColor:   Vec4{0, 0, textPrim, 1},
			}
			padSize := PadSize(cfg.Padding)
			boxH := float32(cfg.Rows)*cfg.FontSize + padSize[1]

			Container(Attrs(
				Focusable, Clip, Grow(1),
				PadVec(cfg.Padding),
				MinSize(80, boxH),
				MaxSizeVec(Vec2{0, boxH}),
				Background(0, 0, 100, 0),
			), func() {
				st := ProcessTextInput(draft, cfg)
				if st.HasFocus {
					ModAttrs(Background(220, 12, 28, 1))
				}
				DrawTextInputPlain(st, cfg)
			})

			canSend := *draft != ""
			sendAccent := Vec4{accentH, 55, 48, 1}
			if !canSend {
				sendAccent = Vec4{0, 0, 35, 1}
			}

			Container(Attrs(
				FixSize(sendSize, sendSize),
				Corners(sendSize/2),
				BackgroundVec(sendAccent),
				Center,
			), func() {
				bst := ProcessButtonEvents(!canSend)
				if bst.Hovered && canSend {
					ModAttrs(Background(accentH, 55, 54, 1))
				}
				if bst.Active && canSend {
					ModAttrs(Background(accentH, 55, 40, 1))
				}
				Icon(TypArrowUp, FontSize(18), TextColor(0, 0, 100, 1))

				if bst.Clicked && canSend {
					text := *draft
					*draft = ""
					n := len(*messages)
					*messages = append(*messages, msg{
						id:     n + 1,
						author: "you",
						body:   text,
						time:   time.Now().Format("15:04"),
					})
				}
			})
		})
	})
}
