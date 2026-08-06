package widgets

import (
	"log"
	"math"
	"sync/atomic"
	"time"
	"unsafe"

	. "go.hasen.dev/shirei"
)

// SCROLLBAR_WIDTH is the default track width of the floating vertical
// scrollbar (also the gutter VirtualList reserves for content width).
// The modern default paints a thinner pill inside this hit area.
const SCROLLBAR_WIDTH = 12

// defaultThumbMinHeight is the shortest thumb default bars will shrink to.
const defaultThumbMinHeight float32 = 24

// defaultTrackPad is the default ScrollBarAttrs.TrackPad (zero means this).
const defaultTrackPad float32 = 2

// ScrollBarState is a snapshot of the current container's scroll geometry and
// activity. Call GetScrollingState inside the scrollable container, typically
// right after ScrollOnInput (same timing as ScrollBarExt / default ScrollBars).
//
// Sizes (Viewport, Content, thumb metrics) come from the last committed layout
// of this container — content built later this frame is not visible yet. Offset
// and Wheel are live for this frame.
type ScrollBarState struct {
	// Needed is true when content overflows the viewport on the vertical axis
	// (the only axis default bars handle today).
	Needed bool

	// Offset is the live scroll offset (after ScrollOnInput / SetScrollOffset
	// so far this frame).
	Offset Vec2
	// MaxOffset is max(0, Content - Viewport) per axis from last layout.
	MaxOffset Vec2

	// Viewport and Content sizes from last layout (see package comment timing).
	Viewport Vec2
	Content  Vec2

	// Wheel is this frame's FrameInput.Scroll when this container is hovered
	// (same gating as ScrollOnInput); zero otherwise so other scrollables
	// do not see global wheel traffic.
	Wheel Vec2
	// OffsetDelta is Offset - previous frame's Offset (captures wheel on this
	// box, thumb drag, track jump, and programmatic SetScrollOffset).
	OffsetDelta Vec2
	// Hovered is IsHovered() on the scrollable container.
	Hovered bool

	// LastScrollTime is the last time scroll activity was observed on THIS
	// container (local wheel, local offset change, or its bar interaction).
	// Zero if never.
	LastScrollTime time.Time
	// IdleFor is now - LastScrollTime (0 if never scrolled).
	IdleFor time.Duration
	// ActiveThisFrame is true if this container's Wheel or OffsetDelta is
	// non-zero this frame (not other containers' scrolling).
	ActiveThisFrame bool
	// RecentlyActive is true if IdleFor < ScrollRecentWindow (default 400ms).
	// Useful for auto-hide chrome without one-frame flicker.
	RecentlyActive bool

	// Vertical thumb metrics in track coordinates (padding already removed
	// from TrackLength). Valid when Needed.
	TrackLength    float32
	ThumbLength    float32
	ThumbOffset    float32 // along the track, 0 = top
	ThumbMinLength float32 // min length used for ThumbLength
}

// ScrollRecentWindow is how long RecentlyActive stays true after activity.
// Customizers can ignore it and use IdleFor with their own threshold.
var ScrollRecentWindow = 400 * time.Millisecond

// scrollActivity is per-container hook state for GetScrollingState.
type scrollActivity struct {
	prevOffset Vec2
	lastActive time.Time
	frame      int64
	cached     ScrollBarState
	havePrev   bool
	haveCache  bool
}

