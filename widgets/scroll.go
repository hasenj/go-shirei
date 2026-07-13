package widgets

import (
	"log"
	"math"
	"sync/atomic"
	"time"
	"unsafe"

	. "go.hasen.dev/shirei"
)

// SCROLLBAR_WIDTH is the width in pixels of the floating scrollbar.
const SCROLLBAR_WIDTH = 16

// ScrollBarsAttrs configures ScrollBarsExt.
type ScrollBarsAttrs struct {
	Accent Vec4 // zero value: use the package-level Accent
}

// ScrollBars draws a floating scrollbar over the current container when its
// content overflows vertically. Call it inside a scrolling (Viewport) container.
func ScrollBars() {
	ScrollBarsExt(ScrollBarsAttrs{})
}

// ScrollBarsExt is ScrollBars with a per-instance accent color.
func ScrollBarsExt(attrs ScrollBarsAttrs) {
	// draws scrollbars that just float on top, to the right side of the window
	// for vertical scrolling
	rd := GetRenderData()

	// no scrollbar!
	if rd.ContentSize[1] <= rd.ResolvedSize[1] {
		Void()
		return
	}

	accent := AccentOrFallback(attrs.Accent, DefaultAccent)
	thumbBorder := Vec4{accent[0], accent[1], accent[2] - 15, accent[3]}

	const pad = 1

	// compute the height and offset of the scroll thumb
	// thumbHeight / scrollbarHeight == resolvedHeight / contentHeight
	var scrollbarHeight = rd.ResolvedSize[1] - (pad * 3)
	var thumbHeight f32
	if rd.ContentSize[1] > 0 {
		thumbHeight = scrollbarHeight * (rd.ResolvedSize[1] / rd.ContentSize[1])
	}
	thumbHeight = max(thumbHeight, 30)

	var maxScrollOffset = max(0, rd.ContentSize[1]-rd.ResolvedSize[1])
	var maxThumbOffset = max(0, scrollbarHeight-thumbHeight)
	// compute the thumb offset from the LIVE offset (this frame's input and
	// restores, applied before this call), not rd's frame-old copy — a
	// discontinuous jump (restored scroll position) would otherwise show
	// the thumb one frame behind the content. Sizes have no live
	// equivalent mid-build, and change rarely.
	var scrollOffset = max(f32(0), min(GetScrollOffset()[1], maxScrollOffset))
	// thumbOffset / maxThumbOffset = scrollOffset / maxScrollOffset
	var thumbOffset f32
	if maxScrollOffset > 0 {
		thumbOffset = maxThumbOffset * (scrollOffset / maxScrollOffset)
	}

	// DebugVar("Scroll Offset", rd.ScrollOffset)

	var scrollbarChange bool
	var offsetChangeTo Vec2

	// the scrollbar
	Container(Attrs(NoAnimate, Float(rd.ResolvedSize[0]-SCROLLBAR_WIDTH, 0), InFront, Pad(pad), FixSize(SCROLLBAR_WIDTH, f32(int(rd.ResolvedSize[1]))), Background(0, 0, 100, 1)), func() {
		// ModAttrs(YesAnimate)
		var desiredThumbOffset = thumbOffset

		if IsClicked() {
			rd := GetRenderData()
			mouse := Vec2Sub(InputState.MousePoint, rd.ResolvedOrigin)
			desiredThumbOffset = mouse[1] - (thumbHeight / 2)
			scrollbarChange = true
		}
		Element(Attrs(YesAnimate, FixHeight(f32(int(thumbOffset))))) // spacer for the thumbnail
		Container(Attrs(YesAnimate, FixHeight(f32(int(thumbHeight))), Expand, Corners(SCROLLBAR_WIDTH/2), BackgroundVec(accent),
			BorderColor(thumbBorder[0], thumbBorder[1], thumbBorder[2], thumbBorder[3]), BorderWidth(1), Center), func() {
			PressAction()
			if IsActive() {
				scrollbarChange = true
				desiredThumbOffset = thumbOffset + FrameInput.Motion[1]
			}
			Icon(TypArrowUnsorted, FontSize(12), TextColor(0, 0, 100, 0.6))
		})

		if scrollbarChange {
			// same formula used to compute the thumbOffset; clamp so dragging
			// past either end of the track cannot produce an out-of-range
			// offset (negative scroll breaks VirtualListView's render math).
			if maxThumbOffset > 0 {
				desiredScrollOffset := maxScrollOffset * (desiredThumbOffset / maxThumbOffset)
				desiredScrollOffset = max(f32(0), min(desiredScrollOffset, maxScrollOffset))
				offsetChangeTo = Vec2{0, desiredScrollOffset}
			}
		}
	})

	if scrollbarChange {
		SetScrollOffset(offsetChangeTo)
	}
}

