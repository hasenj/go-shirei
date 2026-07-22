package main

import (
	"fmt"
	"math"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const moreRowHeight f32 = 48

// hnOrange is the classic YC/HN accent (#ff6600) as HSLA.
var hnOrange = Vec4{24, 100, 50, 1}

// Shared title-bar chrome so feed and thread keep the same pad and pin the
// refresh control to the same distance from the right edge.
const (
	headerBarPadV f32 = 8
	headerBarPadH f32 = 10
	headerBarGap  f32 = 8
)

// headerButtonHeight matches a default primary Button face: roughly 2×
// design text size after ComfortScale (see widgets.AccentButton padding).
func headerButtonHeight() f32 {
	return 2 * 12 * GetHost().ComfortScale
}

// headerTitleRow is the shared top bar: left chrome | fillers + title | right chrome.
// Fillers center the title between the side controls without fixed side widths
// (which overflowed on a phone when the title was long).
func headerTitleRow(title string, titleSize f32, left, right func()) {
	Container(Attrs(Row, CrossMid, Expand, Gap(headerBarGap), Pad2(headerBarPadV, headerBarPadH)), func() {
		if left != nil {
			left()
		}
		Filler(1)
		Label(title, FontSize(titleSize), FontWeight(WeightBold), TextColorVec(hnOrange))
		Filler(1)
		if right != nil {
			right()
		}
	})
}

func feedScreen() {
	appData.mu.Lock()
	feed := appData.feed
	stories := append([]*Item(nil), appData.stories...)
	loading := appData.feedLoading
	more := appData.feedMore
	err := appData.feedErr
	totalIDs := len(appData.storyIDs)
	appData.mu.Unlock()

	// Header chrome: padded title row, then full-bleed feed segments.
	Container(Attrs(Background(220, 14, 94, 1), Expand), func() {
		iconSz := headerButtonHeight()
		headerTitleRow("Hacker News Reader", 20, func() {
			if appIcon != nil {
				ImageView(UseImage("hn-app-icon", appIcon), Vec2{iconSz, iconSz})
			}
		}, func() {
			if ButtonExt("", ButtonAttrs{Icon: SymRefresh, Accent: hnOrange, Disabled: loading}) {
				go loadFeed(true, false)
			}
		})

		// Edge-to-edge segment bar (no side pad on this row).
		feedSeg := DefaultSegmentedControlAttrs()
		feedSeg.CellPadH = 0
		feedSeg.FrameCorners = 0
		feedSeg.Expand = true
		feedSeg.Accent = hnOrange
		if SegmentedControlExt(&feed, feedSeg,
			Cell("Front", FeedFront),
			Cell("New", FeedNew),
			Cell("Show", FeedShow),
			Cell("Ask", FeedAsk),
			Cell("Jobs", FeedJobs),
		) {
			appData.mu.Lock()
			appData.feed = feed
			appData.mu.Unlock()
			go loadFeed(true, true)
		}
	})

	// Separator when idle; indeterminate orange sweep while the feed is busy.
	feedActivityStrip(loading || more)

	Container(Attrs(Grow(1), Expand, Clip), func() {
		if loading && len(stories) == 0 {
			// Centered empty-state copy; activity strip above still runs.
			Container(Attrs(Expand, Grow(1), Center, Gap(8)), func() {
				Label("Loading "+feed.Label()+"…",
					FontSize(22), FontWeight(WeightBold), TextColor(0, 0, 48, 1))
			})
			return
		}
		if err != "" && len(stories) == 0 {
			Container(Attrs(Pad(16), Gap(10), Expand), func() {
				Label("Failed to load: "+err, FontSize(15), TextColor(10, 70, 40, 1))
				if Button(0, "Retry") {
					go loadFeed(true, true)
				}
			})
			return
		}

		// Virtual list: stories + optional "More" row.
		hasMore := totalIDs == 0 || len(stories) < totalIDs
		// When we have not finished the first id fetch, still allow More once
		// we have a page (totalIDs > 0). If totalIDs == 0 and not loading, no more.
		if totalIDs == 0 {
			hasMore = false
		}
		n := len(stories)
		if hasMore || more {
			n++ // trailing More row
		}

		itemKey := func(i int) any {
			if i < len(stories) {
				return stories[i].ID
			}
			return "more"
		}
		itemView := func(i int, width f32) {
			if i < len(stories) {
				storyRow(stories[i], width)
				return
			}
			// More
			Container(Attrs(Expand, FixHeight(moreRowHeight), Center, Pad(8)), func() {
				if more {
					Label("Loading more…", FontSize(15), TextColor(0, 0, 50, 1))
					return
				}
				if Button(0, "More") {
					go loadFeed(false, false)
				}
			})
		}

		// nil ItemHeight → VirtualList Measures itemView under the row width.
		VirtualListView(&appData.feed, n, itemKey, nil, itemView)
	})
}

// feedActivityStrip sits between the segment bar and the list. Idle: a light
// hairline. Busy: a short orange segment that loops left→right (indeterminate).
func feedActivityStrip(active bool) {
	const trackH f32 = 2.5
	if !active {
		Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, 0.08)))
		return
	}
	RequestNextFrame()
	Container(Attrs(Expand, FixHeight(trackH), Background(0, 0, 0, 0.06), Clip, NoAnimate), func() {
		w := GetResolvedSize()[0]
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
			BackgroundVec(hnOrange),
		))
	})
}

func storyRow(it *Item, width f32) {
	if it == nil {
		return
	}
	// No FixHeight: VirtualList owns the slot; Measure uses the same tree.
	ContainerWithKey(it.ID, Attrs(Expand, MaxWidth(width), Clip, Pad2(10, 12), Gap(4),
		Background(0, 0, 100, 1), BorderColor(0, 0, 90, 1), BorderWidth(0.5)), func() {
		if PressAction() {
			openPost(it.ID)
		}
		if pressedFeedback() {
			ModAttrs(Background(220, 20, 98, 1))
		}

		title := it.Title
		if title == "" {
			title = "(untitled)"
		}
		Label(title, FontSize(16), FontWeight(WeightSemibold), TextColor(220, 25, 18, 1))

		meta := fmt.Sprintf("%d pts · %s · %s", it.Score, it.By, it.Timestamp())
		if it.By == "" {
			meta = fmt.Sprintf("%d pts · %s", it.Score, it.Timestamp())
		}
		if it.Type == "job" {
			meta = fmt.Sprintf("%s · %s", it.By, it.Timestamp())
		}
		Label(meta, FontSize(13), TextColor(0, 0, 45, 1))

		var sub string
		if it.Type == "job" {
			sub = "job"
		} else {
			sub = fmt.Sprintf("%d comments", it.Descendants)
		}
		if it.URL != "" {
			// Host-ish hint without parsing carefully.
			sub = sub + " · " + shortURL(it.URL)
		}
		Label(sub, FontSize(13), TextColor(0, 0, 50, 1))
	})
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
