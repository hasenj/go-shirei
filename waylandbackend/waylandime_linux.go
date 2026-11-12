//go:build linux

package waylandbackend

import (
	"unicode/utf8"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/wayland/textinput"
)

// Wayland IME via zwp_text_input_v3. Core contract matches Cocoa/Win32:
// composition is display-only (InputState.Composition / CompositionSel);
// committed text enters through FrameInput.Text via a pending buffer.
//
// Hard requirements shared with Cocoa/Win32: accumulate committed text
// (never assign over earlier text); clear composition on end/leave;
// refresh candidate geometry after frames.
//
// Done-event rule (pragmatic): text-input-v3 double-buffers preedit/commit
// and resets them to empty on every done. Compositor acks of our own
// set_cursor_rectangle commits often arrive as done with NO preedit_string
// / commit_string in the batch. Strictly applying "empty" would wipe the
// inline preedit every frame. We only change composition/commit state when
// the batch actually carried a preedit_string or commit_string event (GTK
// / several clients do the same). An intentional clear is preedit_string
// with empty text, which still sets the gotPreedit flag.

var (
	textInputMgr *textinput.Manager
	textInput    *textinput.TextInput

	textInputSerial uint32 // commits on the shared seat text-input object

)

// bindTextInputManager binds zwp_text_input_manager_v3 from the registry.
func bindTextInputManager(name, version uint32) {
	mgr, err := textinput.BindManager(registry, name, version)
	if err != nil {
		return
	}
	textInputMgr = mgr
	ensureTextInput()
}

// ensureTextInput creates the per-seat text-input object once both the manager
// and the seat exist.
func ensureTextInput() {
	if textInput != nil || textInputMgr == nil || seat == nil {
		return
	}
	ti, err := textInputMgr.GetTextInput(seat)
	if err != nil {
		return
	}
	textInput = ti
	textInput.AddListener(h)
	wlDebug("text-input-v3 bound")
}

// --- text-input events ------------------------------------------------------

func (*handler) HandleTextInputEnter(ev textinput.EnterEvent) {
	w := windowsBySurface[ev.Surface]
	if w == nil || w.closed {
		return
	}
	imeWindow = w
	leave := enterWindow(w)
	defer leave()

	// Focus follows the keyboard; enable for the whole surface (shirei is one
	// text-input client, not per-widget contexts).
	_ = ev
	currentWindow.textInputOnSurface = true
	enableTextInput()
	wlDebug("text-input enter")
}

func (*handler) HandleTextInputLeave(ev textinput.LeaveEvent) {
	w := windowsBySurface[ev.Surface]
	if w == nil || w.closed {
		return
	}
	if imeWindow == w {
		imeWindow = nil
	}
	leave := enterWindow(w)
	defer leave()

	_ = ev
	currentWindow.textInputOnSurface = false
	currentWindow.textInputEnabled = false
	currentWindow.haveLastCursor = false
	resetPendingIME()
	if shirei.GetInputState().Composition != "" {
		clearComposition()
		currentWindow.dirty = true
	}
	wlDebug("text-input leave")
}

func (*handler) HandleTextInputPreeditString(ev textinput.PreeditStringEvent) {
	w := imeWindow
	if w == nil || w.closed {
		return
	}
	leave := enterWindow(w)
	defer leave()

	// Double-buffered: stash until done. Null text arrives as "".
	currentWindow.pendingPreedit = ev.Text
	currentWindow.pendingPreeditBegin = ev.CursorBegin
	currentWindow.pendingPreeditEnd = ev.CursorEnd
	currentWindow.gotPreedit = true
	wlDebug("text-input preedit=%q cursor=%d..%d",
		truncateForLog(ev.Text), ev.CursorBegin, ev.CursorEnd)
}

func (*handler) HandleTextInputCommitString(ev textinput.CommitStringEvent) {
	w := imeWindow
	if w == nil || w.closed {
		return
	}
	leave := enterWindow(w)
	defer leave()

	currentWindow.pendingCommit = ev.Text
	currentWindow.gotCommit = true
	wlDebug("text-input commit_string=%q", truncateForLog(ev.Text))
}

func (*handler) HandleTextInputDeleteSurroundingText(ev textinput.DeleteSurroundingTextEvent) {
	w := imeWindow
	if w == nil || w.closed {
		return
	}
	leave := enterWindow(w)
	defer leave()

	// v1: no surrounding-text / delete channel in core. Japanese IMEs rarely
	// need this for basic composition; reconversion is deferred.
	_ = ev
}