// StringHeadersEqual reports whether a and b are the same string by identity —
// same backing pointer and length — without comparing their contents. It's a
// fast "is this literally the same string value" check for stable/interned
// strings; distinct allocations of equal content are not equal.
func StringHeadersEqual(a, b string) bool {
	return unsafe.StringData(a) == unsafe.StringData(b) && len(a) == len(b)
}

// LargeTextListKey is the VirtualListView key used by LargeText. Call
// VirtualListView_ScrollTo(LargeTextListKey, 0) when the user explicitly
// opens a different file — not when content merely finishes loading.
const LargeTextListKey = "large-text-list"

// LargeText renders a large read-only text blob in a scrolling viewport.
// Lines are addressed as offsets into the source string (not a []string of
// per-line headers), so only visible rows allocate string views. The full
// newline scan runs in the background; the first ~500 lines are available
// immediately. Switching corpora is cheap: the old []int index is dropped
// without freeing millions of string headers on the frame path.
//
// Text identity uses StringHeadersEqual: callers must keep a stable string
// (same backing pointer across frames). Scroll is preserved across the
// tip→full update; reset it yourself on explicit open via
// VirtualListView_ScrollTo(LargeTextListKey, 0).
func LargeText(text string, attrs TextAttrSet) {
	Container(Attrs(Viewport, NoAnimate), func() {
		type _LargeText struct {
			gen     atomic.Uint64 // bumped on each new text; stale scanners bail
			text    string
			starts  []int // byte offset of each line start in text
			lastEnd int   // exclusive end of last tip line; -1 → len(text)
		}

		data := Use[_LargeText]("large-text")

		if !StringHeadersEqual(data.text, text) {
			data.text = text
			gen := data.gen.Add(1)
			data.starts, data.lastEnd = scanLineStarts(text, 500)
			RequestNextFrame()
			go func(text string, gen uint64) {
				t0 := time.Now()
				starts, lastEnd := scanLineStarts(text, 0)
				log.Printf("%d lines indexed in %v", len(starts), time.Since(t0))
				WithFrameLock(func() {
					if data.gen.Load() != gen {
						return
					}
					data.starts = starts
					data.lastEnd = lastEnd
				})
				RequestNextFrame()
			}(text, gen)
		}

		var vpad = attrs.Size / 4
		n := len(data.starts)

		type LineNo int

		itemKey := func(idx int) any {
			return LineNo(idx)
		}

		itemView := func(idx int, width f32) {
			if attrs.MaxWidth == 0 {
				attrs.MaxWidth = width
			}
			line := lineAt(data.text, data.starts, data.lastEnd, idx)
			Container(Attrs(Pad2(vpad, 0), Expand), func() {
				Text(line, attrs)
			})
		}

		itemHeight := func(idx int, width f32) f32 {
			if attrs.MaxWidth == 0 {
				attrs.MaxWidth = width
			}
			shaped := ShapeText(lineAt(data.text, data.starts, data.lastEnd, idx), attrs)
			var height f32
			for _, shapedLine := range shaped.Lines {
				height += shapedLine.Height
			}
			return height + (vpad * 2)
		}

		VirtualListView(LargeTextListKey, n, itemKey, itemHeight, itemView)
	})
}

