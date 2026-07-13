package widgets

import (
	"runtime"
	"strings"

	. "go.hasen.dev/shirei"
)

// a LogView line index, used as the per-row container key when LineID is unavailable
type _LogLineNo int

// a position within a LogView's lines: rune index Rune within line Line
type logPos struct {
	Line int
	Rune int
}

func logPosLess(a, b logPos) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Rune < b.Rune)
}

type logSelection struct {
	Selecting bool // mouse is down and dragging
	Anchor    logPos
	Head      logPos
}

func (s *logSelection) ordered() (from, to logPos) {
	from, to = s.Anchor, s.Head
	if logPosLess(to, from) {
		from, to = to, from
	}
	return
}

// the [from, to) rune range of line idx covered by the selection
func (s *logSelection) lineRange(idx int, runeCount int) (int, int) {
	from, to := s.ordered()
	if from == to || idx < from.Line || idx > to.Line {
		return 0, 0
	}
	lo, hi := 0, runeCount
	if idx == from.Line {
		lo = min(from.Rune, runeCount)
	}
	if idx == to.Line {
		hi = min(to.Rune, runeCount)
	}
	return lo, hi
}

// the width of the selected prefix of the first wrapped line: the advances of
// glyphs with rune index below hi (assumes left-to-right text)
func selectedPrefixWidth(shaped ShapedText, hi int) f32 {
	if len(shaped.Lines) == 0 {
		return 0
	}
	var w f32
	for _, s := range shaped.Lines[0].Segments {
		for _, g := range s.Glyphs {
			if int(g.Cluster) < hi {
				w += g.XAdvance
			}
		}
	}
	return w
}

func (s *logSelection) copyText(ring *TextRing) (string, bool) {
	from, to := s.ordered()
	n := ring.Len()
	if from == to || from.Line >= n {
		return "", false
	}
	to.Line = min(to.Line, n-1)
	var b strings.Builder
	for i := from.Line; i <= to.Line; i++ {
		runes := []rune(ring.Line(i))
		lo, hi := s.lineRange(i, len(runes))
		if i > from.Line {
			b.WriteByte('\n')
		}
		b.WriteString(string(runes[lo:hi]))
	}
	return b.String(), true
}

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
func LogView(ring *TextRing, attrs TextAttrSet) {
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
func LogViewExt(ring *TextRing, attrs TextAttrSet, listKey any, probe *LogViewProbe) {
	logView(ring, attrs, listKey, probe)
}

// logView is the shared implementation.
func logView(ring *TextRing, attrs TextAttrSet, listKey any, probe *LogViewProbe) {
	if ring == nil {
		ring = &TextRing{}
	}
	Container(Attrs(Viewport, NoAnimate), func() {
		var vpad = attrs.Size / 4

		type logViewState struct {
			sel     logSelection
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
			sel.Anchor, sel.Head = logPos{}, logPos{}
			sel.Selecting = false
		}

		if FrameInput.Mouse == MouseRelease {
			sel.Selecting = false
		}
		if FrameInput.Mouse == MouseClick && !IsHovered() {
			sel.Anchor, sel.Head = logPos{}, logPos{}
		}

		var ctrl = ModCtrl
		if runtime.GOOS == "darwin" {
			ctrl = ModCmd
		}
		if ActiveCombo() == Combo(KeyC, ctrl) {
			if text, ok := sel.copyText(ring); ok {
				RequestTextCopy(text)
			}
		}

		n := ring.Len()
		if probe != nil {
			probe.ItemCount = n
		}
		newContent := n != st.lastN || ring.firstID != st.lastHead
		prevMax := st.maxScroll

		wheelUp := IsHovered() && FrameInput.Scroll[1] < 0
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
			if attrs.MaxWidth == 0 {
				attrs.MaxWidth = width
			}
			return ShapeText(ring.Line(idx), attrs)
		}

		itemHeight := func(idx int, width f32) f32 {
			shaped := shapeLine(idx, width)
			var height f32
			for _, shapedLine := range shaped.Lines {
				height += shapedLine.Height
			}
			height = max(height, attrs.Size)
			return height + (vpad * 2)
		}

		itemView := func(idx int, width f32) {
			if probe != nil && idx > probe.LastVisible {
				probe.LastVisible = idx
			}
			shaped := shapeLine(idx, width)
			rowHeight := itemHeight(idx, width)
			Container(Attrs(Pad2(vpad, 0), Expand, Grow(1)), func() {
				rowHovered := IsHovered()
				btnHovered := false

				type logCopyBtn int
				hasSelection := sel.Anchor != sel.Head
				if rowHovered && !sel.Selecting && !hasSelection {
					ModAttrs(Background(0, 0, 50, 0.08))
					btnSize := attrs.Size + 8
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
						Icon(SymCopy, FontSize(attrs.Size), TextColor(0, 0, 30, 1))
					})
				}

				if rowHovered && !btnHovered {
					pos := logPos{idx, ComputeCursorIndex(GetContentRect(), InputState.MousePoint, Vec2{}, shaped)}
					if FrameInput.Mouse == MouseClick {
						sel.Selecting = true
						sel.Anchor, sel.Head = pos, pos
					} else if sel.Selecting {
						sel.Head = pos
						RequestNextFrame()
					}
				}
				selFrom, selTo := sel.lineRange(idx, len(shaped.Runes))

				if from, to := sel.ordered(); from != to && len(shaped.Lines) > 0 {
					lastLine := &shaped.Lines[len(shaped.Lines)-1]
					if idx > from.Line && idx <= to.Line && selTo > 0 {
						w := selectedPrefixWidth(shaped, selTo)
						Element(Attrs(NoAnimate, FloatVec(Vec2{}), FixSize(w, vpad), BackgroundVec(SelectionColor)))
					}
					if idx >= from.Line && idx < to.Line {
						lastLeading := lastLine.Height - attrs.Size
						var blockH f32
						for li := range shaped.Lines {
							blockH += shaped.Lines[li].Height
						}
						blockH = max(blockH-lastLeading, attrs.Size)
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
			avgH := max(attrs.Size, 1) + vpad*2
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