// GetScrollingState returns scroll geometry and activity for the current
// container. Safe to call more than once per frame (cached). Call after
// ScrollOnInput when the wheel should count toward Offset/OffsetDelta.
//
// Wheel and ActiveThisFrame are scoped to THIS container (hovered for wheel),
// so scrolling one pane does not mark sibling panes as active.
func GetScrollingState() ScrollBarState {
	h := Use[scrollActivity]("scroll-activity")
	fn := GetFrameNumber()
	if h.haveCache && h.frame == fn {
		return h.cached
	}

	rd := GetRenderData()
	live := GetScrollOffset()
	hovered := IsHovered()

	// Wheel only counts on the hovered scrollable — same idea as ScrollOnInput.
	var wheel Vec2
	if hovered {
		wheel = GetFrameInput().Scroll
	}

	var offsetDelta Vec2
	if h.havePrev {
		offsetDelta = Vec2Sub(live, h.prevOffset)
	}

	active := wheel != (Vec2{}) || offsetDelta != (Vec2{})
	if active {
		h.lastActive = time.Now()
	}

	viewport := rd.ResolvedSize
	content := rd.ContentSize
	maxOff := Vec2{
		max(float32(0), content[0]-viewport[0]),
		max(float32(0), content[1]-viewport[1]),
	}
	// Live offset may exceed max until layout reclamps; report clamped for metrics.
	offY := max(float32(0), min(live[1], maxOff[1]))
	offX := max(float32(0), min(live[0], maxOff[0]))

	trackPad := defaultTrackPad // matches default ScrollBarAttrs.TrackPad
	trackLen := viewport[1] - trackPad*3
	if trackLen < 1 {
		trackLen = max(float32(0), viewport[1])
	}
	thumbMin := defaultThumbMinHeight
	thumbLen := thumbMin
	if content[1] > 0 && trackLen > 0 {
		thumbLen = trackLen * (viewport[1] / content[1])
	}
	thumbLen = max(thumbMin, thumbLen)
	if thumbLen > trackLen && trackLen > 0 {
		thumbLen = trackLen
	}
	maxThumb := max(float32(0), trackLen-thumbLen)
	var thumbOff float32
	if maxOff[1] > 0 && maxThumb > 0 {
		thumbOff = maxThumb * (offY / maxOff[1])
	}

	var idle time.Duration
	recent := false
	if !h.lastActive.IsZero() {
		idle = time.Since(h.lastActive)
		recent = idle < ScrollRecentWindow
	}

	st := ScrollBarState{
		Needed:          content[1] > viewport[1] && viewport[1] > 0,
		Offset:          Vec2{offX, offY},
		MaxOffset:       maxOff,
		Viewport:        viewport,
		Content:         content,
		Wheel:           wheel,
		OffsetDelta:     offsetDelta,
		Hovered:         hovered,
		LastScrollTime:  h.lastActive,
		IdleFor:         idle,
		ActiveThisFrame: active,
		RecentlyActive:  recent || active,
		TrackLength:     trackLen,
		ThumbLength:     thumbLen,
		ThumbOffset:     thumbOff,
		ThumbMinLength:  thumbMin,
	}

	h.prevOffset = live
	h.havePrev = true
	h.frame = fn
	h.haveCache = true
	h.cached = st
	return st
}

// markScrollActivity records bar-driven scrolling (thumb drag / track jump)
// so IdleFor / RecentlyActive stay honest when OffsetDelta was already
// consumed by a same-frame GetScrollingState cache.
func markScrollActivity() {
	h := Use[scrollActivity]("scroll-activity")
	h.lastActive = time.Now()
	h.cached.LastScrollTime = h.lastActive
	h.cached.IdleFor = 0
	h.cached.RecentlyActive = true
	h.cached.ActiveThisFrame = true
}

// ScrollBarAttrs configures ScrollBarExt: track chrome and optional custom thumb.
// Interaction (track click, thumb drag) stays inside ScrollBarExt.
type ScrollBarAttrs struct {
	// Accent is reserved for custom Thumb painters that want a brand color.
	// The package default modern thumb ignores it (neutral gray only).
	Accent Vec4

	// TrackWidth is the floating bar width (hit target). Zero: SCROLLBAR_WIDTH.
	TrackWidth float32
	// TrackBG is the track background. The zero value is transparent (alpha 0).
	// The package default uses a transparent track (modern overlay).
	TrackBG Vec4
	// TrackPad is inner padding of the track. Zero: defaultTrackPad (2).
	TrackPad float32

	// ThumbMinHeight is the shortest thumb. Zero: defaultThumbMinHeight.
	ThumbMinHeight float32
	// Thumb draws the thumb face for the given size. Nil: modern overlay pill
	// (neutral gray; darker on hover/drag). Interaction is handled by
	// ScrollBarExt; only paint here.
	Thumb func(size Vec2)
}

