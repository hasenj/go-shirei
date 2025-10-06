package widgets

import (
	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
)

var _menuItemPressed bool

var _menuBG = Vec4{220, 20, 94, 1}

func MenuButton(label string, fn func()) {
	MenuButtonExt(label, ButtonAttrs{
		Icon: TypArrowSortedDown,
	}, fn)
}

func MenuButtonExt(label string, attrs ButtonAttrs, fn func()) {
	Layout(TW(), func() {
		type MenuState struct {
			open   bool
			btnId  any
			menuId any
		}
		var state = Use[MenuState]("menu-state")
		if ButtonExt(label, attrs) {
			state.open = !state.open
		}

		if state.open && _menuItemPressed {
			_menuItemPressed = false
			state.open = false
		}

		state.btnId = GetLastId()

		if state.open {
			Popup(func() {
				LayoutId("action-menu", TW(BR(6), BG(0, 0, 10, 0.2)), func() {
					ModAttrs(FloatV(_getPositionRelativeTo(state.btnId)))

					state.menuId = CurrentId()
					ModAttrs(func(a *Attrs) {
						a.Shadow.Blur = 40
						a.Shadow.Alpha = 0.3
					})
					Layout(TW(MinWidth(100), BR(4), Pad2(6, 0), Gap(2), MaxWidth(600), BGV(_menuBG), BW(1), Bo(0, 0, 10, 0.8), Clip), func() {
						ModAttrs(func(a *Attrs) {
							a.Shadow.Blur = 4
							a.Shadow.Alpha = 0.7
							a.Shadow.Offset[1] = 2
						})
						fn()
					})
				})
			})
		}

		// do this after handling the open menu so that clicks inside the menu can still register!
		if !IdIsHovered(state.btnId) && !IdIsHovered(state.menuId) && FrameInput.Mouse == MouseClick { // click outside!
			state.open = false
		}
	})
}

func _getPositionRelativeTo(anchorId any) Vec2 {
	targetRect := GetResolvedRectOf(anchorId)

	// naive: place it at the bottom of the target!
	const sp = 4
	var pos = targetRect.Origin
	pos[1] += targetRect.Size[1] + sp

	var selfSize = GetResolvedSize()
	if pos[0]+selfSize[0] > WindowSize[0] {
		pos[0] = WindowSize[0] - selfSize[0] - sp
	}
	if pos[1]+selfSize[1] > WindowSize[1] {
		pos[1] = WindowSize[1] - selfSize[1] - sp
	}

	pos[0] = max(0, pos[0])
	pos[1] = max(0, pos[1])

	return pos
}

func MenuSeparator() {
	Layout(TW(Expand, Pad2(4, 10)), func() {
		Element(TW(BG(0, 0, 0, 0.5), MinSize(1, 1), Expand))
		Element(TW(BG(0, 0, 100, 1), MinSize(1, 1), Expand))
	})
}

func MenuItemLabel(icon rune, label string) {
	Layout(TW(Row, Expand, CA(AlignMiddle), BGV(_menuBG), Pad2(4, 8), Gap(12)), func() {
		Icon(icon)
		Label(label, Sz(12), Clr(0, 0, 10, 1))
	})
}

func MenuItem(icon rune, label string) bool {
	return MenuItemExt(label, ButtonAttrs{Icon: icon})
}

func MenuItemExt(label string, attrs ButtonAttrs) bool {
	var action bool
	Layout(TW(Row, Expand, CA(AlignMiddle), BGV(_menuBG), Pad2(4, 8), Gap(12)), func() {
		if attrs.Disabled {
			ModAttrs(Trans(0.2))
		}

		if !attrs.Disabled {
			var hovered = IsHovered()
			action = PressAction()

			// hovering highlight
			const sp = 0
			sz := GetResolvedSize()
			sz[0] -= sp * 2
			sz[1] -= sp * 2
			var bg = Vec4{234, 92, 84, 0}
			if hovered {
				bg[ALPHA] = 0.8
			}
			Element(TW(Float(sp, sp), BR(2), MinSizeV(sz), BGV(bg)))
		}

		Icon(attrs.Icon)
		Label(label, Sz(12), Clr(0, 0, 10, 1))
	})
	if action {
		_menuItemPressed = true
	}
	return action
}

func PopupPanel(toggle *bool, anchorId any, a Attrs, fn func()) {
	if *toggle {
		var selfId any
		Popup(func() {
			Layout(TWW(a, Shd(14), BGV(_menuBG), BW(1), Bo(0, 0, 10, 0.8), Clip), func() {
				ModAttrs(FloatV(_getPositionRelativeTo(anchorId)))
				selfId = CurrentId()
				fn()
			})

			// do this after handling the open menu so that clicks inside the
			// menu can still register, but inside the popup call so that the
			// selfid has been set

			if !IdIsHovered(anchorId) && !IdIsHovered(selfId) && FrameInput.Mouse == MouseClick { // click outside!
				*toggle = false
			}
		})
	}
}
