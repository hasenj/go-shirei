package widgets

import (
	"log"
	"math"
	"strings"
	"sync"
	"time"
	"unsafe"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
)

const SCROLLBAR_WIDTH = 20

func ScrollBars() {
	// draws scrollbars that just float on top, to the right side of the window
	// for vertical scrolling
	rd := GetRenderData()

	// no scrollbar!
	if rd.ContentSize[1] <= rd.ResolvedSize[1] {
		Void()
		return
	}

	const pad = 3

	// compute the height and offset of the scroll thumb
	// thumbHeight / scrollbarHeight == resolvedHeight / contentHeight
	var scrollbarHeight = rd.ResolvedSize[1] - (pad * 3)
	var thumbHeight f32
	if rd.ContentSize[1] > 0 {
		thumbHeight = scrollbarHeight * (rd.ResolvedSize[1] / rd.ContentSize[1])
	}
	thumbHeight = max(thumbHeight, 20)

	var maxScrollOffset = max(0, rd.ContentSize[1]-rd.ResolvedSize[1])
	var maxThumbOffset = max(0, scrollbarHeight-thumbHeight)
	// compute the thumb offset
	// thumbOffset / maxThumbOffset = scrollOffset / maxScrollOffset
	var thumbOffset f32
	if maxScrollOffset > 0 {
		thumbOffset = maxThumbOffset * (rd.ScrollOffset[1] / maxScrollOffset)
	}

	// DebugVar("Scroll Offset", rd.ScrollOffset)

	var scrollbarChange bool
	var offsetChangeTo Vec2

	// the scrollbar
	Layout(TW(NoAnimate, Float(rd.ResolvedSize[0]-SCROLLBAR_WIDTH, 0), InFront, Pad(pad), FixSize(SCROLLBAR_WIDTH, f32(int(rd.ResolvedSize[1]))), BG(0, 0, 50, 0.5)), func() {
		// ModAttrs(YesAnimate)
		var desiredThumbOffset = thumbOffset

		if IsClicked() {
			rd := GetRenderData()
			mouse := Vec2Sub(InputState.MousePoint, rd.ResolvedOrigin)
			desiredThumbOffset = mouse[1] - (thumbHeight / 2)
			scrollbarChange = true
		}
		Element(TW(YesAnimate, FixHeight(f32(int(thumbOffset))))) // spacer for the thumbnail
		Layout(TW(YesAnimate, FixHeight(f32(int(thumbHeight))), Expand, BR(6), BG(0, 0, 40, 1)), func() {
			PressAction()
			if IsActive() {
				scrollbarChange = true
				desiredThumbOffset = thumbOffset + FrameInput.Motion[1]
			}
		})

		if scrollbarChange {
			// same formula used to compute the thumbOffset
			if maxThumbOffset > 0 {
				desiredScrollOffset := maxScrollOffset * (desiredThumbOffset / maxThumbOffset)
				offsetChangeTo = Vec2{0, desiredScrollOffset}
			}
		}
	})

	if scrollbarChange {
		SetScrollOffset(offsetChangeTo)
	}
}

func StringHeadersEqual(a, b string) bool {
	return unsafe.StringData(a) == unsafe.StringData(b) && len(a) == len(b)
}

func LargeText(text string, attrs TextAttrs) {
	Layout(TW(Viewport, NoAnimate), func() {
		type _LargeText struct {
			busy sync.WaitGroup // already processing, don't process again until the previous one is done!

			text string // input!

			processing bool // todo need a way to track processing progress (not possible currently)
			lines      []string
		}

		data := Use[_LargeText]("large-text")

		if !StringHeadersEqual(data.text, text) {
			data.text = text
			data.processing = true
			// pre-process the tip of the file to remove the visual waiting
			data.lines = strings.SplitN(text, "\n", 500)
			// drop the last item since it's the full text!!
			if len(data.lines) > 1 {
				data.lines = data.lines[:len(data.lines)-1]
			}
			RequestNextFrame()
			go func() {
				// to handle the case where we are already processing something!!
				data.busy.Wait()

				data.busy.Add(1)
				defer data.busy.Done()

				t0 := time.Now()
				lines := strings.Split(text, "\n")
				log.Printf("%d lines split in %v", len(lines), time.Since(t0))
				WithFrameLock(func() {
					data.processing = false
					data.lines = lines
				})
				RequestNextFrame()
			}()
		}

		var vpad = attrs.Size / 4

		type LineNo int

		itemId := func(idx int) any {
			return LineNo(idx)
		}

		itemView := func(idx int, width f32) {
			if attrs.MaxWidth == 0 {
				attrs.MaxWidth = width
			}
			line := data.lines[idx]
			Layout(TW(Pad2(vpad, 0), Expand), func() {
				Text(line, attrs)
			})
		}

		itemHeight := func(idx int, width f32) f32 {
			if attrs.MaxWidth == 0 {
				attrs.MaxWidth = width
			}
			textLine := data.lines[idx]
			shaped := ShapeText(textLine, attrs)
			var height f32
			for _, shapedLine := range shaped.Lines {
				height += shapedLine.Height
			}
			return height + (vpad * 2)
		}

		VirtualListView(len(data.lines), itemId, itemHeight, itemView)
	})
}

