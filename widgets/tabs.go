package widgets

import . "go.hasen.dev/shirei"

// TabStyle supplies literal colors for flat tabs and their strip.
type TabStyle struct {
	Surface, Hovered, Selected SurfaceColors
	FocusRing                  Vec4
	ScrollBar                  ScrollBarStyle
}

// TabStyleForScheme uses canvas for the strip and panel for the active tab.
func TabStyleForScheme(s ColorScheme) TabStyle {
	return TabStyle{Surface: s.Surfaces.Canvas, Hovered: s.List.Hovered, Selected: s.Surfaces.Panel, FocusRing: s.FocusRing, ScrollBar: s.ScrollBar}
}

// TabStrip lays out horizontally scrollable tabs and an optional fixed trailing
// control. Call TabItem in tabs; trailing can hold a new-tab button. The application
// owns tab order, selection, and close requests.
func TabStrip(tabs, trailing func()) {
	TabStripStyled(tabs, trailing, TabStyleForScheme(CurrentColorScheme))
}

// TabStripStyled renders a tab strip using literal supplied colors.
func TabStripStyled(tabs, trailing func(), style TabStyle) {
	Container(Attrs(Row, Expand, FixHeight(32), BackgroundVec(style.Surface.Background), AmendTextStyle(TextColorVec(style.Surface.Text))), func() {
		Container(Attrs(Row, Grow(1), Extrinsic, Clip, Expand), func() {
			ScrollOnInput()
			ScrollBarStyled(ScrollBarAttrs{}, style.ScrollBar)
			Container(Attrs(Row), tabs)
		})
		if trailing != nil {
			Container(Attrs(Row, CrossMid, Expand, Pad2(0, 10)), trailing)
		}
	})
	Element(Attrs(Expand, FixHeight(1), BackgroundVec(style.Selected.Border)))
}

// TabItem renders a flat tab with an active underline and returns true when pressed.
// content appends optional details or TabCloseButton after the label. Defer
// removing a closed tab until after the loop that renders the strip.
func TabItem(key any, label string, selected bool, content func()) bool {
	return TabItemStyled(key, label, selected, content, TabStyleForScheme(CurrentColorScheme))
}

// TabItemStyled renders a tab using literal supplied colors.
func TabItemStyled(key any, label string, selected bool, content func(), style TabStyle) (clicked bool) {
	ContainerWithKey(key, Attrs(Row, CrossMid, Gap(10), Pad2(0, 12), FixHeight(32), BackgroundVec(style.Surface.Background), AmendTextStyle(TextColorVec(style.Surface.Text))), func() {
		NextAccessRole("tab")
		NextAccessChecked(selected)
		AssignAccess()
		st := ProcessButtonEvents(false)
		clicked = st.Clicked
		if st.Hovered {
			ModAttrs(BackgroundVec(style.Hovered.Background), AmendTextStyle(TextColorVec(style.Hovered.Text)))
		}
		if st.FocusVisible {
			ModAttrs(BorderWidth(1), BorderColorVec(style.FocusRing))
		}
		if selected {
			ModAttrs(BackgroundVec(style.Selected.Background), AmendTextStyle(TextColorVec(style.Selected.Text)))
			Element(Attrs(Float(0, 30), FixSize(GetResolvedWidth(), 2), BackgroundVec(style.FocusRing)))
		}
		Container(Attrs(MaxWidth(180), Clip), func() {
			weight := WeightNormal
			if selected {
				weight = WeightBold
			}
			Label(label, FontWeight(weight))
		})
		if content != nil {
			content()
		}
	})
	return clicked
}

// TabCloseButton is the compact mouse- and keyboard-accessible close control
// for a tab. It consumes pending accessibility attributes like other buttons.
func TabCloseButton() bool {
	return TabCloseButtonStyled(TabStyleForScheme(CurrentColorScheme))
}

// TabCloseButtonStyled renders a close control using literal supplied colors.
func TabCloseButtonStyled(style TabStyle) (clicked bool) {
	Container(Attrs(Pad(3), Corners(3)), func() {
		NextAccessRole("button")
		NextAccessLabel("Close tab")
		AssignAccess()
		st := ProcessButtonEvents(false)
		clicked = st.Clicked
		color := style.Surface.Text
		if st.Hovered || st.FocusVisible {
			ModAttrs(BackgroundVec(style.Hovered.Background))
			color = style.Hovered.Text
		}
		Icon(TypTimes, FontSize(10), TextColorVec(color))
	})
	return clicked
}
