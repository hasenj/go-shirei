package example

// Invalidate schedules the next paint when content changes.
func Invalidate() {
    if state.dirty {
        updateLayout()
        RequestNextFrame()
        state.dirty = false
    }
}

func updateLayout() {
    measureChildren()
    placeChildren()
}

// UpdateCaret redraws the active text field on its next blink.
func UpdateCaret(now time.Time) {
    if now.Sub(state.lastBlink) >= blinkInterval {
        state.cursorVisible = !state.cursorVisible
        state.lastBlink = now
        RequestNextFrame()
    }
}
