package widgets

import (
	. "go.hasen.dev/shirei"
)

var _menuItemPressed bool

var _menuBG = DefaultBackground

// _popupBorder and _popupShadow are the shared "floating surface" treatment
// for menus and popup panels: a hairline border and a soft, low-contrast drop
// shadow (deliberately subtle — the surface should lift off the page, not
// stamp a heavy frame onto it).
func _popupBorder(a *AttrSet) {
	a.BorderWidth = 1
	a.BorderColor = Vec4{0, 0, 0, 0.08}
}

func _popupShadow(a *AttrSet) {
	a.Shadow.Blur = 16
	a.Shadow.Alpha = 0.12
	a.Shadow.Offset[1] = 3
}

// MenuButton renders a button that opens a dropdown menu, built by fn, when
// clicked. The menu closes when one of its items is chosen or the user clicks
// away.
func MenuButton(label string, fn func()) {
	MenuButtonExt(label, ButtonAttrs{
		Icon: TypArrowSortedDown,
	}, fn)
}

var _activePanelTrigger *bool

// ClosePopupPanel closes the popup or menu currently being built, from inside
// its own builder — e.g. from a menu item's handler that should dismiss the menu.
func ClosePopupPanel() {
	if _activePanelTrigger != nil {
		*_activePanelTrigger = false
	}
}

// MenuButtonExt is MenuButton with custom button attributes for the trigger.
func MenuButtonExt(label string, attrs ButtonAttrs, fn func()) {
	Container(Attrs(), func() {
		type MenuState struct {
			open   bool
			btnId  ContainerId
			menuId ContainerId
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

			var _prevTrigger = _activePanelTrigger
			_activePanelTrigger = &state.open
			defer func() {
				_activePanelTrigger = _prevTrigger
			}()

			Popup(func() {
				ContainerWithKey("action-menu", Attrs(MinWidth(100), MaxWidth(600), Corners(4),
					Pad2(6, 0), Gap(2), Clip, BackgroundVec(_menuBG), _popupBorder, _popupShadow), func() {
					ModAttrs(FloatVec(_getPositionRelativeTo(state.btnId)))
					state.menuId = CurrentId()
					fn()
				})
			})
		}

		// do this after handling the open menu so that clicks inside the menu can still register!
		if !IdIsHovered(state.btnId) && !IdIsHovered(state.menuId) && FrameInput.Mouse == MouseClick { // click outside!
			state.open = false
		}
	})
}

func _getPositionRelativeTo(anchorId ContainerId) Vec2 {
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

// MenuSeparator draws a thin horizontal divider between menu items.
func MenuSeparator() {
	Container(Attrs(Expand, Pad2(4, 10)), func() {
		Element(Attrs(Background(0, 0, 0, 0.08), MinSize(1, 1), Expand))
	})
}

// MenuItem renders a clickable menu row with an optional leading icon (pass 0
// for none) and returns true on the frame it is chosen.
func MenuItem(icon rune, label string) bool {
	return MenuItemExt(label, ButtonAttrs{Icon: icon})
}

// MenuItemExt is MenuItem configured by ButtonAttrs (icon, disabled state,
// accent).
func MenuItemExt(label string, attrs ButtonAttrs) bool {
	var action bool
	textColor := Vec4{0, 0, 10, 1}
	Container(Attrs(Row, Expand, CrossAlign(AlignMiddle), BackgroundVec(_menuBG), Pad2(4, 8), Gap(12)), func() {
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
			accent := AccentOrFallback(attrs.Accent, DefaultAccent)
			var bg = Vec4{accent[0], accent[1], accent[2], 0}
			if hovered {
				bg[ALPHA] = 0.8
				// hardcoded for now: ContrastingTextColor(accent) actually
				// picks black for every current preset (their luminance
				// sits just past the WCAG crossover where black overtakes
				// white), which reads worse here than a flat white does.
				textColor = Vec4{0, 0, 100, 1}
			}
			Element(Attrs(Float(sp, sp), Corners(2), MinSizeVec(sz), BackgroundVec(bg)))
		}

		Icon(attrs.Icon, TextColor(textColor[0], textColor[1], textColor[2], textColor[3]))
		Label(label, FontSize(12), TextColor(textColor[0], textColor[1], textColor[2], textColor[3]))
	})
	if action {
		_menuItemPressed = true
	}
	return action
}

// PopupPanel shows a floating panel, built by fn and styled by a, anchored to
// anchorId while *toggle is true. It closes (setting *toggle to false) when the
// user clicks outside it. anchorId is typically the ContainerId of the control
// that toggles it.
func PopupPanel(toggle *bool, anchorId ContainerId, a AttrSet, fn func()) {
	if *toggle {
		var _prevTrigger = _activePanelTrigger
		_activePanelTrigger = toggle
		defer func() {
			_activePanelTrigger = _prevTrigger
		}()
		var selfId ContainerId
		Popup(func() {
			Container(AttrsWith(a, BackgroundVec(_menuBG), _popupBorder, _popupShadow, Clip), func() {
				ModAttrs(FloatVec(_getPositionRelativeTo(anchorId)))
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