func (*handler) HandleTextInputDone(ev textinput.DoneEvent) {
	w := imeWindow
	if w == nil || w.closed {
		return
	}
	leave := enterWindow(w)
	defer leave()

	// Apply preedit/commit only when this done batch carried those events.
	// A bare done (typical ack of our set_cursor_rectangle commit) must not
	// wipe the active preedit — that was the frame-storm clear bug.
	_ = ev

	commit := currentWindow.pendingCommit
	preedit := currentWindow.pendingPreedit
	begin, end := currentWindow.pendingPreeditBegin, currentWindow.pendingPreeditEnd
	hadPreedit, hadCommit := currentWindow.gotPreedit, currentWindow.gotCommit
	resetPendingIME()

	changed := false
	if hadCommit {
		if commit != "" {
			appendPendingText(commit)
			changed = true
		}
		// A commit with no new preedit ends composition.
		if !hadPreedit {
			if shirei.GetInputState().Composition != "" {
				clearComposition()
				changed = true
			}
		}
	}
	if hadPreedit {
		prev := shirei.GetInputState().Composition
		prevSel := shirei.GetInputState().CompositionSel
		setCompositionUTF8(preedit, begin, end)
		if shirei.GetInputState().Composition != prev || shirei.GetInputState().CompositionSel != prevSel {
			changed = true
		}
	}

	if changed {
		currentWindow.dirty = true
		// Fresh geometry for the candidate window after preedit/commit changes.
		// Do this once here rather than every frame (avoids the commit↔done loop).
		currentWindow.haveLastCursor = false // force resend
	}
	wlDebug("text-input done serial=%d hadPreedit=%v hadCommit=%v commit=%q preedit=%q comp=%q",
		ev.Serial, hadPreedit, hadCommit,
		truncateForLog(commit), truncateForLog(preedit),
		truncateForLog(shirei.GetInputState().Composition))
}

// --- enable / geometry ------------------------------------------------------

func enableTextInput() {
	if textInput == nil || !currentWindow.textInputOnSurface || currentWindow.textInputEnabled {
		return
	}
	if err := textInput.Enable(); err != nil {
		return
	}
	_ = textInput.SetContentType(textinput.ContentHintNone, textinput.ContentPurposeNormal)
	currentWindow.haveLastCursor = false
	updateTextInputCursorRectangle()
	if err := textInput.Commit(); err != nil {
		return
	}
	textInputSerial++
	currentWindow.textInputEnabled = true
}

func disableTextInput() {
	if textInput == nil || !currentWindow.textInputEnabled {
		return
	}
	_ = textInput.Disable()
	_ = textInput.Commit()
	textInputSerial++
	currentWindow.textInputEnabled = false
	currentWindow.haveLastCursor = false
}

// updateTextInputCursorRectangle publishes the candidate-window anchor from
// CompositionPos (preedit start) falling back to CaretPos. Surface-local
// logical points; Wayland surface origin is top-left, so y is the top of the
// caret rect (CompositionPos is bottom-left). Returns whether the rect changed
// from the last one we sent.
func updateTextInputCursorRectangle() bool {
	if textInput == nil || !currentWindow.textInputEnabled {
		return false
	}
	pos := shirei.GetHost().CompositionPos
	if shirei.GetInputState().Composition == "" {
		pos = shirei.GetHost().CaretPos
	}
	h := shirei.GetHost().CaretHeight
	if h <= 0 {
		h = 16
	}
	// Skip the all-zero placeholder from before any field has published geometry
	// — sending (0,0) parks the candidate window at the surface origin.
	if pos[0] == 0 && pos[1] == 0 && shirei.GetHost().CaretHeight == 0 {
		return false
	}
	x := int32(pos[0] + 0.5)
	y := int32(pos[1] - h + 0.5)
	if y < 0 {
		y = 0
	}
	w := int32(1) // zero-width caret rects are rejected by some IMs
	hh := int32(h + 0.5)
	if currentWindow.haveLastCursor && x == currentWindow.lastCursorX && y == currentWindow.lastCursorY && w == currentWindow.lastCursorW && hh == currentWindow.lastCursorH {
		return false
	}
	_ = textInput.SetCursorRectangle(x, y, w, hh)
	currentWindow.lastCursorX, currentWindow.lastCursorY, currentWindow.lastCursorW, currentWindow.lastCursorH = x, y, w, hh
	currentWindow.haveLastCursor = true
	return true
}

// commitTextInputState refreshes the candidate-window anchor after a frame
// has published CompositionPos/CaretPos. Only commits when the rect moved —
// committing every composing frame caused Mutter to emit done at 60 Hz and
// (with the old empty-done→clear logic) tore down the preedit every frame.
func commitTextInputState() {
	if textInput == nil || !currentWindow.textInputEnabled {
		return
	}
	// Keep publishing while composing, and once more after a focus/caret move
	// even when not composing so the next composition anchors correctly.
	if shirei.GetInputState().Composition == "" && currentWindow.haveLastCursor {
		// Still update when caret moves between compositions.
		if !cursorPosChanged() {
			return
		}
	}
	if !updateTextInputCursorRectangle() {
		return
	}
	if err := textInput.Commit(); err == nil {
		textInputSerial++
	}
}

