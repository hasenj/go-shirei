package widgets

import (
	. "go.hasen.dev/shirei"
)

// Modal renders fn as a centered card over a dimmed scrim that blocks
// the UI behind it, drawn on top of everything via the popup layer (the
// app's root must host popups — see PopupsHost). dismiss wires the
// universal close gestures: Escape, and a click on the scrim (outside the
// card). Pass nil for a modal that must be answered through its own
// buttons (e.g. a conflict that has no neutral outcome). Anything beyond
// that — Enter-to-submit, multiple choices — belongs in fn, next to the
// buttons it duplicates.
func Modal(width f32, dismiss func(), fn func()) {
	Popup(func() {
		var cardId ContainerId
		var cardFirst bool
		Container(Attrs(Float(0, 0), FixWidth(WindowSize[0]), FixHeight(WindowSize[1]), FocusTrap, Center, Background(220, 25, 12, 0.45), NoAnimate, InFront), func() {
			Container(Attrs(FixWidth(width), Gap(10), Pad(20), Background(0, 0, 100, 1), Corners(10), BoxShadow(24)), func() {
				cardId = CurrentId()
				// Hover is last-frame geometry; a brand-new card is never
				// "hovered" on the open frame, so the opening click would
				// look like a scrim click without this guard.
				cardFirst = FirstRender()
				fn()
			})
			// Escape after content so fn can consume it (e.g. clear a
			// list selection) by zeroing FrameInput.Key.
			if dismiss != nil && FrameInput.Key == KeyEscape {
				dismiss()
			}
			// After the card is laid out so IdIsHovered(cardId) is meaningful.
			// Skip the first frame so open-on-click / double-click callers
			// are not dismissed by the same MouseClick that opened them.
			if dismiss != nil && !cardFirst && FrameInput.Mouse == MouseClick && !IdIsHovered(cardId) {
				dismiss()
			}
		})
	})
}
