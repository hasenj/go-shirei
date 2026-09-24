package main

import (
	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

// feedTabs divides the full strip evenly between feeds. The active feed has
// an inset orange marker with rounded top corners.
func feedTabs(selected Feed) Feed {
	colors := readerColors()
	next := selected
	Container(Attrs(Row, Expand, FixHeight(40), BackgroundVec(colors.page)), func() {
		for _, feed := range []Feed{FeedFront, FeedNew, FeedShow, FeedAsk, FeedJobs} {
			NextAccessName("feed_tab")
			NextAccessValue(feed.Label())
			NextAccessLabel(feed.Label())
			ContainerWithKey(feed, Attrs(Grow(1), Extrinsic, Expand), func() {
				NextAccessRole("tab")
				NextAccessChecked(feed == selected)
				AssignAccess()
				st := ProcessButtonEvents(false)
				if st.Clicked {
					next = feed
				}
				if st.Hovered || st.Active {
					ModAttrs(BackgroundVec(colors.tint))
				}
				if st.FocusVisible {
					ModAttrs(BorderWidth(1), BorderColorVec(colors.accent), Corners(4))
				}
				text, weight := colors.text, WeightNormal
				if feed == selected {
					text, weight = colors.accent, WeightBold
				}
				Container(Attrs(Grow(1), Expand, Center), func() {
					Label(feed.Label(), FontSize(14), FontWeight(weight), TextColorVec(text))
				})
				Container(Attrs(Expand, FixHeight(4), Pad2(0, 10)), func() {
					if feed == selected {
						Element(Attrs(Expand, FixHeight(4), Corners4(3, 3, 0, 0), BackgroundVec(colors.accent)))
					}
				})
			})
		}
	})
	return next
}