// scanLineStarts returns byte offsets of each line start in text.
// maxLines <= 0 indexes the whole string; otherwise stops after that many
// complete lines. lastEnd is the exclusive end of the final returned line
// when the scan is truncated mid-file (-1 means use len(text)).
func scanLineStarts(text string, maxLines int) (starts []int, lastEnd int) {
	lastEnd = -1
	if text == "" {
		return []int{0}, -1
	}
	capHint := 64
	if maxLines > 0 {
		capHint = maxLines
	} else if len(text) > 64 {
		capHint = len(text)/32 + 1
	}
	starts = make([]int, 0, capHint)
	starts = append(starts, 0)
	for i := 0; i < len(text); i++ {
		if text[i] != '\n' {
			continue
		}
		if maxLines > 0 && len(starts) >= maxLines {
			lastEnd = i
			return starts, lastEnd
		}
		if i+1 < len(text) {
			starts = append(starts, i+1)
		} else {
			starts = append(starts, len(text))
		}
	}
	return starts, -1
}

// lineAt returns the idx-th line as a slice of text (no trailing newline).
// lastEnd < 0 means the last line runs to len(text); otherwise it bounds the
// last tip line when the index was truncated.
func lineAt(text string, starts []int, lastEnd, idx int) string {
	if idx < 0 || idx >= len(starts) {
		return ""
	}
	lo := starts[idx]
	if lo > len(text) {
		return ""
	}
	hi := len(text)
	if idx+1 < len(starts) {
		hi = starts[idx+1]
		if hi > 0 && hi <= len(text) && text[hi-1] == '\n' {
			hi--
		}
	} else if lastEnd >= 0 {
		hi = lastEnd
	} else if hi > lo && text[hi-1] == '\n' {
		hi--
	}
	if lo > hi {
		return ""
	}
	return text[lo:hi]
}

// ZeroIfNaN returns a, or 0 when a is NaN.
func ZeroIfNaN(a f32) f32 {
	if math.IsNaN(float64(a)) {
		return 0
	} else {
		return a
	}
}

// ItemKeyFn returns a stable, unique key for the item at index (see
// VirtualListAttrs.ItemKey).
type ItemKeyFn = func(index int) any

// ItemHeightFn returns the height of the item at index for the given content
// width (see VirtualListAttrs.ItemHeight).
type ItemHeightFn = func(index int, width f32) f32

// ItemViewFn renders the item at index for the given content width (see
// VirtualListAttrs.ItemView).
type ItemViewFn = func(index int, width f32)

// VirtualListAttrs is the full configuration for VirtualListViewExt.
type VirtualListAttrs struct {
	// ItemCount is the number of items in the list.
	ItemCount int

	// ItemKey returns a stable, unique identity for the item at index — used
	// for its per-row ContainerWithKey identity and scroll/animation bookkeeping.
	ItemKey ItemKeyFn

	// ItemHeight returns the height of the item at index for the given
	// content width.
	ItemHeight ItemHeightFn

	// ItemView renders the item at index for the given content width.
	ItemView ItemViewFn

	// OutScrollOffset, if non-nil, is written at the end of this call with the
	// settled vertical scroll offset (after clamps and any ScrollTo/
	// ScrollToEnd/ScrollIntoView applied this frame). Read-only from the
	// caller's perspective — the list never reads it back.
	OutScrollOffset *f32

	// OutMaxScrollOffset, if non-nil, is written at the end of this call with
	// the settled maximum scroll offset (content height − viewport). Same
	// timing as OutScrollOffset.
	OutMaxScrollOffset *f32
}

// command wiring: one-line wrappers over shirei's PostCommand/TakeCommand
// so call sites read as widget verbs.
const vlistWidget = "widgets.VirtualList"
const vlistScrollIntoView = "scroll-into-view"

// VirtualListScrollIntoView asks the list whose key (VirtualListView's/
// VirtualListViewExt's key argument) is listKey to bring the item with itemKey
// into view on its next render — minimally: no scroll if fully visible, else
// aligned to the nearest edge. Last request wins; unconsumed requests expire
// after one frame.
func VirtualListScrollIntoView(listKey any, itemKey any) {
	PostCommand(vlistWidget, listKey, vlistScrollIntoView, itemKey)
}