func cursorPosChanged() bool {
	pos := shirei.GetHost().CaretPos
	h := shirei.GetHost().CaretHeight
	if h <= 0 {
		h = 16
	}
	x := int32(pos[0] + 0.5)
	y := int32(pos[1] - h + 0.5)
	if y < 0 {
		y = 0
	}
	hh := int32(h + 0.5)
	return !currentWindow.haveLastCursor || x != currentWindow.lastCursorX || y != currentWindow.lastCursorY || hh != currentWindow.lastCursorH
}

// --- composition helpers ----------------------------------------------------

func clearComposition() {
	shirei.GetInputState().Composition = ""
	shirei.GetInputState().CompositionSel = [2]int{}
}

// setCompositionUTF8 publishes preedit text. cursorBegin/End are UTF-8 byte
// offsets into text (-1,-1 means hide caret → collapsed at end for display).
func setCompositionUTF8(text string, cursorBegin, cursorEnd int32) {
	if text == "" {
		clearComposition()
		return
	}
	start, end := utf8ByteOffsetsToRuneOffsets(text, int(cursorBegin), int(cursorEnd))
	shirei.GetInputState().Composition = text
	shirei.GetInputState().CompositionSel = [2]int{start, end}
}

// utf8ByteOffsetsToRuneOffsets converts text-input-v3 byte offsets into
// CompositionSel rune offsets. -1/-1 (hide caret) becomes a collapsed caret
// at the end of the preedit — the widget needs a place to draw something.
func utf8ByteOffsetsToRuneOffsets(text string, begin, end int) (int, int) {
	if begin < 0 && end < 0 {
		n := utf8.RuneCountInString(text)
		return n, n
	}
	if begin < 0 {
		begin = 0
	}
	if end < 0 {
		end = begin
	}
	if end < begin {
		begin, end = end, begin
	}
	return utf8ByteOffsetToRuneOffset(text, begin), utf8ByteOffsetToRuneOffset(text, end)
}

func utf8ByteOffsetToRuneOffset(text string, byteOffset int) int {
	if byteOffset <= 0 {
		return 0
	}
	if byteOffset >= len(text) {
		return utf8.RuneCountInString(text)
	}
	// Clamp to a code-point boundary if the offset landed mid-rune.
	for byteOffset > 0 && !utf8.RuneStart(text[byteOffset]) {
		byteOffset--
	}
	return utf8.RuneCountInString(text[:byteOffset])
}

func resetPendingIME() {
	currentWindow.pendingPreedit = ""
	currentWindow.pendingPreeditBegin = 0
	currentWindow.pendingPreeditEnd = 0
	currentWindow.pendingCommit = ""
	currentWindow.gotPreedit = false
	currentWindow.gotCommit = false
}

// --- committed-text accumulation --------------------------------------------

func appendPendingText(s string) {
	if s == "" {
		return
	}
	currentWindow.pendingText += s
}

func flushPendingText() {
	if currentWindow.pendingText == "" {
		return
	}
	shirei.GetFrameInput().Text += currentWindow.pendingText
	currentWindow.pendingText = ""
}

// textInputConsumesKeys reports whether the IME currently owns key delivery
// (composition active). Navigation/edit keys must not reach the widget then.
func textInputConsumesKeys() bool {
	return shirei.GetInputState().Composition != ""
}

// commitImeBeforeInterruption accepts the current preedit as typed text and
// resets the compositor IME (disable+enable), matching Cocoa B4 / Win32 W5
// click-commit policy. text-input-v3 has no force-commit request, so we
// promote the shadow preedit ourselves and bounce enable to clear IM state.
func commitImeBeforeInterruption() {
	if shirei.GetInputState().Composition == "" {
		return
	}
	appendPendingText(shirei.GetInputState().Composition)
	clearComposition()
	resetPendingIME()
	currentWindow.haveLastCursor = false
	if textInput != nil && currentWindow.textInputOnSurface {
		// Bounce enable so the input method drops its in-progress conversion.
		_ = textInput.Disable()
		_ = textInput.Commit()
		textInputSerial++
		currentWindow.textInputEnabled = false
		enableTextInput()
	}
	if currentWindow.pendingText != "" {
		// Land the committed text before the click is processed so it inserts
		// at the old caret, not at the newly clicked field.
		drawFrame()
	}
}

func truncateForLog(s string) string {
	const max = 32
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
