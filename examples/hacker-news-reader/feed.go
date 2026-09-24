package main

import (
	"fmt"
	"math"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func hnAccent() Vec4 { return readerColors().accent }

func readerDivider() {
	Element(Attrs(Expand, FixHeight(1), BackgroundVec(readerColors().border)))
}

func feedScreen() {
	colors := readerColors()
	appData.mu.Lock()
	feed := appData.feed
	stories := append([]*Item(nil), appData.stories...)
	loading, more, err := appData.feedLoading, appData.feedMore, appData.feedErr
	totalIDs := len(appData.storyIDs)
	appData.mu.Unlock()

	Container(Attrs(Row, Expand, CrossMid, Pad2(10, 14), Gap(10), BackgroundVec(colors.header)), func() {
		Container(Attrs(FixSize(28, 28), Center, Corners(5), BackgroundVec(colors.headerText)), func() {
			Label("H", FontSize(18), FontWeight(WeightBold), TextColorVec(colors.header))
		})
		Label("Hacker News", FontSize(18), FontWeight(WeightBold), TextColorVec(colors.headerText))
		Filler(1)
		NextAccessName("refresh_feed")
		NextAccessLabel("Refresh stories")
		if readerHeaderButton(SymRefresh, "", loading || more, colors) {
			go loadFeed(true, false)
		}
	})
	selected := feedTabs(feed)
	if selected != feed {
		appData.mu.Lock()
		appData.feed = selected
		appData.mu.Unlock()
		go loadFeed(true, true)
	}
	feedActivityStrip(loading || more)
	NextAccessName("story_list")
	Container(Attrs(Grow(1), Expand, Clip), func() {
		AssignAccess()
		if loading && len(stories) == 0 {
			Container(Attrs(Expand, Grow(1), Center), func() { Label("Loading "+feed.Label()+"…", FontSize(16)) })
			return
		}
		if err != "" {
			Container(Attrs(Expand, Pad(14), Gap(8)), func() {
				Label("Could not load stories: "+err, FontSize(12), TextColorVec(CurrentColorScheme.List.Error))
				if Button(NoIcon, "Retry") {
					go loadFeed(true, false)
				}
			})
			if len(stories) == 0 {
				return
			}
		}
		if !loading && len(stories) == 0 {
			Container(Attrs(Expand, Pad(16)), func() { Label("No stories in this feed.", TextColorVec(CurrentColorScheme.List.Muted)) })
			return
		}
		VirtualListView(&appData.feed, len(stories), func(i int) any { return stories[i].ID }, nil, func(i int, width f32) {
			storyRow(stories[i], i+1, width)
		})
	})
	readerDivider()
	Container(Attrs(Row, Expand, CrossMid, FixHeight(46), Pad2(0, 14), BackgroundVec(colors.page)), func() {
		Label(fmt.Sprintf("%d stories", len(stories)), FontSize(11), TextColorVec(colors.muted))
		Filler(1)
		if len(stories) < totalIDs || more {
			label := "Load more"
			if more {
				label = "Loading…"
			}
			NextAccessName("load_more")
			if ButtonStyled(label, ButtonAttrs{Disabled: loading || more, TextSize: 12},
				ButtonLook{TextSize: 12, PadScale: .8},
				readerButtonStyle(colors.header, colors.header, colors.headerText), colors.accent) {
				go loadFeed(false, false)
			}
		}
	})
}

// feedActivityStrip sits between the segment bar and the list. Idle: a light
// hairline. Busy: a short orange segment that loops left→right (indeterminate).
func feedActivityStrip(active bool) {
	colors := readerColors()
	const trackH f32 = 2.5
	if !active {
		Element(Attrs(Expand, FixHeight(1), BackgroundVec(colors.border)))
		return
	}
	RequestNextFrame()
	Container(Attrs(Expand, FixHeight(trackH), BackgroundVec(colors.border), Clip, NoAnimate), func() {
		w := GetResolvedWidth()
		if w < 1 {
			w = GetHost().WindowSize[0]
		}
		// ~1/3 of the track; travels fully off one side before re-entering.
		barW := w * 0.34
		if barW < 48 {
			barW = 48
		}
		// One full traverse every ~0.9s.
		phase := float32(math.Mod(float64(time.Now().UnixMilli())/900, 1))
		x := -barW + (w+barW)*phase
		Element(Attrs(
			Float(x, 0),
			FixSize(barW, trackH),
			NoAnimate,
			ClickThrough,
			Corners(trackH/2),
			BackgroundVec(hnAccent()),
		))
	})
}

func storyRow(it *Item, rank int, width f32) {
	if it == nil {
		return
	}
	colors := readerColors()
	background := colors.card
	if rank == 1 {
		background = colors.featured
	}
	Container(Attrs(Expand, MaxWidth(width), Pad2(4, 8)), func() {
		NextAccessName("story")
		NextAccessValue(fmt.Sprint(it.ID))
		NextAccessLabel(it.Title)
		ContainerWithKey(it.ID, Attrs(Expand, Corners(9), BorderWidth(1), BorderColorVec(colors.border), BackgroundVec(background), Pad2(10, 12)), func() {
			NextAccessRole("button")
			AssignAccess()
			st := ProcessButtonEvents(false)
			if st.Hovered || st.Active {
				ModAttrs(BackgroundVec(colors.tint))
			}
			if st.FocusVisible {
				ModAttrs(BorderColorVec(CurrentColorScheme.FocusRing))
			}
			if st.Clicked {
				openPost(it.ID)
			}
			Container(Attrs(Row, Expand, Gap(10)), func() {
				rankColor := colors.muted
				if rank == 1 {
					rankColor = colors.accent
				}
				Container(Attrs(FixWidth(18), Pad2(1, 0)), func() {
					Label(fmt.Sprint(rank), FontSize(13), FontWeight(WeightBold), TextColorVec(rankColor))
				})
				Container(Attrs(Grow(1), MaxWidth(max(40, width-70)), Gap(5)), func() {
					title := it.Title
					if title == "" {
						title = "(untitled)"
					}
					Label(title, FontSize(16), FontWeight(WeightSemibold), TextColorVec(colors.text))
					if it.URL != "" {
						Label(shortURL(it.URL), FontSize(11), TextColorVec(colors.muted))
					}
					Container(Attrs(Row, Expand, CrossMid, Gap(6)), func() {
						Container(Attrs(Grow(1)), func() {
							if it.Type == "job" {
								Label("Job · "+it.By+" · "+relativeTime(it.Time), FontSize(11), TextColorVec(colors.muted))
								return
							}
							Container(Attrs(Row, Wrap, CrossMid, Gap(4)), func() {
								Label(fmt.Sprint(it.Score), FontSize(11), FontWeight(WeightBold), TextColorVec(colors.accent))
								Label("points · "+it.By+" · "+relativeTime(it.Time), FontSize(11), TextColorVec(colors.muted))
							})
						})
						if it.IsCommentable() {
							Container(Attrs(Row, CrossMid, Gap(4), Pad2(4, 6), Corners(10), BackgroundVec(colors.tint)), func() {
								Icon(SymChat, FontSize(11), TextColorVec(colors.muted))
								Label(fmt.Sprint(it.Descendants), FontSize(11), TextColorVec(colors.text))
							})
						}
					})
				})
			})
		})
	})
}

// relativeTime uses compact ages for scanning; older stories retain a date.
func relativeTime(timestamp int64) string {
	if timestamp == 0 {
		return ""
	}
	age := time.Since(time.Unix(timestamp, 0))
	switch {
	case age < time.Minute:
		return "now"
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	case age < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	default:
		return time.Unix(timestamp, 0).Local().Format("2006-01-02")
	}
}

func shortURL(u string) string {
	// strip scheme and path for a compact host display
	s := u
	for _, pfx := range []string{"https://", "http://"} {
		if len(s) > len(pfx) && s[:len(pfx)] == pfx {
			s = s[len(pfx):]
			break
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			s = s[:i]
			break
		}
	}
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}