func _VirtualListTakeScrollIntoView(listKey any) (any, bool) {
	if listKey == nil {
		return nil, false
	}
	return TakeCommand[any](vlistWidget, listKey, vlistScrollIntoView)
}

const vlistScrollTo = "scroll-to"

// VirtualListView_ScrollTo asks the list to set its vertical scroll offset on
// its next render — used to restore a saved position, e.g. when a tab whose
// list was hidden and rebuilt becomes visible again. offset is distance from
// the top of the content (clamped ≥ 0).
func VirtualListView_ScrollTo(listKey any, offset f32) {
	PostCommand(vlistWidget, listKey, vlistScrollTo, offset)
}

func _VirtualListTakeScrollTo(listKey any) (f32, bool) {
	if listKey == nil {
		return 0, false
	}
	return TakeCommand[f32](vlistWidget, listKey, vlistScrollTo)
}

const vlistScrollToEnd = "scroll-to-end"

// VirtualListView_ScrollToEnd asks the list to set its vertical scroll so the
// content end sits margin pixels below the bottom of the viewport (margin 0 =
// flush with the last row). The list measures a real tail rather than trusting
// the average-height TotalHeight estimate, and seeds the anchor near the end so
// large lists do not walk from a stale top anchor.
//
// Pin-to-bottom is a caller policy: capture maxScroll−scrollY as the margin,
// then re-post this command while pinned. The list stays policy-free.
func VirtualListView_ScrollToEnd(listKey any, margin f32) {
	PostCommand(vlistWidget, listKey, vlistScrollToEnd, max(f32(0), margin))
}

func _VirtualListTakeScrollToEnd(listKey any) (f32, bool) {
	if listKey == nil {
		return 0, false
	}
	return TakeCommand[f32](vlistWidget, listKey, vlistScrollToEnd)
}

// VirtualListView renders a scrolling list whose items may have different
// heights, laying out only the visible rows. key is forwarded to
// ContainerWithKey (nil = anonymous positional identity) and is the address that
// VirtualListScrollIntoView and the other command helpers post to — use a typed
// pointer to app-owned data, unique among live widgets.
func VirtualListView(key any, itemCount int, itemKeyFn ItemKeyFn, itemHeightFn ItemHeightFn, itemViewFn ItemViewFn) {
	VirtualListViewExt(key, VirtualListAttrs{
		ItemCount:  itemCount,
		ItemKey:    itemKeyFn,
		ItemHeight: itemHeightFn,
		ItemView:   itemViewFn,
	})
}

