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
// While a popup callback runs, outermost floating containers with unset Z
// (0) pick up ui.popupZ so later drains paint above earlier ones. Nested
// floats under an already-floating ancestor keep Z=0 so decoration fills
// still paint under labels (menu hover, text selection, …).
func Popup(fn func()) {
	ui.popups = append(ui.popups, fn)
}

// framePopupSources run at the start of every PopupsHost drain. They may call
// Popup to enqueue UI for that frame. Used by package-level retained chrome
// (e.g. widgets.Toast) so apps need not re-arm it each frame.
var framePopupSources []func()

// RegisterFramePopup adds fn to run at the start of every PopupsHost drain.
// Typical use: retain a message or flag in package state, and from fn call
// Popup while that state is live.
func RegisterFramePopup(fn func()) {
	framePopupSources = append(framePopupSources, fn)
}

// PopupsHost drains the popup queue until empty. The frame loop calls this
// automatically after the app's frame function, in the same container scope
// the frame ran in — applications do not call it.
//
// Frame popup sources run first (they may append to the queue). Then an index
// loop drains until empty so popups appended during a callback still run.
// ui.popupZ is that index (1-based) for the duration of each callback.
func PopupsHost() {
	for _, fn := range framePopupSources {
		fn()
	}
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

// applyPopupZ stamps ui.popupZ onto an outermost floating container that left
// Z at 0. Nested floats inside an already-floating ancestor are left alone —
// their Z is sibling-local (menu highlight under text, selection carets, …).
//
// Ancestor walk (not a build-depth counter) is required because menus often
// set Float via ModAttrs after Container opens; a depth counter would miss
// that and still stamp interior decoration floats.
//
// Used when Float is set via Attrs or via ModAttrs (menus/panels). Call with
// ui.current still pointing at the *parent* (ContainerWithKey) or at the
// container being modified (ModAttrs). When a is the current container's
// AttrSet, the walk starts at the parent so the candidate's own Float does
// not look like an already-floating ancestor.
func applyPopupZ(a *AttrSet) {
	if ui.popupZ == 0 || !a.Floats || a.Z != 0 {
		return
	}
	p := ui.current
	if p != nil && &p.AttrSet == a {
		p = p.parent
	}
	for ; p != nil; p = p.parent {
		if p.Floats {
			return
		}
	}
	a.Z = ui.popupZ
}
