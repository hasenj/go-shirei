package main

// The collapsible bottom panel — preview, deletion bin, and transfer
// queue are all the same interaction: a one-line header that must stay
// informative on its own, a chevron toggle scoped to the title zone
// (action buttons live outside it, so clicking one can never also
// toggle), and a body that keeps its node identity across the toggle so
// the height change animates instead of popping.

import (
	. "go.hasen.dev/shirei"
)

const panelHeaderH = 28

type PanelSpec struct {
	Id   string // stable identity; the body derives its own from it
	Open *bool

	Title   func() // toggle zone, after the chevron: what this panel is
	Actions func() // header content outside the toggle zone (optional)

	BodyH f32 // expanded body height (callers cap their lists)
	Body  func()

	// zero values = the neutral palette; the bin passes its reds
	Bg, Sep, Hover, Fg Vec4
}

func CollapsiblePanel(s PanelSpec) {
	if s.Bg == (Vec4{}) {
		s.Bg = Vec4{220, 14, 93, 1}
	}
	if s.Sep == (Vec4{}) {
		s.Sep = Vec4{220, 12, 80, 1}
	}
	if s.Hover == (Vec4{}) {
		s.Hover = Vec4{220, 14, 90, 1}
	}
	if s.Fg == (Vec4{}) {
		s.Fg = Vec4{0, 0, 45, 1}
	}
	ContainerWithKey(s.Id, Attrs(Expand, BackgroundVec(s.Bg)), func() {
		Element(Attrs(Expand, FixHeight(1), BackgroundVec(s.Sep)))
		Container(Attrs(Row, CrossMid, Expand, FixHeight(panelHeaderH), Gap(8), Pad4(0, 10, 0, 0)), func() {
			Container(Attrs(Row, CrossMid, Grow(1), FixHeight(panelHeaderH), Pad2(0, 10), Gap(8), Clip), func() {
				if IsHovered() {
					ModAttrs(BackgroundVec(s.Hover))
				}
				if PressAction() {
					*s.Open = !*s.Open
				}
				chevron := "▸"
				if *s.Open {
					chevron = "▾"
				}
				Label(chevron, FontSize(9), TextColorVec(s.Fg))
				s.Title()
			})
			if s.Actions != nil {
				s.Actions()
			}
		})
		h := f32(0)
		if *s.Open {
			h = s.BodyH
		}
		ContainerWithKey(s.Id+"-body", Attrs(Expand, FixHeight(h), Clip), func() {
			if h > 0 {
				s.Body()
			}
		})
	})
}