// VirtualListViewExt is VirtualListView with the full configuration surface;
// see VirtualListAttrs.
func VirtualListViewExt(key any, attrs VirtualListAttrs) {
	// the body works in terms of these locals (also captured by the closures
	// below); attrs just carries them in
	itemCount := attrs.ItemCount
	itemKeyFn := attrs.ItemKey
	itemHeightFn := attrs.ItemHeight
	itemViewFn := attrs.ItemView
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

		// a VirtualListView_ScrollTo target being driven toward: layout
		// clamps the offset against each frame's content, so a target set
		// on a freshly (re)built list (no rows yet) gets trimmed to 0. We
		// latch the target and re-apply until a frame with real content
		// has been laid out.
		restoreTo f32
		restoring bool

		// VirtualListView_ScrollToEnd latch: margin is distance from the
		// content bottom (0 = flush end). Survives the width-unknown first
		// frame and multi-frame settle while the tail is still learning.
		endMargin f32
		toEnd     bool

		// Learned content-end floor: max of the average-height estimate and
		// extents measured while scrolling / ScrollToEnd. Average-height
		// alone undershoots when lower rows are taller than the top sample
		// (continuous wheel then clamps at a FALSE BOTTOM). Invalidated
		// when width or itemCount changes.
		endFloor      f32
		endFloorCount int
		endFloorWidth f32
	}

	computeAverageHeight := func(width f32) f32 {
		var topN int = min(N, itemCount)
		if topN == 0 {
			return 1
		}
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

		if itemCount <= 0 {
			return ItemOffset{}
		}

		var result = anchor
		if result.Index < 0 {
			result = ItemOffset{}
		}
		if result.Index >= itemCount {
			result.Index = itemCount - 1
		}

		if scrollOffset < result.Offset {
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
				h := itemHeightFn(result.Index, width)
				if result.Offset+h > scrollOffset {
					break
				}
				result.Offset += h
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

	ContainerWithKey(key, Attrs(Viewport), func() {
		ScrollOnInput()

		var state = Use[VirtualListState]("virtual-list-state")

		// consume scroll commands right away — even a pass that can't
		// lay anything out yet (width unknown, below) must latch the
		// target, or a command posted at tab-switch time would sit out the
		// early-returning first frame and expire. Last-taken wins when both
		// are posted in the same frame (callers should only use one).
		if offset, ok := _VirtualListTakeScrollTo(key); ok {
			state.restoreTo = max(0, offset)
			state.restoring = true
			state.toEnd = false
		}
		if margin, ok := _VirtualListTakeScrollToEnd(key); ok {
			state.endMargin = max(0, margin)
			state.toEnd = true
			state.restoring = false
		}
		// drive toward the latched top-relative target until a frame with
		// real content has been laid out: layout clamps the offset against
		// that frame's actual content, which is the best any restore can do.
		// (Checking the offset itself can't terminate this — an unreachable
		// target would re-request frames forever.) ScrollToEnd is applied
		// after width is known (needs a real tail measure).
		if state.restoring {
			SetScrollOffset(Vec2{0, state.restoreTo})
			if GetRenderData().ContentSize[1] > 0 || itemCount == 0 || state.restoreTo == 0 {
				state.restoring = false
			} else {
				RequestNextFrame()
			}
		}
		if state.toEnd {
			if itemCount == 0 {
				state.toEnd = false
			} else if GetRenderData().ContentSize[1] == 0 {
				// keep the latch alive across the empty / width-unknown first frames
				RequestNextFrame()
			}
		}

		// after the restore, so the thumb draws from this frame's offset
		ScrollBars()

		var widthChanged bool

		scroll := GetScrollOffset()
		size := GetResolvedSize()

		width := max(0, size[0]-SCROLLBAR_WIDTH)
		if width <= 0 {
			// we can't do anything until width is known
			if attrs.OutScrollOffset != nil {
				*attrs.OutScrollOffset = GetScrollOffset()[1]
			}
			if attrs.OutMaxScrollOffset != nil {
				*attrs.OutMaxScrollOffset = 0
			}
			RequestNextFrame()
			return
		}

		// compute average height
		avgHeight := computeAverageHeight(width)

		var totalHeight0 = state.TotalHeight
		state.TotalHeight = avgHeight * f32(itemCount)
		// Keep extents learned while scrolling (or by ScrollToEnd) when the
		// corpus geometry is unchanged; pure estimate would clamp scroll
		// short of a tall tail every frame.
		if state.endFloorCount != itemCount || state.endFloorWidth != width {
			state.endFloor = 0
			state.endFloorCount = itemCount
			state.endFloorWidth = width
		}
		if state.endFloor > state.TotalHeight {
			state.TotalHeight = state.endFloor
		}

		// Content can shrink while a scroll position from taller content is
		// still in effect — one list instance reused for a smaller data set, or
		// a live list losing rows. A stale anchor or offset would then index
		// past the current items and panic in itemOffsetFromAnchor. Clamp both
		// back into range before any height is read. Also reject negative
		// offsets (e.g. thumb dragged past the top before ScrollBars clamped):
		// renderedHeight = -(scroll - spaceBefore) would otherwise start huge
		// and the visible-row loop would stop after one item.
		maxScroll := max(0, state.TotalHeight-size[1])
		if scroll[1] < 0 || scroll[1] > maxScroll {
			scroll[1] = max(f32(0), min(scroll[1], maxScroll))
			SetScrollOffset(Vec2{0, scroll[1]})
		}
		if state.ScrollOffset < 0 || state.ScrollOffset > maxScroll {
			state.ScrollOffset = max(f32(0), min(state.ScrollOffset, maxScroll))
		}
		if state.Anchor.Index >= itemCount {
			state.Anchor = ItemOffset{}
		}

		var scrollOffset0 = state.ScrollOffset

		if width != state.Width {
			widthChanged = true
			state.Width = width
		}

		// consume a scroll-into-view command, if one is addressed at us
		// (an item that isn't in the list — filtered out, gone — is a no-op)
		if revealId, ok := _VirtualListTakeScrollIntoView(key); ok {
			targetIndex := -1
			for i := 0; i < itemCount; i++ {
				if itemKeyFn(i) == revealId {
					targetIndex = i
					break
				}
			}
			if targetIndex >= 0 {
				// walk from the anchor — the widget's own best truth for
				// absolute offsets (estimates self-correct via re-anchoring)
				top := state.Anchor.Offset
				for i := state.Anchor.Index; i < targetIndex; i++ {
					top += itemHeightFn(i, width)
				}
				for i := state.Anchor.Index; i > targetIndex; i-- {
					top -= itemHeightFn(i-1, width)
				}
				height := itemHeightFn(targetIndex, width)

				target := scroll[1]
				if top < scroll[1] {
					target = top // above the viewport: align its top edge
				} else if top+height > scroll[1]+size[1] {
					target = top + height - size[1] // below: align bottom edge
				}
				if target != scroll[1] {
					SetScrollOffset(Vec2{0, max(0, target)})
					RequestNextFrame()
					scroll = GetScrollOffset()
				}
			}
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

		// totalHeight0 == 0 means there was no previous content to scale from —
		// a freshly (re)built list. Rescaling then divides by zero (→ NaN → 0)
		// and would wipe the offset, clobbering a just-restored scroll position;
		// leave the offset untouched instead.
		if widthChanged && totalHeight0 > 0 {
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

		// ScrollToEnd: measure a real tail and seed the anchor at the last
		// item *before* picking the visible window. Walking forward from a
		// stale top anchor under-reports contentEnd when heights vary, so
		// maxScroll would stay short of the last lines.
		//
		// Target scroll and the last-item anchor MUST share the same
		// TotalHeight coordinate system. Using contentEnd for scroll but
		// TotalHeight for the anchor (when estimate > measured end) parks
		// the last rows below the viewport while still reporting fromBottom=0.
		if state.toEnd {
			if itemCount <= 0 {
				state.toEnd = false
				state.endFloor = 0
				SetScrollOffset(Vec2{})
				state.ScrollOffset = 0
			} else {
				tailStart := 0
				if itemCount > N*2 {
					tailStart = itemCount - N*2
				}
				var tailH f32
				for i := tailStart; i < itemCount; i++ {
					tailH += max(1, itemHeightFn(i, width))
				}
				contentEnd := tailH
				if tailStart > 0 {
					contentEnd = avgHeight*f32(tailStart) + tailH
				}
				if contentEnd > state.TotalHeight {
					state.TotalHeight = contentEnd
				}
				state.endFloor = state.TotalHeight
				state.endFloorCount = itemCount
				state.endFloorWidth = width
				lastH := max(1, itemHeightFn(itemCount-1, width))
				state.Anchor = ItemOffset{Index: itemCount - 1, Offset: state.TotalHeight - lastH}
				target := max(f32(0), state.TotalHeight-size[1]-state.endMargin)
				SetScrollOffset(Vec2{0, target})
				state.ScrollOffset = target
			}
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

		Element(Attrs(FixHeight(spaceBefore)))

		// account for the unseeen portions of the first item (pixels above the fold)
		var renderedHeight = -(state.ScrollOffset - spaceBefore)
		var sumHeights f32

		var startIndex int = first.Index
		var endIndex int = itemCount // exclusive

		// find endIndex such that all items are in view
		for idx := startIndex; idx < itemCount; idx++ {
			endIndex = idx + 1
			height := itemHeightFn(idx, width)
			renderedHeight += height
			sumHeights += height

			var id = itemKeyFn(idx)
			ContainerWithKey(id, Attrs(FixSize(width, height)), func() {
				itemViewFn(idx, width)
			})

			if renderedHeight > size[1] {
				break
			}
		}

		// Real content extent through the last rendered row (coordinate
		// system of the re-anchored walk). spaceBefore+renderedHeight is
		// wrong here: renderedHeight includes the partial-first-item scroll
		// adjustment and must not drive TotalHeight/spaceAfter.
		measuredThrough := spaceBefore + sumHeights
		var spaceAfter f32

		// Learn TotalHeight from what the walk actually measured. Continuous
		// wheel re-anchors with real row heights; when those exceed the
		// top-N average estimate, maxScroll must grow or the list clamps at
		// a false bottom with more rows still unrendered.
		if endIndex >= itemCount {
			// Exact end: snap to measured (also corrects overestimate slack).
			state.TotalHeight = measuredThrough
			spaceAfter = 0
		} else {
			remaining := itemCount - endIndex
			contentEnd := measuredThrough + f32(remaining)*avgHeight
			// Near the reported end, or when the walk already overran the
			// estimate, measure the real tail so maxScroll can catch up.
			nearReportedEnd := measuredThrough+size[1] >= state.TotalHeight-avgHeight
			if remaining <= N*2 || nearReportedEnd || measuredThrough > state.TotalHeight {
				var rest f32
				for i := endIndex; i < itemCount; i++ {
					rest += max(1, itemHeightFn(i, width))
				}
				contentEnd = measuredThrough + rest
			}
			if contentEnd > state.TotalHeight {
				state.TotalHeight = contentEnd
			}
			spaceAfter = max(0, state.TotalHeight-measuredThrough)
			if spaceAfter < avgHeight {
				spaceAfter = f32(remaining) * avgHeight
				if measuredThrough+spaceAfter > state.TotalHeight {
					state.TotalHeight = measuredThrough + spaceAfter
				}
			}
		}

		// Persist learned end for the next frame (estimate alone would wipe it).
		state.endFloor = state.TotalHeight
		state.endFloorCount = itemCount
		state.endFloorWidth = width

		// ScrollToEnd settle: if we aimed near the bottom but still did not
		// render the last row, the tail measure was short — extend TotalHeight
		// from what we walked and re-apply next frame. Large margins (pin far
		// above the end) do not require the last row and clear after one apply.
		if state.toEnd {
			nearEnd := state.endMargin < size[1]
			if nearEnd && endIndex < itemCount {
				contentEnd := measuredThrough
				for i := endIndex; i < itemCount; i++ {
					contentEnd += max(1, itemHeightFn(i, width))
				}
				if contentEnd > state.TotalHeight {
					state.TotalHeight = contentEnd
					spaceAfter = max(0, state.TotalHeight-measuredThrough)
				}
				state.endFloor = state.TotalHeight
				state.endFloorCount = itemCount
				state.endFloorWidth = width
				lastH := max(1, itemHeightFn(itemCount-1, width))
				state.Anchor = ItemOffset{Index: itemCount - 1, Offset: state.TotalHeight - lastH}
				target := max(f32(0), state.TotalHeight-size[1]-state.endMargin)
				SetScrollOffset(Vec2{0, target})
				state.ScrollOffset = target
				RequestNextFrame()
			} else {
				state.toEnd = false
			}
		}

		Element(Attrs(FixHeight(spaceAfter)))

		// Settled readbacks — after every SetScrollOffset above. While a
		// restore is still settling (first frames of a rebuilt list, before
		// layout has real content to clamp against), report the target — a
		// mid-restore offset would round-trip a wrong value back to the caller.
		scrollOut := GetScrollOffset()[1]
		if state.restoring {
			scrollOut = state.restoreTo
		}
		maxOut := max(0, state.TotalHeight-size[1])
		if attrs.OutScrollOffset != nil {
			*attrs.OutScrollOffset = scrollOut
		}
		if attrs.OutMaxScrollOffset != nil {
			*attrs.OutMaxScrollOffset = maxOut
		}
	})
}
