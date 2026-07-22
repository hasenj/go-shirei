package widgets

import (
	. "go.hasen.dev/shirei"
)

// a LogView line index, used as the per-row container key when LineID is unavailable
type _LogLineNo int

// LogView displays an append-only TextRing, pinned to the bottom until the
// user scrolls up; scrolling back to the bottom re-pins it. Long lines wrap.
//
// Model:
//   - pinned starts true
//   - while pinned, VirtualListView_ScrollToEnd(0) when content changes or
//     scroll sits short of last frame's max (TotalHeight still learning)
//   - unpin only when scrollY decreases and is no longer at max
//     (a clamp from content eviction that leaves us at the new max stays pinned)
//   - re-pin when unpinned and scrollY is within a margin of maxScroll
//
// Pin policy lives here; VirtualList only supplies the end-relative scroll
// command (no Follow flag on the list).
//
// Text can be selected flat across lines by dragging, and copied with
// Cmd/Ctrl+C. Clicking outside the view clears the selection.
//
// Appends from background goroutines must happen under the frame lock
// (shirei.WithFrameLock) followed by shirei.RequestNextFrame. A nil ring
// draws an empty view.
func LogView(ring *TextRing, attrs TextStyleAttrs) {
	LogViewExt(ring, attrs, nil, nil)
}

// LogViewProbe is filled each frame for headless pin/streaming tests and
// behavior harnesses. Pass a non-nil pointer to LogViewExt.
type LogViewProbe struct {
	ScrollY     f32
	MaxScroll   f32
	Pinned      bool
	LastVisible int // highest row index rendered; -1 if none
	ItemCount   int // ring.Len() at the start of this frame's build
}

// LogViewExt is LogView with optional listKey (for command addressing) and
// probe (per-frame scroll/pin readbacks). Either may be nil.
func LogViewExt(ring *TextRing, attrs TextStyleAttrs, listKey any, probe *LogViewProbe) {
	logView(ring, attrs, listKey, probe)
}