// ScrollBarsAttrs is the legacy accent-only config. Prefer ScrollBarAttrs.
type ScrollBarsAttrs struct {
	Accent Vec4 // zero value: use the package-level Accent
}

// ScrollBarFn draws a floating vertical scrollbar for the current container
// (call after ScrollOnInput). Returns the track id, or nil when no bar is needed.
// Same contract as ScrollBars / ScrollBarExt.
type ScrollBarFn func() ContainerId

// DefaultScrollBarStyle is the built-in modern overlay scrollbar: transparent
// track, thin rounded neutral-gray thumb (darker on hover/drag). Use this when
// you need the package look even if DefaultScrollBar was overridden (e.g. a
// gallery panel next to custom skins). ScrollBars() uses DefaultScrollBar
// instead.
func DefaultScrollBarStyle() ContainerId {
	// TrackBG zero = transparent. Thumb nil → modern paint in ScrollBarExt.
	return ScrollBarExt(ScrollBarAttrs{})
}

// DefaultScrollBar is the app-wide scrollbar drawer used by ScrollBars() and by
// widgets that embed a bar (VirtualList, menus, …). Assign at startup to skin
// every standard scrollbar without threading attrs through each call site:
//
//	widgets.DefaultScrollBar = myDarkBar
//
// or SetDefaultScrollBar(myDarkBar). Per-site skins still call ScrollBarExt
// (or DefaultScrollBarStyle) directly. Buttons and other simple widgets stay
// call-site styled; bars are nested chrome, so a package default fits better.
//
// Nil is treated as DefaultScrollBarStyle (SetDefaultScrollBar(nil) restores
// the package default style).
var DefaultScrollBar ScrollBarFn = DefaultScrollBarStyle

// SetDefaultScrollBar sets DefaultScrollBar. A nil fn restores DefaultScrollBarStyle.
func SetDefaultScrollBar(fn ScrollBarFn) {
	if fn == nil {
		DefaultScrollBar = DefaultScrollBarStyle
		return
	}
	DefaultScrollBar = fn
}

// ScrollBars draws the app default floating vertical scrollbar over the
// current container when content overflows (see DefaultScrollBar). Call inside
// a scrolling container after ScrollOnInput.
func ScrollBars() ContainerId {
	fn := DefaultScrollBar
	if fn == nil {
		fn = DefaultScrollBarStyle
	}
	return fn()
}

// ScrollBarsExt is the legacy accent-only entry point (always package default
// chrome, not DefaultScrollBar). Prefer ScrollBarExt or SetDefaultScrollBar.
func ScrollBarsExt(attrs ScrollBarsAttrs) ContainerId {
	return ScrollBarExt(ScrollBarAttrs{Accent: attrs.Accent})
}

