package widgets

// checkboxes and radios
//
import (
	. "go.hasen.dev/slay"
	. "go.hasen.dev/slay/tw"
)

// OptionButton is also known as radio button
func CheckBoxExt[T comparable](target *T, label string, value T, selectedIcon rune, unselectedIcon rune) {
	Layout(TW(Row, Gap(6), CrossMid), func() {
		if PressAction() {
			*target = value
		}
		var icon = selectedIcon
		if *target != value {
			icon = unselectedIcon
		}
		Icon(icon, Sz(14), Clr(240, 50, 20, 1))
		Label(label, Sz(12), Clr(240, 50, 20, 1))
	})
}

func CheckBox(target *bool, label string) {
	CheckBoxExt(target, label, true, SymBoxTick, SymBox)
}

func OptionButton[T comparable](target *T, label string, value T) {
	CheckBoxExt(target, label, value, SymRadioOn, SymRadioOff)
}

// iOS style toggle switch
func ToggleSwitch(on *bool) {
	Layout(TW(Row, BG(0, 0, 80, 1), Pad(4), CA(AlignMiddle), BR(12), MinSize(40, 20), BW(1), Bo(0, 0, 20, 0.7)), func() {
		if IsClicked() {
			*on = !*on
		}

		if *on {
			ModAttrs(Grad(0, 0, 10, 0))
		}

		if *on {
			// spacer to push the switch to the right
			Element(TW(Grow(1)))
		} else {
			Nil()
		}

		// the inner button (round)
		Layout(TW(BR(8), MinSize(16, 16), BG(0, 0, 0, 0.5)), func() {
			if *on {
				ModAttrs(BG(240, 80, 70, 1), Grad(0, 0, -10, 0), Shd(3))
			}
		})
	})
}
