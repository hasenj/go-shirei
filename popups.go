package shirei

import g "go.hasen.dev/generic"

// Popup registers fn to render at the end of the current frame, on top of the
// rest of the UI. Call it from anywhere while building the frame; it renders
// where the frame loop drains the queue (ui.popups), not where Popup is called.
//
// A popup is not special: once PopupsHost runs its callback, the containers it
// builds are ordinary containers. Layout, hover, and the settle/second-pass
// mechanism all apply without special casing, because the queue is drained
// inside the frame build and re-populated on every pass.
//
// While a popup callback runs, floating containers with unset Z (0) pick up
// ui.popupZ so later drains paint above earlier ones.
func Popup(fn func()) {
	ui.popups = append(ui.popups, fn)
}

// PopupsHost drains the popup queue until empty. The frame loop calls this
// automatically after the app's frame function, in the same container scope
// the frame ran in — applications do not call it.
//
// Index loop (not range): len is re-checked each step so popups appended
// during a callback still run. ui.popupZ is that index (1-based) for the
// duration of each callback.
func PopupsHost() {
	for i := 0; i < len(ui.popups); i++ {
		ui.popupZ = f32(i + 1)
		ui.popups[i]()
	}
	ui.popupZ = 0
	g.ResetSlice(&ui.popups)
}

// Modal renders fn as a centered card over a dimmed scrim that blocks
// the UI behind it, drawn on top of everything via the popup layer.
// dismiss wires the universal close gestures: Escape, and a click on the
// scrim (outside the card). Pass nil for a modal that must be answered
// through its own buttons (e.g. a conflict that has no neutral outcome).
// Anything beyond that — Enter-to-submit, multiple choices — belongs in fn,
// next to the buttons it duplicates.
//
// Modal is immediate: call it every frame while the dialog should stay open
// (typically `if open { Modal(...) }`).
func Modal(width f32, dismiss func(), fn func()) {
	Popup(func() {
		var cardId ContainerId
		var cardFirst bool
		Container(Attrs(Float(0, 0), FixSizeVec(GetHost().WindowSize), FocusTrap, Center, Background(220, 25, 12, 0.45), NoAnimate), func() {
			Container(Attrs(FixWidth(width), Gap(10), Pad(20), Background(0, 0, 100, 1), Corners(10), BoxShadow(24)), func() {
				cardId = CurrentId()
				// Hover is last-frame geometry; a brand-new card is never
				// "hovered" on the open frame, so the opening click would
				// look like a scrim click without this guard.
				cardFirst = FirstRender()
				fn()
			})
			// Escape after content so fn can consume it (e.g. clear a
			// list selection) by zeroing GetFrameInput().Key.
			if dismiss != nil && GetFrameInput().Key == KeyEscape {
				dismiss()
			}
			// After the card is laid out so IdIsHovered(cardId) is meaningful.
			// Skip the first frame so open-on-click / double-click callers
			// are not dismissed by the same MouseClick that opened them.
			if dismiss != nil && !cardFirst && GetFrameInput().Mouse == MouseClick && !IdIsHovered(cardId) {
				dismiss()
			}
		})
	})
}

// applyPopupZ stamps ui.popupZ onto a floating container that left Z at 0.
// Used when Float is set via Attrs or via ModAttrs (menus/panels).
func applyPopupZ(a *AttrSet) {
	if ui.popupZ != 0 && a.Floats && a.Z == 0 {
		a.Z = ui.popupZ
	}
}