// logView is the shared implementation.
func logView(ring *TextRing, attrs TextStyleAttrs, listKey any, probe *LogViewProbe) {
	if ring == nil {
		ring = &TextRing{}
	}
	Container(Attrs(Viewport, NoAnimate), func() {
		var vpad = attrs.FontSize / 4

		type logViewState struct {
			sel     LineSelection
			firstID int64

			pinned      bool
			started     bool
			scrollY     f32
			prevScrollY f32
			maxScroll   f32
			lastN       int
			lastHead    int64
		}
		st := Use[logViewState]("log-view")
		sel := &st.sel
		if listKey == nil {
			listKey = st
		}
		if probe != nil {
			probe.LastVisible = -1
			probe.ItemCount = 0
		}

		if !st.started {
			st.pinned = true
			st.started = true
		}

		if st.firstID != ring.firstID {
			st.firstID = ring.firstID
			sel.Clear()
		}

		n := ring.Len()
		LineSelectionFrame(sel, IsHovered(), n, ring.Line)
		if probe != nil {
			probe.ItemCount = n
		}
		newContent := n != st.lastN || ring.firstID != st.lastHead
		prevMax := st.maxScroll

		wheelUp := IsHovered() && GetFrameInput().Scroll[1] < 0
		// When pinned, stick to the true end if content changed (max will
		// grow) or we are short of last frame's max (TotalHeight still
		// learning). ScrollToEnd measures a real tail; ScrollTo(∞) did not.
		if st.pinned && n > 0 && !wheelUp && (newContent || st.scrollY+0.5 < prevMax) {
			VirtualListView_ScrollToEnd(listKey, 0)
		}

		itemKey := func(idx int) any {
			return ring.LineID(idx)
		}

		shapeLine := func(idx int, width f32) ShapedText {
			return ShapeTextMax(ring.Line(idx), attrs, width)
		}

		itemHeight := func(idx int, width f32) f32 {
			shaped := shapeLine(idx, width)
			var height f32
			for _, shapedLine := range shaped.Lines {
				height += shapedLine.Height
			}
			height = max(height, attrs.FontSize)
			return height + (vpad * 2)
		}

		itemView := func(idx int, width f32) {
			if probe != nil && idx > probe.LastVisible {
				probe.LastVisible = idx
			}
			shaped := shapeLine(idx, width)
			rowHeight := itemHeight(idx, width)
			Container(Attrs(Pad2(vpad, 0), Expand, Grow(1), MaxWidth(width)), func() {
				rowHovered := IsHovered()
				btnHovered := false

				type logCopyBtn int
				hasSelection := !sel.Empty()
				if rowHovered && !sel.Selecting && !hasSelection {
					ModAttrs(Background(0, 0, 50, 0.08))
					btnSize := attrs.FontSize + 8
					btnY := (rowHeight - btnSize) / 2
					ContainerWithKey(logCopyBtn(0), Attrs(NoAnimate, FloatVec(Vec2{width - btnSize - 2, btnY}),
						FixSize(btnSize, btnSize), Center, Corners(3),
						Background(0, 0, 92, 0.95)), func() {
						btnHovered = IsHovered()
						if btnHovered {
							ModAttrs(Background(0, 0, 82, 1))
						}
						if PressAction() {
							RequestTextCopy(ring.Line(idx))
						}
						Icon(SymCopy, FontSize(attrs.FontSize), TextColor(0, 0, 30, 1))
					})
				}

				if rowHovered && !btnHovered {
					sel.Hit(idx, shaped)
				}
				selFrom, selTo := sel.LineRange(idx, len(shaped.Runes))

				// Inter-row padding selection glue (wrap + vpad) — LogView layout
				// detail on top of the pure LineSelection range.
				if from, to := sel.Ordered(); from != to && len(shaped.Lines) > 0 {
					lastLine := &shaped.Lines[len(shaped.Lines)-1]
					if idx > from.Line && idx <= to.Line && selTo > 0 {
						w := SelectedPrefixWidth(shaped, selTo)
						Element(Attrs(NoAnimate, FloatVec(Vec2{}), FixSize(w, vpad), BackgroundVec(SelectionColor)))
					}
					if idx >= from.Line && idx < to.Line {
						lastLeading := lastLine.Height - attrs.FontSize
						var blockH f32
						for li := range shaped.Lines {
							blockH += shaped.Lines[li].Height
						}
						blockH = max(blockH-lastLeading, attrs.FontSize)
						Element(Attrs(NoAnimate, FloatVec(Vec2{0, vpad + blockH}),
							FixSize(lastLine.Width, vpad+lastLeading), BackgroundVec(SelectionColor)))
					}
				}

				ShapedTextLayout(shaped, attrs, selFrom, selTo)
			})
		}

		VirtualListViewExt(listKey, VirtualListAttrs{
			ItemCount:          n,
			ItemKey:            itemKey,
			ItemHeight:         itemHeight,
			ItemView:           itemView,
			OutScrollOffset:    &st.scrollY,
			OutMaxScrollOffset: &st.maxScroll,
		})

		if st.pinned {
			// Unpin only on a real scroll-up. A clamp from content
			// eviction that leaves us at the new max stays pinned.
			if st.scrollY < st.prevScrollY-0.5 && st.scrollY+0.5 < st.maxScroll {
				st.pinned = false
			}
		} else {
			avgH := max(attrs.FontSize, 1) + vpad*2
			margin := max(avgH*2, f32(8))
			if st.scrollY+margin >= st.maxScroll {
				st.pinned = true
			}
		}
		st.prevScrollY = st.scrollY
		st.lastN = n
		st.lastHead = ring.firstID

		if probe != nil {
			probe.ScrollY = st.scrollY
			probe.MaxScroll = st.maxScroll
			probe.Pinned = st.pinned
		}
	})
}
