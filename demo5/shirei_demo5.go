package main

import (
	"fmt"

	app "go.hasen.dev/shirei/giobackend"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
	. "go.hasen.dev/shirei/widgets"
)

var s = ViewState{
	Counter:    10,
	PlusLabel:  "+",
	MinusLabel: "-",
}
var ss = SplittableView(s)

func main() {
	app.SetupWindow("Context Menu Demo", 800, 400)
	app.Run(func() {
		// ViewFn(&s)
		TileView(&ss)
	})
}

type ViewState struct {
	Counter    int
	ShowEditor bool
	PlusLabel  string
	MinusLabel string
}

func ViewFn(s *ViewState) {
	Layout(TW(Pad(10), Gap(10)), func() {

		Layout(TW(Row, Gap(10)), func() {
			if Button(0, s.PlusLabel) {
				s.Counter++
			}
			if Button(0, s.MinusLabel) {
				s.Counter--
			}
		})
		Label(fmt.Sprintf("Counter Value: %d", s.Counter))
		if Button(SymList, "Toggle Labels Editor") {
			s.ShowEditor = !s.ShowEditor
		}
		if s.ShowEditor {
			Layout(TW(Gap(10)), func() {
				Label("Plus Label")
				TextInput(&s.PlusLabel)
				Label("Minus Label")
				TextInput(&s.MinusLabel)
			})
		}

	})
}

type SplittableView ViewState

func (s *SplittableView) CloneState() View {
	var v = ViewState(*s)
	var s2 = SplittableView(v)
	return &s2
}

func (s *SplittableView) View() {
	ViewFn((*ViewState)(s))
}

// ==================================================================
// 			Splitable VIew
// ==================================================================

type View interface {
	CloneState() View
	View()
}

// viewport is itself a view!
type Tiler struct {
	// if set, we are a view container, if not, we have A and B tiles
	V View

	// if set, we split horizontally
	Row bool

	// offset of splitter from center point
	// 0 means both view get equal size
	Splitter float32

	A *Tiler
	B *Tiler
}

var tiler *Tiler

func TileView(v View) {
	// init tiler state
	if tiler == nil {
		tiler = new(Tiler)
		tiler.V = v
	}

	ViewTile(tiler)
}

const splitterSize = 10

func ViewTile(t *Tiler) {
	var attrs Attrs
	attrs.Row = t.Row
	attrs.ExtrinsicSize = true
	attrs.ExpandAcross = true
	attrs.Grow = 1
	attrs.NoAnimate = true
	Layout(attrs, func() {
		if t.V != nil {
			Layout(TW(Pad(2), Gap(2), Grow(1), Expand, BR(4)), func() {
				var split = false
				var splitRow bool
				// split buttons
				Layout(TW(Expand, Row, Pad(4), Gap(6), BG(0, 0, 0, 0.4)), func() {
					if CtrlButton(SymLandscape, "Split V", true) {
						split = true
						splitRow = false
					}
					if CtrlButton(SymPortrait, "Split H", true) {
						split = true
						splitRow = true
					}
				})
				// the view itself!
				Layout(TW(Clip, Extrinsic, Grow(1), Expand, BW(2), Bo(0, 0, 0, 1)), func() {
					ScrollOnInput()
					t.V.View()
				})
				if split {
					var a = new(Tiler)
					var b = new(Tiler)
					a.V = t.V
					b.V = t.V.CloneState()
					t.V = nil
					t.Row = splitRow
					t.A = a
					t.B = b
				}
			})
		} else {
			main, _ := MainCrossAxes(t.Row)
			sz := GetAvailableSize()
			sz[main] -= splitterSize
			sz1 := sz
			sz2 := sz
			sz1[main] = (sz[main] / 2) - t.Splitter
			sz2[main] = sz[main] - sz1[main]
			Layout(TW(FixSizeV(sz1), Bo(0, 0, 0, 1), BW(1), Clip), func() {
				ViewTile(t.A)
			})
			ViewSplitter(&t.Splitter, t.Row)
			Layout(TW(FixSizeV(sz2), Bo(0, 0, 0, 1), BW(1), Clip), func() {
				ViewTile(t.B)
			})
		}
	})
}

func ViewSplitter(s *float32, row bool) {
	var sz = Vec2{splitterSize, splitterSize}
	Layout(TW(FixSizeV(sz), Expand, BG(0, 0, 70, 1)), func() {
		PressAction()
		if IsActive() {
			if row {
				*s -= FrameInput.Motion[0]
			} else {
				*s -= FrameInput.Motion[1]
			}
		}
	})
}
