package widgets

import (
	. "go.hasen.dev/shirei"
)

var _menuItemPressed bool
var _menuItemKeyboard bool

func _popupShadow(a *AttrSet) {
	a.Shadow.Blur = 6
	a.Shadow.Alpha = 0.12
	a.Shadow.Offset[1] = 2
}

var MenuIcon = TypArrowSortedDown

// MenuButton renders a button that opens a dropdown menu, built by fn, when
// clicked or when Space/Enter is pressed while the trigger is focused. The
// menu closes when one of its items is chosen, the user clicks away, Escape
// is pressed, or Tab moves to the next control.
// NextButton properties configure its trigger. A disabled trigger closes its menu.
//
// Typeahead filtering is opt-in: call MenuFilterQuery (or MenuFilterMatches)
// inside fn. Menus that never call it have no filter field and do not capture
// typing. See MenuFilterQuery.
//
// Up/Down move a keyboard selection (nothing is selected until Down, or
// until Down opens the menu). Enter or Space activates the selection.
// Escape clears the query (filterable) then closes. Tab walks Focusable
// contents (spliced after the trigger); leaving the popup closes it.
func MenuButton(icon IconGlyph, label string, fn func()) {
	attrs := takeButtonAttrs()
	attrs.Icon = icon
	menuButtonExt(label, attrs, DefaultButtonLook(), fn)
}

// CtrlMenuButton renders a menu with compact control-button chrome.
func CtrlMenuButton(icon IconGlyph, label string, fn func()) {
	attrs := takeButtonAttrs()
	attrs.Icon = icon
	menuButtonExt(label, attrs, DefaultCtrlButtonLook(), fn)
}

var _activePanelTrigger *bool

// ClosePopupPanel closes the popup or menu currently being built, from inside
// its own builder — e.g. from a menu item's handler that should dismiss the menu.
func ClosePopupPanel() {
	if _activePanelTrigger != nil {
		*_activePanelTrigger = false
	}
}

// menuFilterSession is live only while an open MenuButton is building its
// item list. MenuFilterQuery / MenuItem write through it.
type menuFilterSession struct {
	query     *string
	wants     *bool
	selected  *int // keyboard highlight among visible MenuItems this frame
	itemCount *int // incremented by each MenuItem while the session is live
	composing bool // filter field IME active — suppress Enter-to-activate
}

var _menuFilter *menuFilterSession

// MenuFilterQuery returns the typeahead query for the open menu and opts that
// menu into filtering. Call inside a MenuButton builder when the list is long
// enough to search. If the builder never calls it, the menu has no filter UI
// and typing is not captured.
//
// Empty query means "show everything". Pair with fuzzyMatch or
// MenuFilterMatches to hide non-matching MenuItems.
//
// The filter strip is shown when the committed query is non-empty or when
// ProcessTextInput reports IME composition (TextInputState.Composing) —
// so Japanese preedit reveals the field before the first commit.
func MenuFilterQuery() string {
	if _menuFilter == nil || _menuFilter.query == nil {
		return ""
	}
	if _menuFilter.wants != nil {
		*_menuFilter.wants = true
	}
	return *_menuFilter.query
}

// MenuFilterMatches reports whether label matches the current menu filter
// (empty query matches all). Opts the menu into filtering like MenuFilterQuery.
func MenuFilterMatches(label string) bool {
	q := MenuFilterQuery()
	if q == "" {
		return true
	}
	return fuzzyMatch(q, label) >= 0
}

// MenuButtonExt renders a menu whose trigger uses the supplied button
// look. The menu behavior and contents are otherwise identical to MenuButton.
// attrs replaces and clears pending NextButton properties in full.
func MenuButtonExt(label string, attrs ButtonAttrs, look ButtonLook, fn func()) {
	nextButtonAttrs = ButtonAttrs{}
	menuButtonExt(label, attrs, look, fn)
}

func menuButtonExt(label string, attrs ButtonAttrs, look ButtonLook, fn func()) {
	MenuButtonStyled(label, attrs, look, resolveButtonStyle(attrs), CurrentColorScheme.FocusRing, fn)
}