func ZeroIfNaN(a f32) f32 {
	if math.IsNaN(float64(a)) {
		return 0
	} else {
		return a
	}
}

type ItemHeightFn = func(index int, width f32) f32
type ItemViewFn = func(index int, width f32)

// VirtualListView is virtual list view where items have different heights!
func VirtualListView(itemCount int, itemIdFn func(int) any, itemHeightFn ItemHeightFn, itemViewFn ItemViewFn) {
	/*

		Requirements and constraints:

		- Smooth scrolling must be smooth
		- Random access must be possible (e.g. to the middle of the screen!)
		- Scrolling near the bottom or top must look normal
		- Scrollbar thumbsize must not change radically as you scroll up and down
		- Changing width must not cause a visual scrolling of items (stablize scroll position)

		Strategy

		- When smooth scrolling, scroll relative to a known anchor
		- Keep updating the anchor to be the first item in view
		- When random scrolling, use heuristic based on average height
		- Use the top N elements to compute average height
	*/

	const N = 50

	type ItemOffset struct {
		Index  int
		Offset f32
	}

	type VirtualListState struct {
		// the anchor is an invariant that is to be maintained in order to
		// preserve the appearance of consistent smooth continuous scrolling
		Anchor ItemOffset

		// state used to handle width resizing
		TotalHeight f32

		// known view state; used to detect changes
		ScrollOffset f32
		Width        f32
	}

	computeAverageHeight := func(width f32) f32 {
		var topN int = min(N, itemCount)
		var seenHeight f32
		for i := range topN {
			seenHeight += max(1, itemHeightFn(i, width))
		}
		return seenHeight / f32(topN)
	}

	itemOffsetFromAnchor := func(width f32, anchor ItemOffset, scrollOffset f32) ItemOffset {
		/*
			The purpose of this computation is to support smooth scrolling
			relative to an anchor

			Given an anchor defined by (index, offset), we want to find the
			(index, offset) of the first item in the visible window, given the
			scroll offset

			We iterate from the anchor offset upward or downward until we find
			the item where:

				space_before < scroll_offset && space_before + height > scroll_offset

			----- space before ------------ ┌────────────────┐
			----- scroll offset ----------- │     index      │  height
			                                └────────────────┘
			                                        •
			                                        •
			                                        •
			----- anchor offset ----------- ┌────────────────┐
			                                │  anchor_index  │
			                                └────────────────┘
		*/

		// with some help from ChatGPT

		var result = anchor

		if scrollOffset < anchor.Offset {
			// scrolling up
			for result.Index > 0 {
				result.Index--
				result.Offset -= itemHeightFn(result.Index, width)
				if result.Offset <= scrollOffset {
					break
				}
			}
		} else {
			// scrolling down
			for result.Index < itemCount-1 {
				if result.Offset+itemHeightFn(result.Index, width) > scrollOffset {
					break
				}
				result.Offset += itemHeightFn(result.Index, width)
				result.Index++
			}
		}

		return result
	}

	// for handling random-access scrolling!
	anchorFromOffset := func(width f32, avgHeight f32, scrollOffset f32) ItemOffset {
		// Special case when number of items is less than N*2
		if itemCount <= N*2 {
			return itemOffsetFromAnchor(width, ItemOffset{}, scrollOffset)
		}

		// round to nearest multiple of assumedHeight
		var anchor ItemOffset
		anchor.Offset = f32(int(scrollOffset/avgHeight)) * avgHeight
		anchor.Index = int(ZeroIfNaN(anchor.Offset / avgHeight))

		// Special handling for items near the edges
		if anchor.Index <= N {
			return itemOffsetFromAnchor(width, ItemOffset{}, scrollOffset)
		} else if anchor.Index >= itemCount-N {
			// no need to call countTotalHeight because we know itemCount is not
			// smaller than N*2
			var totalHeight = avgHeight * f32(itemCount)
			var offset = totalHeight
			for i := itemCount - 1; i >= anchor.Index; i-- {
				offset -= itemHeightFn(i, width)
			}
			anchor.Offset = offset
			return anchor
		} else {
			return anchor
		}
	}

	Layout(TW(Viewport, NoAnimate), func() {
		ScrollOnInput()
		ScrollBars()

		var widthChanged bool

		var state = Use[VirtualListState]("virtual-list-state")

		scroll := GetScrollOffset()
		size := GetResolvedSize()

		width := max(0, size[0]-SCROLLBAR_WIDTH)
		if width <= 0 {
			// we can't do anything until width is known
			RequestNextFrame()
			return
		}

		// compute average height
		avgHeight := computeAverageHeight(width)

		var totalHeight0 = state.TotalHeight
		state.TotalHeight = avgHeight * f32(itemCount)

		var scrollOffset0 = state.ScrollOffset

		if width != state.Width {
			widthChanged = true
			state.Width = width
		}

		// a scrolling has happened
		// we need to figure out if we need to re-anchor or not
		if scroll[1] != state.ScrollOffset {
			scrollAmount := Absf32(state.ScrollOffset - scroll[1])
			state.ScrollOffset = scroll[1]

			var jumpThreshold = size[1] * 2

			// TODO/FIXME: keep track of the seen range from continuous scrolling
			// and only re-anchor if we go outside of that range
			if scrollAmount > jumpThreshold {
				// re-anchor
				state.Anchor = anchorFromOffset(width, avgHeight, state.ScrollOffset)
			}
		}

		if widthChanged {
			/*
				when width changes, heights change, and the anchor offset is now
				wrong! we would like the scroll position to remain stable
				visually AND for the scroll position on the scrollbar to also
				remain stable

					offset0 / height0 = offset / height
					offset = height * offset0 / height0

				Ideally we want to apply this to the first item on the screen,
				but we don't keep that in our state, and the anchor is usually
				set to the first item anyway, so this should be good enough.
			*/
			state.Anchor.Offset = ZeroIfNaN(state.TotalHeight * state.Anchor.Offset / totalHeight0)
			state.ScrollOffset = ZeroIfNaN(state.TotalHeight * scrollOffset0 / totalHeight0)
			SetScrollOffset(Vec2{0, state.ScrollOffset})
		}

		first := itemOffsetFromAnchor(width, state.Anchor, state.ScrollOffset)

		// edge case 1 (top)
		if first.Index == 0 {
			first.Offset = 0
		}
		if first.Offset < avgHeight && first.Index != 0 {
			first = itemOffsetFromAnchor(width, ItemOffset{}, state.ScrollOffset)
		}

		state.Anchor = first // always be re-anchoring!!

		spaceBefore := first.Offset

		Element(TW(FixHeight(spaceBefore)))

		// account for the unseeen portions of the first item (pixels above the fold)
		var renderedHeight = -(state.ScrollOffset - spaceBefore)

		var startIndex int = first.Index
		var endIndex int = itemCount // exclusive

		// find endIndex such that all items are in view
		for idx := startIndex; idx < itemCount; idx++ {
			endIndex = idx + 1
			height := itemHeightFn(idx, width)
			renderedHeight += height

			var id = itemIdFn(idx)
			LayoutId(id, TW(FixSize(width, height)), func() {
				itemViewFn(idx, width)
			})

			if renderedHeight > size[1] {
				break
			}
		}

		spaceAfter := max(0, state.TotalHeight-(spaceBefore+renderedHeight))

		// edge case 2 (bottom)
		if endIndex == itemCount {
			spaceAfter = 0
		}
		if endIndex != itemCount && spaceAfter < avgHeight {
			remainingCount := itemCount - endIndex
			spaceAfter = f32(remainingCount) * avgHeight
		}

		Element(TW(FixHeight(spaceAfter)))
	})
}