// ScrollBarExt draws a vertical floating scrollbar for the current container.
// Returns the track container id, or nil when no bar is needed.
//
// Call after ScrollOnInput on the scrollable container (same timing as before).
func ScrollBarExt(attrs ScrollBarAttrs) ContainerId {
	st := GetScrollingState()
	if !st.Needed {
		Void()
		return nil
	}
	trackW := attrs.TrackWidth
	if trackW <= 0 {
		trackW = SCROLLBAR_WIDTH
	}
	pad := attrs.TrackPad
	if pad <= 0 {
		pad = defaultTrackPad
	}
	thumbMin := attrs.ThumbMinHeight
	if thumbMin <= 0 {
		thumbMin = defaultThumbMinHeight
	}

	trackBG := attrs.TrackBG // zero = transparent

	// Recompute thumb with caller min height (state used default min).
	viewportH := st.Viewport[1]
	contentH := st.Content[1]
	trackLen := viewportH - pad*3
	if trackLen < 1 {
		trackLen = max(float32(0), viewportH)
	}
	thumbH := thumbMin
	if contentH > 0 && trackLen > 0 {
		thumbH = trackLen * (viewportH / contentH)
	}
	thumbH = max(thumbMin, thumbH)
	if thumbH > trackLen && trackLen > 0 {
		thumbH = trackLen
	}
	maxScroll := st.MaxOffset[1]
	maxThumb := max(float32(0), trackLen-thumbH)
	scrollY := st.Offset[1]
	var thumbY float32
	if maxScroll > 0 && maxThumb > 0 {
		thumbY = maxThumb * (scrollY / maxScroll)
	}

	var scrollbarChange bool
	var offsetChangeTo Vec2
	var trackId ContainerId

	// Track + thumb always NoAnimate: scroll chrome snaps with the offset
	// (easing the bar against wheel/drag feels laggy).
	trackFns := []AttrsFn{
		NoAnimate,
		Float(st.Viewport[0]-trackW, 0),
		InFront,
		Pad(pad),
		FixSize(trackW, float32(int(viewportH))),
		BackgroundVec(trackBG),
	}

	ContainerWithKey("scroll-bar-track", Attrs(trackFns...), func() {
		trackId = CurrentId()
		desiredThumb := thumbY

		if IsClicked() {
			// Jump thumb so its center meets the click (same as before).
			local := Vec2Sub(GetInputState().MousePoint, GetScreenRect().Origin)
			desiredThumb = local[1] - (thumbH / 2)
			scrollbarChange = true
			markScrollActivity()
		}

		Element(Attrs(FixHeight(float32(int(thumbY)))))

		thumbInnerW := trackW - pad*2
		if thumbInnerW < 1 {
			thumbInnerW = 1
		}
		// Children inherit NoAnimate from the track (cascade).
		Container(Attrs(FixHeight(float32(int(thumbH))), Expand), func() {
			PressAction()
			dragging := IsActive()
			if dragging {
				scrollbarChange = true
				desiredThumb = thumbY + GetFrameInput().Motion[1]
				markScrollActivity()
			}
			sz := Vec2{thumbInnerW, float32(int(thumbH))}
			if attrs.Thumb != nil {
				attrs.Thumb(sz)
			} else {
				// Modern overlay: neutral gray only (no accent — same idea as
				// text fields). Darker / more opaque on hover and drag.
				// No grip icon — the pill silhouette is the affordance.
				bg := Vec4{0, 0, 45, 0.40}
				if dragging {
					bg = Vec4{0, 0, 35, 0.72}
				} else if IsHovered() {
					bg = Vec4{0, 0, 40, 0.58}
				}
				r := sz[0] / 2
				if r < 1 {
					r = 1
				}
				Element(Attrs(
					FixSizeVec(sz),
					Corners(r),
					BackgroundVec(bg),
				))
			}
		})

		if scrollbarChange {
			if maxThumb > 0 {
				y := maxScroll * (desiredThumb / maxThumb)
				y = max(float32(0), min(y, maxScroll))
				offsetChangeTo = Vec2{0, y}
			}
		}
	})

	if scrollbarChange {
		SetScrollOffset(offsetChangeTo)
	}
	return trackId
}

// StringHeadersEqual reports whether a and b are the same string by identity —
// same backing pointer and length — without comparing their contents. It's a
// fast "is this literally the same string value" check for stable/interned
// strings; distinct allocations of equal content are not equal.
func StringHeadersEqual(a, b string) bool {
	return unsafe.StringData(a) == unsafe.StringData(b) && len(a) == len(b)
}

