package main

import (
	"fmt"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

// Virtual list rows on the post screen:
//
//	0 … header rows (title / meta / url / body) as a single composite row
//	1 … comments
//
// Using one header row keeps VirtualList simple and matches the dir_weight pattern of
// flattening structure into a single list.
//
// Heights: ItemHeight is nil so VirtualList Measures postHeaderRow / commentRow
// under the row width (same builders as paint).

const (
	commentFontSize f32 = 15
	commentMetaSize f32 = 13
	indentPerLevel  f32 = 16
)

func postScreen() {
	appData.mu.Lock()
	post := appData.post
	loading := appData.postLoading
	err := appData.postErr
	loaded := appData.commentsLoaded
	// Flatten comments under the lock so expand is consistent with render.
	var visible []*CommentNode
	listVisibleComments(appData.comments, appData.expanded, &visible)
	// expanded / kidsLoading are only read for display; mutations go through
	// PressAction after unlock — fine for immediate mode.
	expanded := appData.expanded
	kidsLoading := appData.kidsLoading
	appData.mu.Unlock()

	// Sticky chrome: same pad/gap as the feed title row so Refresh stays put.
	Container(Attrs(Background(220, 14, 94, 1), Expand), func() {
		headerTitleRow("Thread", 18, func() {
			if ButtonExt("Back", ButtonAttrs{Icon: SymLeft, Accent: hnOrange}, DefaultButtonLook()) {
				closePost()
			}
		}, func() {
			if ButtonExt("", ButtonAttrs{Icon: SymRefresh, Accent: hnOrange, Disabled: loading}, DefaultButtonLook()) {
				refreshPost()
			}
		})
	})
	Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, 0.08)))

	Container(Attrs(Grow(1), Expand, Clip), func() {
		if post == nil && loading {
			Container(Attrs(Pad(16)), func() {
				Label("Loading post…", FontSize(16), TextColor(0, 0, 45, 1))
			})
			return
		}
		if post == nil && err != "" {
			Container(Attrs(Pad(16), Gap(10)), func() {
				Label("Failed: "+err, FontSize(15), TextColor(10, 70, 40, 1))
				if Button(NoIcon, "Back") {
					closePost()
				}
			})
			return
		}
		if post == nil {
			return
		}

		commentsLoading := loading && !loaded

		// item 0 = post header; 1.. = comments
		n := 1 + len(visible)
		itemKey := func(i int) any {
			if i == 0 {
				return "post-header"
			}
			return visible[i-1].Item.ID
		}
		itemView := func(i int, width f32) {
			if i == 0 {
				postHeaderRow(post, commentsLoading, err, width)
				return
			}
			commentRow(visible[i-1], expanded, kidsLoading, width)
		}

		// nil ItemHeight → VirtualList Measures itemView under the row width.
		VirtualListView(&appData.openID, n, itemKey, nil, itemView)
	})
}

// postHeader layout: outer full-width shell with side inset; Labels live in an
// inner column whose MaxWidth is the wrap budget and which has zero horizontal
// pad (Text reads MaxSize[0] directly — not content-box after pad).
const (
	postHeaderHInset f32 = 12
	postHeaderVPad   f32 = 12
	postHeaderGap    f32 = 8
)

func postHeaderTextMax(rowWidth f32) f32 {
	w := rowWidth - postHeaderHInset*2
	if w < 40 {
		return 40
	}
	return w
}

func postHeaderRow(post *Item, commentsLoading bool, err string, width f32) {
	textMax := postHeaderTextMax(width)
	// Full-bleed background; side inset is empty outer pad. Labels sit in a
	// child with MaxWidth(textMax) and Pad2(v, 0) so wrap == measure.
	Container(Attrs(Expand, MaxWidth(width), Background(0, 0, 100, 1), Pad2(0, postHeaderHInset)), func() {
		Container(Attrs(Expand, MaxWidth(textMax), Pad2(postHeaderVPad, 0), Gap(postHeaderGap)), func() {
			Label(post.Title, FontSize(20), FontWeight(WeightBold), TextColor(220, 25, 16, 1))

			meta := fmt.Sprintf("%d pts · %s · %s", post.Score, post.By, post.Timestamp())
			if post.By == "" {
				meta = fmt.Sprintf("%d pts · %s", post.Score, post.Timestamp())
			}
			if post.Type == "job" {
				meta = fmt.Sprintf("%s · %s", post.By, post.Timestamp())
			}
			Label(meta, FontSize(14), TextColor(0, 0, 40, 1))

			if post.URL != "" {
				Container(Attrs(Expand), func() {
					if PressAction() {
						OpenURL(post.URL)
					}
					if pressedFeedback() {
						ModAttrs(Background(210, 30, 96, 0.5))
					}
					Label(post.URL, FontSize(14), TextColor(210, 55, 35, 1))
				})
			}

			if body := post.PlainText(); body != "" {
				Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, 0.06)))
				Label(body, FontSize(16), TextColor(0, 0, 20, 1))
			}

			Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, 0.08)))

			switch {
			case commentsLoading:
				Label("Loading comments…", FontSize(14), TextColor(0, 0, 50, 1))
			case err != "":
				Label("Comments: "+err, FontSize(14), TextColor(10, 70, 40, 1))
			case post.Type == "job":
				Label("Job posting", FontSize(14), TextColor(0, 0, 50, 1))
			default:
				Label(fmt.Sprintf("%d comments", post.Descendants), FontSize(14), FontWeight(WeightSemibold), TextColor(0, 0, 35, 1))
			}
		})
	})
}

