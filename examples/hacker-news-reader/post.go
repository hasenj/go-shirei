package main

import (
	"fmt"
	"maps"

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

func postScreen() {
	colors := readerColors()
	appData.mu.Lock()
	post := appData.post
	loading := appData.postLoading
	err := appData.postErr
	loaded := appData.commentsLoaded
	// Flatten comments under the lock so expand is consistent with render.
	var visible []*CommentNode
	listVisibleComments(appData.comments, appData.expanded, &visible)
	expanded := maps.Clone(appData.expanded)
	kidsLoading := maps.Clone(appData.kidsLoading)
	feed := appData.feed
	appData.mu.Unlock()

	Container(Attrs(Row, Expand, CrossMid, Pad2(10, 14), BackgroundVec(colors.header)), func() {
		Container(Attrs(FixWidth(86)), func() {
			NextAccessName("back_to_feed")
			if readerHeaderButton(SymLeft, feed.Label(), false, colors) {
				closePost()
			}
		})
		Container(Attrs(Grow(1), Center), func() {
			Label("Discussion", FontSize(16), FontWeight(WeightBold), TextColorVec(colors.headerText))
		})
		Container(Attrs(FixWidth(86), Row, MainAlign(AlignEnd)), func() {
			NextAccessName("refresh_post")
			NextAccessLabel("Refresh discussion")
			if readerHeaderButton(SymRefresh, "", loading, colors) {
				refreshPost()
			}
		})
	})

	Container(Attrs(Grow(1), Expand, Clip), func() {
		if post == nil && loading {
			Container(Attrs(Pad(16)), func() {
				Label("Loading post…", FontSize(16))
			})
			return
		}
		if post == nil && err != "" {
			Container(Attrs(Pad(16), Gap(10)), func() {
				Label("Failed: "+err, FontSize(15), TextColorVec(CurrentColorScheme.List.Error))
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
			commentRow(visible[i-1], expanded, kidsLoading, post.By, width)
		}

		// nil ItemHeight → VirtualList Measures itemView under the row width.
		VirtualListView(&appData.openID, n, itemKey, nil, itemView)
	})
	readerDivider()
	Container(Attrs(Row, Expand, CrossMid, FixHeight(30), Pad2(0, 14), BackgroundVec(colors.page)), func() {
		Label("Hacker News", FontSize(11), TextColorVec(colors.muted))
		Filler(1)
		Label("Read only", FontSize(11), TextColorVec(colors.muted))
	})

}

// The post summary scrolls with the comment tree.
func postHeaderRow(post *Item, commentsLoading bool, err string, width f32) {
	colors := readerColors()
	Container(Attrs(Expand, MaxWidth(width), Pad2(10, 10)), func() {
		Container(Attrs(Expand, Gap(10), Pad(14), Corners(10), BorderWidth(1), BorderColorVec(colors.border), BackgroundVec(colors.featured)), func() {
			Label(post.Title, FontSize(20), FontWeight(WeightBold), TextColorVec(colors.text))
			if post.Type == "job" {
				Label("Job · "+post.By+" · "+relativeTime(post.Time), FontSize(12), TextColorVec(colors.muted))
			} else {
				Container(Attrs(Row, Wrap, CrossMid, Gap(5)), func() {
					Label(fmt.Sprint(post.Score), FontSize(12), FontWeight(WeightBold), TextColorVec(colors.accent))
					Label("points · "+post.By+" · "+relativeTime(post.Time), FontSize(12), TextColorVec(colors.muted))
				})
			}
			if post.URL != "" {
				Container(Attrs(Row, Wrap, CrossMid, Gap(10)), func() {
					NextAccessName("read_article")
					if ButtonStyled("Read article", ButtonAttrs{Icon: SymExternal, TextSize: 12},
						ButtonLook{TextSize: 12, PadScale: .85},
						readerButtonStyle(colors.featured, colors.accent, colors.accent), colors.accent) {
						OpenURL(post.URL)
					}
					Label(shortURL(post.URL), FontSize(12), TextColorVec(colors.muted))
				})
			}
			if body := post.PlainText(); body != "" {
				Label(body, FontSize(14), TextColorVec(colors.text))
			}
		})
		Container(Attrs(Expand, Pad2(12, 4)), func() {
			switch {
			case commentsLoading:
				Label("Loading comments…", FontSize(12), TextColorVec(colors.muted))
			case err != "":
				Label("Comments: "+err, FontSize(12), TextColorVec(CurrentColorScheme.List.Error))
			case post.Type == "job":
				Label("Job posting", FontSize(12), TextColorVec(colors.muted))
			default:
				Label(fmt.Sprintf("%d comments", post.Descendants), FontSize(13), FontWeight(WeightSemibold), TextColorVec(colors.text))
			}
		})
		readerDivider()
	})
}

// Deep threads cap their indentation to preserve room for comment text.
func commentRow(n *CommentNode, expanded, kidsLoading map[int]bool, author string, width f32) {
	if n == nil || n.Item == nil {
		return
	}
	colors := readerColors()
	it := n.Item
	hasKids := len(it.Kids) > 0
	isExpanded, isLoading := expanded[it.ID], kidsLoading[it.ID]
	depth := min(n.Depth, int(max(0, (width-240)/18)))
	NextAccessName("comment")
	NextAccessValue(fmt.Sprint(it.ID))
	ContainerWithKey(it.ID, Attrs(Expand, MaxWidth(width), Pad2(4, 10)), func() {
		AssignAccess()
		Container(Attrs(Row, Expand, Gap(6)), func() {
			for d := 0; d < depth; d++ {
				Container(Attrs(FixWidth(12), Expand), func() {
					Element(Attrs(FixWidth(2), Grow(1), BackgroundVec(colors.thread)))
				})
			}
			Container(Attrs(Grow(1), MaxWidth(max(40, width-20-f32(depth)*18)), Gap(8), Pad(12), Corners(9), BorderWidth(1), BorderColorVec(colors.border), BackgroundVec(colors.card)), func() {
				Container(Attrs(Row, Expand, CrossMid, Gap(6)), func() {
					by := it.By
					if by == "" {
						by = "[deleted]"
					}
					Container(Attrs(MaxWidth(max(80, (width-44-f32(depth)*18)/2))), func() {
						Label(by, FontSize(12), FontWeight(WeightSemibold), TextColorVec(colors.text))
					})
					if by == author && author != "" {
						Label("author", FontSize(10), TextColorVec(colors.accent))
					}
					Label(relativeTime(it.Time), FontSize(11), TextColorVec(colors.muted))
				})
				Label(it.PlainText(), FontSize(14), TextColorVec(colors.text))
				if hasKids {
					label := fmt.Sprintf("%d replies", len(it.Kids))
					if len(it.Kids) == 1 {
						label = "1 reply"
					}
					if isLoading {
						label = "Loading…"
					}
					icon := SymRight
					if isExpanded {
						icon = SymDown
					}
					NextAccessName("comment_replies")
					NextAccessValue(fmt.Sprint(it.ID))
					NextAccessChecked(isExpanded)
					NextAccessLabel("Show or hide replies")
					if ButtonStyled(label, ButtonAttrs{Icon: icon, TextSize: 12},
						ButtonLook{TextSize: 12, PadScale: .85},
						readerButtonStyle(colors.tint, colors.border, colors.accent), colors.accent) {
						toggleCommentExpand(it.ID)
					}
				}
			})
		})
	})
}
