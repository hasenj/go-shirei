package shirei

import g "go.hasen.dev/generic"

// popups holds render callbacks deferred to the end of the frame, so they
// paint on top of everything else — as children of the root container (or, under
// client-side decorations, the content area). Menus, tooltips, dropdowns and
// modals all register here.
//
// A popup is not special: once PopupsHost runs its callback, the containers it
// builds are ordinary containers. Layout, hover, and the settle/second-pass
// mechanism all apply to them without any special casing, precisely because the
// queue is drained inside the frame build (see RunFrameFn's pass loop) and
// re-populated on every pass.
var popups = make([]func(), 0, 128)

// Popup registers fn to render at the end of the current frame, on top of the
// rest of the UI. Call it from anywhere while building the frame; it renders
// where the frame loop drains the queue, not where Popup is called.
func Popup(fn func()) {
	popups = append(popups, fn)
}

// PopupsHost runs the queued popup callbacks in order, then clears the queue.
// The frame loop calls this automatically after the app's frame function, in
// the same container scope the frame ran in — applications do not call it.
func PopupsHost() {
	for _, p := range popups {
		p()
	}
	g.ResetSlice(&popups)
}