// LargeTextListKey is the VirtualListView key used by LargeText. Call
// VirtualListView_ScrollToIndex(LargeTextListKey, 0) when the user explicitly
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
// VirtualListView_ScrollToIndex(LargeTextListKey, 0).
func LargeText(text string, styleFn ...TextStyleFn) {
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

		var vpad = TextStyle().FontSize / 4
		n := len(data.starts)

		type LineNo int

		itemKey := func(idx int) any {
			return LineNo(idx)
		}

		itemView := func(idx int, width f32) {
			line := lineAt(data.text, data.starts, data.lastEnd, idx)
			// Keep the vlist width as an explicit max on the row host so Text
			// inherits it via cascade (and soft-wraps to that budget).
			Container(Attrs(Pad2(vpad, 0), Expand, MaxWidth(width)), func() {
				Label(line, styleFn...)
			})
		}

		itemHeight := func(idx int, width f32) f32 {
			shaped := ShapeTextMax(lineAt(data.text, data.starts, data.lastEnd, idx), TextStyle(styleFn...), width)
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
	// content width. Optional: if nil, VirtualList measures ItemView with
	// shirei.Measure under the row width (same builder as paint). Prefer a
	// custom fn only for cheap fixed/heuristic heights.
	ItemHeight ItemHeightFn

	// ItemView renders the item at index for the given content width.
	// Also used for auto-height when ItemHeight is nil.
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

	// OutFirstVisible / OutLastVisible, if non-nil, are written with the
	// inclusive index range of rows the list actually built this frame
	// (the painted window). Empty list → both -1. Same timing as OutScrollOffset.
	// This is the list's own truth — do not re-derive from scrollY + guessed heights.
	OutFirstVisible *int
	OutLastVisible  *int

	// AvgSampleTop / AvgSampleBottom are how many items from each end feed the
	// average-height TotalHeight estimate (TotalHeight ≈ avg × ItemCount).
	// Both zero → defaults (top N=50, bottom 0). Overlap is not double-counted:
	// if top+bottom ≥ ItemCount, every row is sampled once and the mean is
	// exact. Cheap ItemHeight callers can pass (n+1)/2 and n/2 to cover the
	// whole list. See docs/virtual-list.md §5 when the default sample mis-
	// estimates the scrollbar range (region-skewed heights).
	AvgSampleTop    int
	AvgSampleBottom int
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

const vlistScrollToEnd = "scroll-to-end"

// VirtualListView_ScrollToEnd asks the list to set its vertical scroll so the
// content end sits margin pixels below the bottom of the viewport (margin 0 =
// flush with the last row). The list measures a real tail rather than trusting
// the average-height TotalHeight estimate, and seeds the anchor near the end so
// large lists do not walk from a stale top anchor.
//
// Use this for pin-to-bottom (e.g. LogView): re-post each frame while pinned.
// Prefer ScrollToIndex for “show this item” restores — do not program in raw
// pixel offsets (variable row heights make them unstable).
func VirtualListView_ScrollToEnd(listKey any, margin f32) {
	PostCommand(vlistWidget, listKey, vlistScrollToEnd, max(f32(0), margin))
}

func _VirtualListTakeScrollToEnd(listKey any) (f32, bool) {
	if listKey == nil {
		return 0, false
	}
	return TakeCommand[f32](vlistWidget, listKey, vlistScrollToEnd)
}

const vlistScrollToIndex = "scroll-to-index"

// vlistToIndex is the payload for ScrollToIndex / ScrollToIndexAt.
type vlistToIndex struct {
	Index int
	// Frac places the item's top at this fraction of the viewport height
	// (0 = top of view, 0.5 = middle, 1 = bottom). Clamped to [0, 1].
	Frac f32
}

// VirtualListView_ScrollToIndex asks the list to put item index at the top of
// the viewport on its next render (clamped if the tail is shorter than the
// viewport). Uses the list's own height walk, not a caller-supplied Y.
// Last request wins among scroll commands that frame.
func VirtualListView_ScrollToIndex(listKey any, index int) {
	VirtualListView_ScrollToIndexAt(listKey, index, 0)
}

// VirtualListView_ScrollToIndexAt is like ScrollToIndex, but places the item's
// top at viewportFrac of the viewport height (0 = top, ~0.5 = middle, 1 =
// bottom). Useful for find-next/prev so hits land mid-view rather than flush
// to the top edge. The list still owns height walking and clamping.
func VirtualListView_ScrollToIndexAt(listKey any, index int, viewportFrac f32) {
	if listKey == nil {
		return
	}
	PostCommand(vlistWidget, listKey, vlistScrollToIndex, vlistToIndex{Index: index, Frac: viewportFrac})
}

func _VirtualListTakeScrollToIndex(listKey any) (vlistToIndex, bool) {
	if listKey == nil {
		return vlistToIndex{}, false
	}
	return TakeCommand[vlistToIndex](vlistWidget, listKey, vlistScrollToIndex)
}

// VirtualListView renders a scrolling list whose items may have different
// heights, laying out only the visible rows. key is forwarded to
// ContainerWithKey (nil = anonymous positional identity) and is the address that
// VirtualListScrollIntoView and the other command helpers post to — use a typed
// pointer to app-owned data, unique among live widgets.
//
// itemHeightFn may be nil: heights are then measured from itemViewFn via Measure.
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

	// N is the default sample window: average-height head sample, near-end
	// tail walks, and jump-scroll edge re-anchors all use it (or N*2).
	const N = 50

	// Resolve average sample sizes from attrs (see VirtualListAttrs).
	// Both zero → historical default: top N, bottom 0.
	avgTop := attrs.AvgSampleTop
	avgBot := attrs.AvgSampleBottom
	if avgTop == 0 && avgBot == 0 {
		avgTop = N
	}
	if avgTop < 0 {
		avgTop = 0
	}
	if avgBot < 0 {
		avgBot = 0
	}

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

		// VirtualListView_ScrollToEnd latch: margin is distance from the
		// content bottom (0 = flush end). Survives the width-unknown first
		// frame and multi-frame settle while the tail is still learning.
		endMargin f32
		toEnd     bool

		// VirtualListView_ScrollToIndex / At: pin this item; Frac is where its
		// top sits in the viewport (0 = top edge).
		toIndex     int
		toIndexFrac f32
		hasToIndex  bool

		// Learned content-end floor: max of the average-height estimate and
		// extents measured while scrolling / ScrollToEnd. Average-height
		// alone undershoots when lower rows are taller than the top sample
		// (continuous wheel then clamps at a FALSE BOTTOM). Invalidated
		// when width or itemCount changes.
		endFloor      f32
		endFloorCount int
		endFloorWidth f32
	}

	// heightOf: explicit ItemHeight, or Measure(ItemView) with no cache.
	heightOf := func(index int, width f32) f32 {
		if itemHeightFn != nil {
			return max(1, itemHeightFn(index, width))
		}
		if itemViewFn == nil {
			return 1
		}
		h := Measure(Vec2{width, 0}, func() {
			itemViewFn(index, width)
		})[1]
		if h < 1 {
			return 1
		}
		return h
	}

	// computeAverageHeight samples up to avgTop items from the head and
	// avgBot from the tail. Ranges that meet or overlap are not double-counted
	// (top+bottom ≥ itemCount → every row once → exact mean).
	computeAverageHeight := func(width f32) f32 {
		if itemCount <= 0 {
			return 1
		}
		topN := min(avgTop, itemCount)
		botN := min(avgBot, itemCount)
		if topN+botN <= 0 {
			return 1
		}
		var seenHeight f32
		var seen int
		if topN+botN >= itemCount {
			for i := 0; i < itemCount; i++ {
				seenHeight += heightOf(i, width)
			}
			return seenHeight / f32(itemCount)
		}
		for i := 0; i < topN; i++ {
			seenHeight += heightOf(i, width)
			seen++
		}
		for i := itemCount - botN; i < itemCount; i++ {
			seenHeight += heightOf(i, width)
			seen++
		}
		return seenHeight / f32(seen)
	}

	// sumHeightsFrom is Σ heightOf(i) for i in [from, itemCount).
	sumHeightsFrom := func(from int, width f32) f32 {
		var s f32
		for i := from; i < itemCount; i++ {
			s += max(1, heightOf(i, width))
		}
		return s
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
				result.Offset -= heightOf(result.Index, width)
				if result.Offset <= scrollOffset {
					break
				}
			}
		} else {
			// scrolling down
			for result.Index < itemCount-1 {
				h := heightOf(result.Index, width)
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
				offset -= heightOf(i, width)
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
		if margin, ok := _VirtualListTakeScrollToEnd(key); ok {
			state.endMargin = max(0, margin)
			state.toEnd = true
			state.hasToIndex = false
		}
		if cmd, ok := _VirtualListTakeScrollToIndex(key); ok {
			state.toIndex = cmd.Index
			state.toIndexFrac = cmd.Frac
			state.hasToIndex = true
			state.toEnd = false
		}
		// ScrollToEnd is applied after width is known (needs a real tail measure).
		if state.toEnd {
			if itemCount == 0 {
				state.toEnd = false
			} else if GetRenderData().ContentSize[1] == 0 {
				// keep the latch alive across the empty / width-unknown first frames
				RequestNextFrame()
			}
		}
		// ScrollToIndex: keep requesting frames until width is known so a
		// tab-restore command is not lost on the first empty pass.
		if state.hasToIndex && GetRenderData().ContentSize[1] == 0 && itemCount > 0 {
			RequestNextFrame()
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
			if attrs.OutFirstVisible != nil {
				*attrs.OutFirstVisible = -1
			}
			if attrs.OutLastVisible != nil {
				*attrs.OutLastVisible = -1
			}
			RequestNextFrame()
			return
		}

		// compute average height
		avgHeight := computeAverageHeight(width)

		var totalHeight0 = state.TotalHeight
		// Base estimate: mean(sample) × count. Exact when the sample covers
		// every row; approximate otherwise (top/bottom skew).
		state.TotalHeight = avgHeight * f32(itemCount)
		// Keep extents learned from *exact* rest measures while geometry is
		// unchanged (tall-tail under-estimate). Invalidate on count/width change.
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

		// ScrollToIndex / At: list-owned height walk; Frac places item top in view.
		if state.hasToIndex && itemCount > 0 {
			targetIndex := state.toIndex
			if targetIndex < 0 {
				targetIndex = 0
			}
			if targetIndex >= itemCount {
				targetIndex = itemCount - 1
			}
			top := state.Anchor.Offset
			for i := state.Anchor.Index; i < targetIndex; i++ {
				top += heightOf(i, width)
			}
			for i := state.Anchor.Index; i > targetIndex; i-- {
				top -= heightOf(i-1, width)
			}
			frac := state.toIndexFrac
			if frac < 0 {
				frac = 0
			}
			if frac > 1 {
				frac = 1
			}
			// itemTop - scroll = frac * viewportH  →  scroll = itemTop - frac*viewH
			target := top - frac*size[1]
			maxScroll := max(0, state.TotalHeight-size[1])
			if target < 0 {
				target = 0
			}
			if target > maxScroll {
				target = maxScroll
			}
			SetScrollOffset(Vec2{0, target})
			RequestNextFrame()
			scroll = GetScrollOffset()
			state.hasToIndex = false
			// Seed anchor at the target so the visible walk starts coherently.
			state.Anchor = ItemOffset{Index: targetIndex, Offset: max(0, top)}
			state.ScrollOffset = scroll[1]
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
					top += heightOf(i, width)
				}
				for i := state.Anchor.Index; i > targetIndex; i-- {
					top -= heightOf(i-1, width)
				}
				height := heightOf(targetIndex, width)

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
		// TotalHeight coordinate system.
		if state.toEnd {
			if itemCount <= 0 {
				state.toEnd = false
				state.endFloor = 0
				SetScrollOffset(Vec2{})
				state.ScrollOffset = 0
			} else {
				// Exact sum of the last min(N*2, n) rows; keep avg×count for
				// the unmeasured prefix (no measuredThrough+remaining×avg).
				tailStart := 0
				if itemCount > N*2 {
					tailStart = itemCount - N*2
				}
				tailH := sumHeightsFrom(tailStart, width)
				contentEnd := tailH
				if tailStart > 0 {
					contentEnd = avgHeight*f32(tailStart) + tailH
				}
				// Only raise from an exact tail measure (under-estimate catch-up).
				if contentEnd > state.TotalHeight {
					state.TotalHeight = contentEnd
					state.endFloor = state.TotalHeight
					state.endFloorCount = itemCount
					state.endFloorWidth = width
				}
				lastH := max(1, heightOf(itemCount-1, width))
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
			height := heightOf(idx, width)
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

		// Learn TotalHeight only from solid measurements — never from
		// measuredThrough + remaining×avg. That formula overshoots whenever
		// the painted prefix is taller than the global mean (tall head), even
		// when avg itself is exact (mean×count was already right).
		//
		// Under-estimates (tall tail, short sample) still catch up: when the
		// walk overruns the estimate or we near the reported end, sum the
		// real rest and *set* TotalHeight to that exact content end.
		if endIndex >= itemCount {
			state.TotalHeight = measuredThrough
			spaceAfter = 0
		} else {
			remaining := itemCount - endIndex
			nearReportedEnd := measuredThrough+size[1] >= state.TotalHeight-avgHeight
			measureRest := remaining <= N*2 || nearReportedEnd || measuredThrough > state.TotalHeight
			if measureRest {
				contentEnd := measuredThrough + sumHeightsFrom(endIndex, width)
				// Exact through the end: assign (corrects both under- and over-estimate).
				state.TotalHeight = contentEnd
			}
			// else: keep avg×count (and any endFloor already applied above)
			spaceAfter = max(0, state.TotalHeight-measuredThrough)
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
				contentEnd := measuredThrough + sumHeightsFrom(endIndex, width)
				state.TotalHeight = contentEnd
				spaceAfter = max(0, state.TotalHeight-measuredThrough)
				state.endFloor = state.TotalHeight
				state.endFloorCount = itemCount
				state.endFloorWidth = width
				lastH := max(1, heightOf(itemCount-1, width))
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

		// Settled readbacks — after every SetScrollOffset above.
		scrollOut := GetScrollOffset()[1]
		maxOut := max(0, state.TotalHeight-size[1])
		if attrs.OutScrollOffset != nil {
			*attrs.OutScrollOffset = scrollOut
		}
		if attrs.OutMaxScrollOffset != nil {
			*attrs.OutMaxScrollOffset = maxOut
		}
		// Painted window from this frame's build loop (authoritative).
		firstVis, lastVis := -1, -1
		if itemCount > 0 && startIndex < endIndex {
			firstVis = startIndex
			lastVis = endIndex - 1
		}
		if attrs.OutFirstVisible != nil {
			*attrs.OutFirstVisible = firstVis
		}
		if attrs.OutLastVisible != nil {
			*attrs.OutLastVisible = lastVis
		}

		// DebugPanel lines (no-op unless the app calls DebugPanel this frame).
		// Multiple lists each append a block; last paint wins for eyeballing
		// the active pane if you only watch the trailing lines.
		DebugVar("vlist.n", itemCount)
		DebugVar("vlist.scrollY", scrollOut)
		DebugVar("vlist.maxScroll", maxOut)
		DebugVar("vlist.fromBottom", maxOut-scrollOut)
		DebugVar("vlist.totalH", state.TotalHeight)
		DebugVar("vlist.avgH", avgHeight)
		DebugVar("vlist.avgTop", avgTop)
		DebugVar("vlist.avgBot", avgBot)
		DebugVar("vlist.endFloor", state.endFloor)
		DebugVar("vlist.viewH", size[1])
		DebugVar("vlist.width", width)
		DebugVar("vlist.anchor.i", state.Anchor.Index)
		DebugVar("vlist.anchor.y", state.Anchor.Offset)
		DebugVar("vlist.first", firstVis)
		DebugVar("vlist.last", lastVis)
		DebugVar("vlist.spaceBefore", spaceBefore)
		DebugVar("vlist.spaceAfter", spaceAfter)
	})
}