// MenuButtonStyled uses explicit paint and focus colors for the menu trigger.
// It follows ButtonStyled's attribute and pending-property rules. Popup surfaces
// and the widgets built by fn use their own styling.
func MenuButtonStyled(label string, attrs ButtonAttrs, look ButtonLook, style ButtonStyle, focusRing Vec4, fn func()) {
	nextButtonAttrs = ButtonAttrs{}
	Container(Attrs(), func() {
		NextAccessRole("menu")
		NextAccessDisabled(attrs.Disabled)
		AssignAccess()
		type MenuState struct {
			open   bool
			btnId  ContainerId
			menuId ContainerId
			// wantsFilter is sticky for this open session once MenuFilterQuery runs.
			wantsFilter bool
			filterQuery string
			// composing is set by the filter field after ProcessTextInput.
			composing bool
			// Keyboard selection among visible MenuItems.
			// -1 means no keyboard selection (default until the user presses Down).
			selected        int
			lastFilterQuery string
			itemCount       int // last frame's count; for clamp
		}
		var state = Use[MenuState]("menu-state")
		if ButtonStyled(label, attrs, look, style, focusRing) {
			key := GetFrameInput().Key
			// While open, Space/Enter belong to the item list (activate the
			// highlight). A pointer click on the trigger still toggles.
			if state.open && (key == KeyEnter || key == KeySpace) {
				// leave open
			} else {
				state.open = !state.open
				if state.open {
					// Fresh query/selection each open. Keep wantsFilter sticky so a
					// menu that already opted in still has its filter field on the
					// first frame (no one-frame lag before typing works).
					state.filterQuery = ""
					state.composing = false
					state.selected = -1
					state.lastFilterQuery = ""
					state.itemCount = 0
				}
			}
		}

		state.btnId = GetLastId()
		if attrs.Disabled {
			state.open = false
		}
		if !attrs.Disabled && !state.open && IdHasFocus(state.btnId) && GetFrameInput().Key == KeyDown {
			ShowFocusIndicator()
			state.open = true
			state.filterQuery = ""
			state.composing = false
			state.selected = 0
			state.lastFilterQuery = ""
			state.itemCount = 0
			GetFrameInput().Key = KeyCodeNone
		}

		if state.open && _menuItemPressed {
			_menuItemPressed = false
			state.open = false
			FocusImmediateOn(state.btnId)
			if _menuItemKeyboard {
				ShowFocusIndicator()
			}
			_menuItemKeyboard = false
		}

		if !state.open {
			state.filterQuery = ""
			state.composing = false
			state.selected = -1
			state.lastFilterQuery = ""
			state.itemCount = 0
			// wantsFilter stays true once this MenuButton has ever filtered.
		}

		if state.open {

			var _prevTrigger = _activePanelTrigger
			_activePanelTrigger = &state.open
			defer func() {
				_activePanelTrigger = _prevTrigger
			}()

			Popup(func() {
				// A long menu must never extend past the window: cap its
				// height to the viewport (small margin for the drop shadow)
				// and scroll the items inside. Short menus size intrinsically
				// as before — the cap only engages on overflow.
				maxH := GetHost().WindowSize[1] - 8
				ContainerWithKey("action-menu", Attrs(MinWidth(100), MaxWidth(600), MaxHeight(maxH),
					Corners(4), Pad2(6, 0), Gap(2), Clip, NoAnimate, BackgroundVec(CurrentColorScheme.Menu.Surface.Background), AmendTextStyle(TextColorVec(CurrentColorScheme.Menu.Surface.Text)), BorderWidth(1), BorderColorVec(CurrentColorScheme.Menu.Surface.Border), _popupShadow,
					TabAfter(state.btnId)), func() {
					ModAttrs(FloatVec(_getPositionRelativeTo(state.btnId)))
					state.menuId = CurrentId()

					// Filter field first when this open session has opted in
					// (sticky wantsFilter). First frame before any
					// MenuFilterQuery: no field — appears next frame.
					if state.wantsFilter {
						const fs f32 = 12
						pad := N4(fs / 2)
						// Show strip when there is committed text or IME preedit.
						// ProcessTextInput runs first so same-frame typing can show.
						show := state.filterQuery != "" || state.composing
						fieldAttrs := Attrs(Expand, Focusable, Clip, Corners(2))
						if show {
							fieldAttrs = Attrs(Expand, Focusable, Clip, Corners(2),
								PadVec(pad),
								BackgroundVec(CurrentColorScheme.TextInput.Background),
								BorderWidth(1), BorderColorVec(CurrentColorScheme.TextInput.Border),
								MinHeight(fs+pad[PAD_TOP]+pad[PAD_BOTTOM]))
						} else {
							// Invisible sink: zero height, not a tab stop.
							fieldAttrs = Attrs(Expand, Clip, MaxHeight(0), MinHeight(0))
						}
						ContainerWithKey("menu-filter", fieldAttrs, func() {
							// AutoFocus only runs on FirstRender; after the menu
							// has been opened once the filter identity often
							// still has prev data, so AutoFocus never re-fires
							// and typing/arrows do nothing. Steal focus every
							// frame while this filterable menu is open (same
							// idea as keeping the filter box focused in
							// FileSelector). Skip on Tab so prologue cycling
							// is not overwritten.
							if GetFrameInput().Key != KeyTab {
								Focus()
							}

							cfg := TextInputConfig{
								FontSize:          fs,
								Padding:           pad,
								MaxLines:          1,
								NoAutoFocus:       true, // Focus() above owns it
								NoUpDownLineEdges: true, // Up/Down navigate the list
							}.withDefaults()
							if !show {
								cfg.Padding = N4(0)
							}
							cfg = TextInputConfigWithStyle(cfg, CurrentColorScheme.TextInput)
							st := ProcessTextInput(&state.filterQuery, cfg)
							state.composing = st.Composing
							showNow := state.filterQuery != "" || st.Composing
							if showNow {
								DrawTextInputPlain(st, cfg)
							}
						})
						if state.filterQuery != "" || state.composing {
							Element(Attrs(Expand, FixHeight(1), BackgroundVec(CurrentColorScheme.Menu.Separator)))
						}

						// Reset keyboard selection when the query changes
						// (nothing selected until Down — avoids a forced first-item highlight).
						if state.filterQuery != state.lastFilterQuery {
							state.selected = -1
							state.lastFilterQuery = state.filterQuery
						}
					}

					ScrollOnInput()
					ScrollBars()

					itemCount := 0
					prev := _menuFilter
					_menuFilter = &menuFilterSession{
						query:     &state.filterQuery,
						wants:     &state.wantsFilter,
						selected:  &state.selected,
						itemCount: &itemCount,
						composing: state.composing,
					}
					fn()
					_menuFilter = prev
					state.itemCount = itemCount

					if itemCount == 0 {
						state.selected = -1
					} else if state.selected >= itemCount {
						state.selected = itemCount - 1
					} else if state.selected < -1 {
						state.selected = -1
					}

					// Skip list keys while IME is composing (Enter commits composition).
					if !state.composing {
						switch GetFrameInput().Key {
						case KeyDown:
							if itemCount > 0 {
								if state.selected < 0 {
									state.selected = 0
								} else if state.selected+1 < itemCount {
									state.selected++
								}
							}
							GetFrameInput().Key = KeyCodeNone
						case KeyUp:
							if state.selected > 0 {
								state.selected--
							} else if state.selected == 0 {
								state.selected = -1
							}
							GetFrameInput().Key = KeyCodeNone
						case KeyEscape:
							if state.filterQuery != "" || state.composing {
								state.filterQuery = ""
								state.composing = false
								state.selected = -1
								state.lastFilterQuery = ""
							} else {
								state.open = false
								FocusImmediateOn(state.btnId)
								ShowFocusIndicator()
							}
							GetFrameInput().Key = KeyCodeNone
						}
						// Enter/Space: handled in MenuItem while building (selected match).
					}
				})
			})
		}

		// do this after handling the open menu so that clicks inside the menu can still register!
		if !IdIsHovered(state.btnId) && !IdIsHovered(state.menuId) && GetFrameInput().Mouse == MouseClick { // click outside!
			state.open = false
		}
		if state.open && state.menuId != nil && FocusedId() != nil &&
			!IdHasFocus(state.btnId) && !IdHasFocusWithin(state.menuId) {
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
	if pos[0]+selfSize[0] > GetHost().WindowSize[0] {
		pos[0] = GetHost().WindowSize[0] - selfSize[0] - sp
	}
	if pos[1]+selfSize[1] > GetHost().WindowSize[1] {
		pos[1] = GetHost().WindowSize[1] - selfSize[1] - sp
	}

	pos[0] = max(0, pos[0])
	pos[1] = max(0, pos[1])

	return pos
}

// MenuSeparator draws a thin horizontal divider between menu items.
func MenuSeparator() { MenuSeparatorStyled(CurrentColorScheme.Menu.Separator) }

// MenuSeparatorStyled draws a separator with a literal color.
func MenuSeparatorStyled(color Vec4) {
	Container(Attrs(Expand, Pad2(4, 10)), func() {
		Element(Attrs(BackgroundVec(color), MinSize(1, 1), Expand))
	})
}

// MenuItem renders a clickable menu row with an optional leading icon (pass
// NoIcon for none) and returns true on the frame it is chosen.
func MenuItem(icon IconGlyph, label string) bool {
	return MenuItemExt(label, ButtonAttrs{Icon: icon})
}

// MenuItemExt is MenuItem configured by ButtonAttrs (icon, disabled state,
// accent). Interaction uses ProcessButtonEvents — same building block as
// custom buttons and ButtonExt, with menu-row chrome instead of the
// elevated face.
//
// Each MenuItem takes part in keyboard selection while the parent menu is
// open: the selected row is highlighted, and Enter or Space activates it
// (unless IME composition is active on the filter field). Menu rows are
// not tab stops — arrows move the selection; Tab dismisses the menu.
func MenuItemExt(label string, attrs ButtonAttrs) bool {
	style := CurrentColorScheme.Menu
	if attrs.Accent != (Vec4{}) {
		style.Hovered.Background = attrs.Accent
		style.Hovered.Text = ContrastingTextColor(attrs.Accent)
		style.Pressed = style.Hovered
	}
	return MenuItemStyled(label, attrs, style)
}

// MenuItemStyled supplies literal item states. Accent and Type are ignored.
func MenuItemStyled(label string, attrs ButtonAttrs, style MenuStyle) bool {
	var action bool
	var keyboardAction bool

	// Keyboard selection index (stable order of MenuItem calls this frame).
	itemIdx := -1
	kbSelected := false
	if _menuFilter != nil && _menuFilter.itemCount != nil {
		itemIdx = *_menuFilter.itemCount
		*_menuFilter.itemCount++
		if _menuFilter.selected != nil && *_menuFilter.selected == itemIdx {
			kbSelected = true
		}
		// Enter/Space activate the keyboard-selected row (previous-frame
		// selection index; Up/Down are applied after the item list each frame).
		if kbSelected && !_menuFilter.composing {
			switch GetFrameInput().Key {
			case KeyEnter, KeySpace:
				action = true
				keyboardAction = true
				GetFrameInput().Key = KeyCodeNone
			}
		}
	}

	Container(Attrs(Row, Expand, CrossAlign(AlignMiddle), BackgroundVec(style.Normal.Background), Pad2(4, 8), Gap(12)), func() {
		st := ProcessButtonEvents(attrs.Disabled)
		NextAccessRole("menuitem")
		NextAccessDisabled(attrs.Disabled)
		AssignAccess()
		// Rows are arrow-activated, not tab stops.
		ModAttrs(func(a *AttrSet) { a.Focusable = false })
		if st.Clicked {
			action = true
			keyboardAction = st.FocusVisible
		}

		paint := style.Normal
		switch {
		case attrs.Disabled:
			paint = style.Disabled
		case st.Active:
			paint = style.Pressed
		case st.Hovered || kbSelected:
			paint = style.Hovered
		}
		ModAttrs(BackgroundVec(paint.Background))
		textColor := paint.Text

		if attrs.Icon.Rune != 0 {
			Icon(attrs.Icon, TextColor(textColor[0], textColor[1], textColor[2], textColor[3]))
		}
		Label(label, FontSize(12), TextColor(textColor[0], textColor[1], textColor[2], textColor[3]))
	})
	if attrs.Disabled {
		action = false
	}
	if action {
		_menuItemPressed = true
		_menuItemKeyboard = keyboardAction
	}
	return action
}

// PopupPanel shows a floating panel, built by fn and styled by a, anchored to
// anchorId while *toggle is true. It closes (setting *toggle to false) when the
// user clicks outside it or presses Escape (unless fn already consumed the
// key). anchorId is typically the ContainerId of the control that toggles it.
func PopupPanel(toggle *bool, anchorId ContainerId, a AttrSet, fn func()) {
	PopupPanelStyled(toggle, anchorId, a, CurrentColorScheme.Menu.Surface, fn)
}

// PopupPanelStyled supplies literal popup surface colors; fn owns its child styling.
func PopupPanelStyled(toggle *bool, anchorId ContainerId, a AttrSet, style SurfaceColors, fn func()) {
	if *toggle {
		var _prevTrigger = _activePanelTrigger
		_activePanelTrigger = toggle
		defer func() {
			_activePanelTrigger = _prevTrigger
		}()
		var selfId ContainerId
		Popup(func() {
			Container(AttrsWith(a, BackgroundVec(style.Background), AmendTextStyle(TextColorVec(style.Text)), BorderWidth(1), BorderColorVec(style.Border), _popupShadow, Clip, NoAnimate, TabAfter(anchorId)), func() {
				ModAttrs(FloatVec(_getPositionRelativeTo(anchorId)))
				selfId = CurrentId()
				fn()
			})

			// After fn so inner content can consume Escape (e.g. a nested
			// field). Same idea as Modal.
			if GetFrameInput().Key == KeyEscape {
				*toggle = false
				FocusImmediateOn(anchorId)
				ShowFocusIndicator()
				GetFrameInput().Key = KeyCodeNone
			}

			// do this after handling the open menu so that clicks inside the
			// menu can still register, but inside the popup call so that the
			// selfid has been set

			if !IdIsHovered(anchorId) && !IdIsHovered(selfId) && GetFrameInput().Mouse == MouseClick { // click outside!
				*toggle = false
			}
			if *toggle && selfId != nil && FocusedId() != nil &&
				!IdHasFocus(anchorId) && !IdHasFocusWithin(selfId) {
				*toggle = false
			}
		})

	}
}
