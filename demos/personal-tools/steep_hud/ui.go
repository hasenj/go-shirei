package main

import (
	"fmt"
	"time"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const (
	winWidth  = 368
	winHeight = 268
	ringSize  = 82
)

// RootView renders the translucent floating timer overlay.
func RootView() {
	handleShortcuts()

	preset := state.currentPreset()
	cat := preset.Category
	accent := cat.AccentHSLA()

	// Root viewport stays transparent over the desktop unless -backdrop is enabled.
	rootAttrs := Attrs(Viewport, Pad(12))
	if state.Backdrop {
		rootAttrs = Attrs(Viewport, Pad(12), Background(222, 28, 22, 1), Grad(16, -6, -8, 0))
	}

	Container(rootAttrs, func() {
		// Subtle accent border glow when finished or chiming.
		borderCol := Vec4{215, 22, 88, 0.17}
		if state.Status == StatusDone || state.ChimeFlash > 0 {
			glow := float32(0.55)
			if state.Status != StatusDone {
				glow = 0.17 + 0.45*state.ChimeFlash
			}
			borderCol = Vec4{accent[0], accent[1], min(76, accent[2]+10), glow}
		}

		// Translucent frosted obsidian glass card.
		Container(Attrs(
			Grow(1), Expand,
			Pad4(12, 14, 13, 14),
			Gap(8),
			Corners(18),
			BorderWidth(1),
			BorderColorVec(borderCol),
			Background(224, 18, 10, state.Opacity),
			Grad(2, -4, 4, 0),
			BoxShadow(10),
			AmendTextStyle(TextColor(215, 12, 94, 1)),
		), func() {
			renderHeader(preset, cat, accent)
			renderHero(preset, cat, accent)

			// Soft hairline separator.
			Element(Attrs(Expand, FixHeight(1), Background(215, 16, 86, 0.09), NoAnimate))

			renderCategoryTabs()
			renderPresetGrid()
		})
	})
}

func renderHeader(preset Preset, cat Category, accent Vec4) {
	Container(Attrs(Row, Expand, CrossMid, Gap(6)), func() {
		// Status indicator dot.
		dotCol := Vec4{215, 10, 48, 0.75}
		switch state.Status {
		case StatusRunning:
			dotCol = accent
		case StatusPaused:
			dotCol = Vec4{40, 82, 60, 0.95}
		case StatusDone:
			dotCol = Vec4{accent[0], min(95, accent[1]+12), min(82, accent[2]+14), 1}
		}
		Element(Attrs(FixSize(7, 7), Corners(3.5), BackgroundVec(dotCol)))

		Label("STEEP", FontSize(10), FontWeight(WeightSemibold), TextColor(215, 14, 62, 1))

		// Compact status badge pill.
		badgeBg := Vec4{220, 14, 22, 0.65}
		badgeFg := Vec4{215, 12, 74, 1}
		if state.Status == StatusRunning || state.Status == StatusDone {
			badgeBg = cat.SoftTintHSLA(0.55)
			badgeFg = Vec4{accent[0], min(90, accent[1]+10), min(86, accent[2]+20), 1}
		} else if state.Status == StatusPaused {
			badgeBg = Vec4{40, 46, 22, 0.55}
			badgeFg = Vec4{42, 85, 76, 1}
		}
		Container(Attrs(Pad2(2, 7), Corners(9), BackgroundVec(badgeBg)), func() {
			Label(state.Status.StatusText(cat), FontSize(9.5), FontWeight(WeightMedium), TextColorVec(badgeFg))
		})

		Filler(1)

		// Translucency cycle chip (90% / 78% / 64%).
		if hudMiniChip("opacity", "Cycle glass opacity", fmt.Sprintf("%.0f%%", state.Opacity*100), NoIcon, false) {
			state.CycleOpacity()
		}

		// Preview soft chime button.
		if hudIconBtn("preview_chime", "Preview soft chime", TypBell, state.ChimeFlash > 0.05, accent) {
			triggerChime(cat, true)
		}

		// Mute / unmute chime toggle.
		volIcon := TypVolumeUp
		volLabel := "Mute chime"
		if state.Muted {
			volIcon = TypVolumeMute
			volLabel = "Unmute chime"
		}
		if hudIconBtn("mute", volLabel, volIcon, state.Muted, Vec4{36, 76, 62, 1}) {
			state.Muted = !state.Muted
		}

		// Close overlay button.
		if hudIconBtn("close", "Close Steep HUD", SymICross, false, Vec4{4, 72, 62, 1}) {
			app.Quit()
		}
	})
}

func renderHero(preset Preset, cat Category, accent Vec4) {
	Container(Attrs(Row, Expand, CrossMid, Gap(14)), func() {
		// Left: High-DPI anti-aliased radial steeping ring with centered icon.
		frac := state.ElapsedFraction()
		ringID := getRingImage(ringSize, frac, cat, state.Status, state.ChimeFlash)

		Container(Attrs(FixSize(ringSize, ringSize), Center, NoAnimate), func() {
			Container(Attrs(Float(0, 0), FixSize(ringSize, ringSize), Behind, ClickThrough, NoAnimate), func() {
				ImageViewAt(ringID, Vec2{ringSize, ringSize})
			})
			centerIcon := cat.Icon()
			iconCol := Vec4{accent[0], min(88, accent[1]+6), min(84, accent[2]+18), 0.95}
			if state.Status == StatusDone {
				centerIcon = TypTick
				iconCol = Vec4{accent[0], min(95, accent[1]+10), min(90, accent[2]+22), 1}
			} else if state.Status == StatusPaused {
				iconCol = Vec4{215, 12, 72, 0.85}
			}
			Icon(centerIcon, FontSize(24), TextColorVec(iconCol))
		})

		// Right: Countdown clock, preset note, and transport controls.
		Container(Attrs(Grow(1), Gap(5)), func() {
			// Primary countdown digits + total target reference.
			Container(Attrs(Row, Expand, CrossMid, Gap(6)), func() {
				clockCol := Vec4{0, 0, 97, 1}
				if state.Status == StatusDone {
					clockCol = Vec4{accent[0], min(90, accent[1]+8), min(86, accent[2]+20), 1}
				} else if state.Status == StatusPaused {
					clockCol = Vec4{215, 10, 82, 1}
				}
				Label(FormatClock(state.Remaining), FontSize(33), FontWeight(WeightSemibold), TextColorVec(clockCol))
				Filler(1)
				Container(Attrs(Pad4(12, 0, 0, 0)), func() {
					Label("/ "+FormatShortDuration(state.Total), FontSize(11), TextColor(215, 12, 56, 1))
				})
			})

			// Brew / rest note line.
			Label(fmt.Sprintf("%s · %s", preset.Name, preset.Note), FontSize(10.5), TextColor(216, 12, 68, 1))

			// Transport & quick time nudge controls.
			Container(Attrs(Row, Expand, CrossMid, Gap(5)), func() {
				primaryIcon := SymPlay
				primaryLabel := "Start"
				switch state.Status {
				case StatusRunning:
					primaryIcon = SymPause
					primaryLabel = "Pause"
				case StatusPaused:
					primaryIcon = SymPlay
					primaryLabel = "Resume"
				case StatusDone:
					primaryIcon = SymReplay
					primaryLabel = "Again"
				}
				if hudPrimaryBtn("toggle_timer", primaryLabel, primaryIcon, accent) {
					state.ToggleStartPause(time.Now())
				}

				if hudSquareBtn("reset_timer", "Reset timer", SymReplay) {
					state.Reset()
				}

				Filler(1)

				if hudNudgeBtn("sub_15s", "Subtract 15 seconds", "-15s") {
					state.AdjustDuration(-15 * time.Second)
				}
				if hudNudgeBtn("add_15s", "Add 15 seconds", "+15s") {
					state.AdjustDuration(15 * time.Second)
				}
			})
		})
	})
}

func renderCategoryTabs() {
	Container(Attrs(Row, Expand, Gap(5)), func() {
		for _, cat := range allCategories {
			selected := state.ActiveCat == cat
			catAccent := cat.AccentHSLA()
			if hudCategoryTab(cat, selected, catAccent) {
				state.SelectCategory(cat)
			}
		}
	})
}

func renderPresetGrid() {
	indices := state.presetsForCategory(state.ActiveCat)
	catAccent := state.ActiveCat.AccentHSLA()

	Container(Attrs(Expand, Gap(5)), func() {
		for rowStart := 0; rowStart < len(indices); rowStart += 2 {
			Container(Attrs(Row, Expand, Gap(5)), func() {
				for col := 0; col < 2; col++ {
					pos := rowStart + col
					if pos >= len(indices) {
						Filler(1)
						continue
					}
					idx := indices[pos]
					p := state.Presets[idx]
					active := state.ActivePreset == idx
					if hudPresetChip(p, active, catAccent) {
						state.StartPreset(idx, time.Now())
					}
				}
			})
		}
	})
}

func hudPrimaryBtn(accessName, label string, icon IconGlyph, accent Vec4) bool {
	var clicked bool
	Container(Attrs(
		FixHeight(26),
		Pad2(0, 10),
		Corners(8),
		BorderWidth(1),
		Row, CrossMid, Center, Gap(5),
	), func() {
		st := ProcessButtonEvents(false)
		NextAccessName(accessName)
		NextAccessRole("button")
		NextAccessLabel(label)
		AssignAccess()
		clicked = st.Clicked

		bg := Vec4{accent[0], accent[1], max(36, accent[2]-11), 1}
		border := Vec4{accent[0], accent[1], min(80, accent[2]+8), 0.72}
		switch {
		case st.Active:
			bg[2] = max(26, bg[2]-7)
		case st.Hovered:
			bg[2] = min(62, bg[2]+6)
		}
		if st.FocusVisible {
			border = CurrentColorScheme.FocusRing
		}
		ModAttrs(BackgroundVec(bg), BorderColorVec(border))

		fg := Vec4{0, 0, 98, 1}
		Icon(icon, FontSize(11), TextColorVec(fg))
		Label(label, FontSize(11.5), FontWeight(WeightSemibold), TextColorVec(fg))
	})
	return clicked
}

func hudSquareBtn(accessName, accessLabel string, icon IconGlyph) bool {
	var clicked bool
	Container(Attrs(FixSize(26, 26), Corners(8), BorderWidth(1), Center), func() {
		st := ProcessButtonEvents(false)
		NextAccessName(accessName)
		NextAccessRole("button")
		NextAccessLabel(accessLabel)
		AssignAccess()
		clicked = st.Clicked

		bg := Vec4{220, 14, 22, 0.55}
		border := Vec4{215, 16, 84, 0.14}
		fg := Vec4{215, 12, 82, 1}
		switch {
		case st.Active:
			bg = Vec4{220, 14, 18, 0.85}
		case st.Hovered:
			bg = Vec4{220, 14, 28, 0.78}
			fg = Vec4{0, 0, 98, 1}
		}
		if st.FocusVisible {
			border = CurrentColorScheme.FocusRing
		}
		ModAttrs(BackgroundVec(bg), BorderColorVec(border))
		Icon(icon, FontSize(12), TextColorVec(fg))
	})
	return clicked
}

func hudNudgeBtn(accessName, accessLabel, text string) bool {
	var clicked bool
	Container(Attrs(FixHeight(26), Pad2(0, 7), Corners(8), BorderWidth(1), Center), func() {
		st := ProcessButtonEvents(false)
		NextAccessName(accessName)
		NextAccessRole("button")
		NextAccessLabel(accessLabel)
		AssignAccess()
		clicked = st.Clicked

		bg := Vec4{220, 14, 20, 0.48}
		border := Vec4{215, 16, 84, 0.12}
		fg := Vec4{215, 12, 76, 1}
		switch {
		case st.Active:
			bg = Vec4{220, 14, 16, 0.80}
		case st.Hovered:
			bg = Vec4{220, 14, 27, 0.72}
			fg = Vec4{0, 0, 96, 1}
		}
		if st.FocusVisible {
			border = CurrentColorScheme.FocusRing
		}
		ModAttrs(BackgroundVec(bg), BorderColorVec(border))
		Label(text, FontSize(10.5), FontWeight(WeightMedium), TextColorVec(fg))
	})
	return clicked
}

func hudIconBtn(accessName, accessLabel string, icon IconGlyph, highlighted bool, hiCol Vec4) bool {
	var clicked bool
	Container(Attrs(FixSize(22, 22), Corners(7), BorderWidth(1), Center), func() {
		st := ProcessButtonEvents(false)
		NextAccessName(accessName)
		NextAccessRole("button")
		NextAccessLabel(accessLabel)
		AssignAccess()
		clicked = st.Clicked

		bg := Vec4{220, 14, 22, 0.35}
		border := Vec4{215, 16, 84, 0.08}
		fg := Vec4{215, 12, 72, 1}
		if highlighted {
			bg = Vec4{hiCol[0], hiCol[1] * 0.65, 22, 0.60}
			border = Vec4{hiCol[0], hiCol[1], hiCol[2], 0.45}
			fg = Vec4{hiCol[0], min(92, hiCol[1]+8), min(84, hiCol[2]+16), 1}
		}
		switch {
		case st.Active:
			bg[3] = min(1, bg[3]+0.35)
		case st.Hovered:
			bg = Vec4{220, 14, 28, 0.70}
			fg = Vec4{0, 0, 96, 1}
		}
		if st.FocusVisible {
			border = CurrentColorScheme.FocusRing
		}
		ModAttrs(BackgroundVec(bg), BorderColorVec(border))
		Icon(icon, FontSize(12), TextColorVec(fg))
	})
	return clicked
}

func hudMiniChip(accessName, accessLabel, text string, icon IconGlyph, active bool) bool {
	var clicked bool
	Container(Attrs(FixHeight(22), Pad2(0, 6), Corners(7), BorderWidth(1), Row, CrossMid, Center, Gap(3)), func() {
		st := ProcessButtonEvents(false)
		NextAccessName(accessName)
		NextAccessRole("button")
		NextAccessLabel(accessLabel)
		AssignAccess()
		clicked = st.Clicked

		bg := Vec4{220, 14, 22, 0.35}
		border := Vec4{215, 16, 84, 0.09}
		fg := Vec4{215, 12, 68, 1}
		if active {
			bg = Vec4{220, 14, 28, 0.65}
			fg = Vec4{0, 0, 94, 1}
		}
		switch {
		case st.Active:
			bg = Vec4{220, 14, 18, 0.80}
		case st.Hovered:
			bg = Vec4{220, 14, 28, 0.68}
			fg = Vec4{0, 0, 95, 1}
		}
		if st.FocusVisible {
			border = CurrentColorScheme.FocusRing
		}
		ModAttrs(BackgroundVec(bg), BorderColorVec(border))
		if icon != NoIcon {
			Icon(icon, FontSize(10), TextColorVec(fg))
		}
		Label(text, FontSize(9.5), FontWeight(WeightMedium), TextColorVec(fg))
	})
	return clicked
}

func hudCategoryTab(cat Category, selected bool, accent Vec4) bool {
	var clicked bool
	Container(Attrs(Grow(1), FixHeight(26), Corners(8), BorderWidth(1), Row, CrossMid, Center, Gap(5)), func() {
		st := ProcessButtonEvents(false)
		NextAccessName("tab_" + cat.Label())
		NextAccessRole("tab")
		NextAccessLabel(cat.Label() + " presets")
		AssignAccess()
		clicked = st.Clicked

		bg := Vec4{220, 14, 18, 0.42}
		border := Vec4{215, 16, 84, 0.08}
		fg := Vec4{215, 12, 68, 1}
		iconCol := Vec4{215, 12, 64, 1}

		if selected {
			bg = cat.SoftTintHSLA(0.58)
			border = Vec4{accent[0], accent[1], accent[2], 0.55}
			fg = Vec4{0, 0, 97, 1}
			iconCol = Vec4{accent[0], min(92, accent[1]+10), min(84, accent[2]+18), 1}
		}
		switch {
		case st.Active:
			bg[3] = min(1, bg[3]+0.22)
		case st.Hovered && !selected:
			bg = Vec4{220, 14, 24, 0.65}
			fg = Vec4{0, 0, 92, 1}
			iconCol = Vec4{accent[0], accent[1] * 0.85, accent[2] + 10, 0.9}
		}
		if st.FocusVisible {
			border = CurrentColorScheme.FocusRing
		}
		ModAttrs(BackgroundVec(bg), BorderColorVec(border))
		Icon(cat.Icon(), FontSize(12), TextColorVec(iconCol))
		weight := WeightNormal
		if selected {
			weight = WeightSemibold
		}
		Label(cat.Label(), FontSize(11), FontWeight(weight), TextColorVec(fg))
	})
	return clicked
}

func hudPresetChip(p Preset, active bool, accent Vec4) bool {
	var clicked bool
	ContainerWithKey(p.ID, Attrs(
		Grow(1), FixHeight(27),
		Pad2(0, 9),
		Corners(8),
		BorderWidth(1),
		Row, CrossMid, Gap(4),
	), func() {
		st := ProcessButtonEvents(false)
		NextAccessName("preset_" + p.ID)
		NextAccessRole("button")
		NextAccessLabel(fmt.Sprintf("%s %s", p.Name, FormatShortDuration(p.Duration)))
		AssignAccess()
		clicked = st.Clicked

		bg := Vec4{220, 14, 18, 0.46}
		border := Vec4{215, 16, 84, 0.10}
		nameCol := Vec4{215, 12, 86, 1}
		durCol := Vec4{215, 12, 60, 1}

		if active {
			bg = p.Category.SoftTintHSLA(0.44)
			border = Vec4{accent[0], accent[1], accent[2], 0.62}
			nameCol = Vec4{0, 0, 98, 1}
			durCol = Vec4{accent[0], min(90, accent[1]+8), min(84, accent[2]+18), 1}
		}
		switch {
		case st.Active:
			bg[3] = min(1, bg[3]+0.24)
		case st.Hovered && !active:
			bg = Vec4{220, 14, 24, 0.72}
			border = Vec4{215, 16, 86, 0.22}
			nameCol = Vec4{0, 0, 97, 1}
			durCol = Vec4{215, 14, 76, 1}
		}
		if st.FocusVisible {
			border = CurrentColorScheme.FocusRing
		}
		ModAttrs(BackgroundVec(bg), BorderColorVec(border))

		weight := WeightNormal
		if active {
			weight = WeightSemibold
		}
		Label(p.Name, FontSize(11), FontWeight(weight), TextColorVec(nameCol))
		Filler(1)
		Label(FormatShortDuration(p.Duration), FontSize(10.5), FontWeight(WeightMedium), TextColorVec(durCol))
	})
	return clicked
}

func handleShortcuts() {
	fi := GetFrameInput()
	if fi.Key == KeyCodeNone {
		return
	}
	if ActiveCombo() == Combo(KeyQ, PrimaryMod()) {
		app.Quit()
		return
	}
	// Let focused buttons own Space/Enter; handle Space globally only when unfocused.
	if fi.Key == KeySpace && FocusedId() == nil {
		state.ToggleStartPause(time.Now())
		return
	}
	switch fi.Key {
	case KeyR:
		state.Reset()
	case KeyM:
		state.Muted = !state.Muted
	case KeyT:
		state.SelectCategory(CatTea)
	case KeyC:
		state.SelectCategory(CatCoffee)
	case KeyE:
		state.SelectCategory(CatEyes)
	case KeyUp:
		state.AdjustDuration(15 * time.Second)
	case KeyDown:
		state.AdjustDuration(-15 * time.Second)
	case KeyEscape:
		if state.Status == StatusDone {
			state.Reset()
		} else if state.Status == StatusRunning {
			state.ToggleStartPause(time.Now())
		}
	case Key1, Key2, Key3, Key4:
		slot := int(fi.Key - Key1)
		indices := state.presetsForCategory(state.ActiveCat)
		if slot >= 0 && slot < len(indices) {
			state.StartPreset(indices[slot], time.Now())
		}
	}
}