// Comment rows: no outer frame padding (cards sit edge-to-edge after indent).
// Inside padding lives on the white card shell; Labels sit in a nested column
// that inherits MaxWidth with pad peeled (cascade), so wrap == content box.
// Never put horizontal pad on the same container that is Label's current —
// Text reads MaxSize[0] raw, not content-box.
const (
	commentAccentW  f32 = 3
	commentInnerPad f32 = 10 // pad inside the white card only
	commentGap      f32 = 3  // meta ↔ body
	commentMetaIcon f32 = 15
	// Expand control: HN-orange affordance so folded threads read as tappable.
	expandHue f32 = 15
	expandSat f32 = 80
	expandLit f32 = 32
)

// commentCardMax is MaxWidth of the white card (indent + accent already subtracted).
func commentCardMax(depth int, rowWidth f32) f32 {
	w := rowWidth - indentPerLevel*f32(depth) - commentAccentW
	if w < 40 {
		return 40
	}
	return w
}

func commentRow(n *CommentNode, expanded, kidsLoading map[int]bool, width f32) {
	if n == nil || n.Item == nil {
		return
	}
	it := n.Item
	// Kid *ids* come with the parent item from Firebase; child bodies load on expand.
	hasKids := len(it.Kids) > 0
	isExpanded := expanded != nil && expanded[it.ID]
	isLoading := kidsLoading != nil && kidsLoading[it.ID]
	cardMax := commentCardMax(n.Depth, width)

	ContainerWithKey(it.ID, Attrs(Expand, MaxWidth(width)), func() {
		Container(Attrs(Row, Expand, Gap(0)), func() {
			for d := 0; d < n.Depth; d++ {
				hue := f32((d * 47) % 360)
				Container(Attrs(FixWidth(indentPerLevel), Expand, Background(hue, 35, 94, 1)), func() {
					Element(Attrs(FixWidth(1), Expand, Background(0, 0, 0, 0.15)))
				})
			}
			hue := f32((n.Depth * 47) % 360)
			Element(Attrs(FixWidth(commentAccentW), Expand, Background(hue, 50, 55, 1)))

			// Card shell: MaxWidth + inner pad + background. Labels must NOT be
			// direct children (would wrap at cardMax). Nested column inherits
			// MaxWidth = cardMax − 2*pad via cascade, H pad 0 → wrap matches measure.
			// Whole card is the expand hit target when there are replies.
			Container(Attrs(Grow(1), Expand, MaxWidth(cardMax), Pad(commentInnerPad), Background(0, 0, 100, 1)), func() {
				if hasKids {
					if PressAction() {
						toggleCommentExpand(it.ID)
					}
					if pressedFeedback() {
						ModAttrs(Background(0, 0, 97, 1))
					}
				}
				Container(Attrs(Expand, Gap(commentGap)), func() {
					Container(Attrs(Row, CrossMid, Expand, Gap(6)), func() {
						if hasKids {
							// Tinted chip so expand/collapse is the loudest meta signal.
							// Press feedback is the card wash (IsTouched on the shell).
							Container(Attrs(Row, CrossMid, Gap(4), Pad2(2, 6), Corners(6),
								Background(expandHue, 35, 94, 1)), func() {
								icon := SymRight
								if isExpanded && !isLoading {
									icon = SymDown
								}
								Icon(icon, FontSize(commentMetaIcon),
									TextColor(expandHue, expandSat, expandLit, 1))
								switch {
								case isLoading:
									Label("loading…",
										FontSize(12), FontWeight(WeightBold),
										TextColor(expandHue, expandSat, expandLit, 1))
								case !isExpanded:
									nKids := len(it.Kids)
									reply := fmt.Sprintf("%d replies", nKids)
									if nKids == 1 {
										reply = "1 reply"
									}
									Label(reply,
										FontSize(12), FontWeight(WeightBold),
										TextColor(expandHue, expandSat, expandLit, 1))
								}
							})
						} else {
							// Align leaf meta with parents that have a chevron chip.
							Element(Attrs(FixWidth(commentMetaIcon + 4)))
						}

						by := it.By
						if by == "" {
							by = "?"
						}
						Label(by, FontSize(commentMetaSize), FontWeight(WeightSemibold), TextColor(15, 60, 30, 1))
						Label(it.Timestamp(), FontSize(commentMetaSize), TextColor(0, 0, 50, 1))
						Filler(1)
					})

					body := it.PlainText()
					if body == "" {
						body = " "
					}
					Label(body, FontSize(commentFontSize), TextColor(0, 0, 18, 1))
				})
			})
		})
	})
}
