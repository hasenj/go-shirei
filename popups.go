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
func Popup(fn func()) {
	ui.popups = append(ui.popups, fn)
}

// PopupsHost runs the queued popup callbacks in order, then clears the queue.
// The frame loop calls this automatically after the app's frame function, in
// the same container scope the frame ran in — applications do not call it.
func PopupsHost() {
	for _, p := range ui.popups {
		p()
	}
	g.ResetSlice(&ui.popups)
}
