package widgets

// The pure editing model for text inputs. _EditState is a rune buffer
// plus caret and selection, and every operation is a plain state
// transition — no shaping, no focus, no frames, no globals. The widget
// shell issues _EditCommand values from input analysis and Apply
// executes them; keeping the layers apart is what keeps input code
// from growing hairy as features accrete. Design contract and the
// recipe for adding features: notes/textinput-architecture.md.

import (
	"sort"
	"unicode"

	g "go.hasen.dev/generic"
)

// _EditState is a snapshot of editable text plus caret and selection,
// all in rune indices. Cursor is the moving end (the caret); Anchor is
// the fixed end set where a selection started. Cursor == Anchor means
// no selection.
type _EditState struct {
	Runes  []rune
	Cursor int
	Anchor int

	// Bounds, when set, lists the rune indices the caret may rest on —
	// the shaped text's cluster boundaries (the shell derives them via
	// clusterBounds). Char motions and single-target deletes snap to
	// them, so the caret can never stop inside a ligature, a combining
	// sequence, or a ZWJ emoji, and backspace removes whole clusters.
	// nil means every rune index is a stop (pure tests, no shaping).
	// Sorted ascending; structure-as-data like the future multiline
	// line starts (notes: no interfaces for few variants).
	Bounds []int

	// LineStarts lists the rune index where each visual line begins,
	// ascending, starting with 0. Line-edge motions and deletes use
	// this data; nil behaves as one line spanning the whole buffer.
	LineStarts []int
}

// prevStop and nextStop are the caret-legal neighbors of pos. Results
// may exceed the buffer edges; MoveTo/deleteRange clamp.
func (e *_EditState) prevStop(pos int) int {
	if e.Bounds == nil {
		return pos - 1
	}
	i := sort.SearchInts(e.Bounds, pos) // first bound >= pos
	if i == 0 {
		return pos - 1
	}
	return e.Bounds[i-1]
}

func (e *_EditState) nextStop(pos int) int {
	if e.Bounds == nil {
		return pos + 1
	}
	i := sort.SearchInts(e.Bounds, pos+1) // first bound > pos
	if i == len(e.Bounds) {
		return pos + 1
	}
	return e.Bounds[i]
}

type _EditOp byte

const (
	_EditNone _EditOp = iota
	_EditMoveLeft
	_EditMoveRight
	_EditMoveWordLeft
	_EditMoveWordRight
	_EditMoveLineStart
	_EditMoveLineEnd
	_EditMoveUp   // resolved by the shell from frame-local geometry
	_EditMoveDown // resolved by the shell from frame-local geometry
	_EditMoveTo
	_EditSelectAll
	_EditSelectWord // Pos: select the word run under a double-click
	_EditDeleteBackward
	_EditDeleteForward
	_EditDeleteWordBackward
	_EditDeleteWordForward
	_EditDeleteToLineStart
	_EditDeleteSelection
	_EditInsert
	_EditCopy
	_EditCut
	_EditPaste
	_EditUndo // dispatched by the shell against the input's _EditHistory
	_EditRedo
)

// _EditCommand is one thing a frame's input asks the editor to do. The
// widget shell produces commands from input analysis — mouse geometry,
// key combos, typed text (textinput.go, editdecode.go) — and Apply
// executes them, so the model never sees raw input and the shell never
// touches editing state. Commands are also the intended unit of undo
// recording and coalescing later (notes/textinput-plan.md phase 4).
type _EditCommand struct {
	Op     _EditOp
	Extend bool   // motions: extend the selection instead of collapsing it (shift)
	Pos    int    // _EditMoveTo: target rune index (already resolved from geometry)
	Text   string // _EditInsert: the text to insert
}

// _EditResult reports what a command did, so the shell can sync the
// outside world: write the buffer back, reset the caret blink, and
// perform clipboard I/O — the model itself stays pure (Copy is just a
// produced string; Paste is just a request flag).
type _EditResult struct {
	Edited bool   // the buffer changed
	Caret  bool   // the caret or selection was interacted with (blink reset)
	Copy   string // text the command wants on the clipboard
	Paste  bool   // the command wants a clipboard read (text arrives as a later _EditInsert)
}

func (e *_EditState) Apply(cmd _EditCommand) (r _EditResult) {
	switch cmd.Op {
	case _EditMoveLeft:
		e.MoveLeft(cmd.Extend)
		r.Caret = true
	case _EditMoveRight:
		e.MoveRight(cmd.Extend)
		r.Caret = true
	case _EditMoveWordLeft:
		e.MoveWordLeft(cmd.Extend)
		r.Caret = true
	case _EditMoveWordRight:
		e.MoveWordRight(cmd.Extend)
		r.Caret = true
	case _EditMoveLineStart:
		e.MoveLineStart(cmd.Extend)
		r.Caret = true
	case _EditMoveLineEnd:
		e.MoveLineEnd(cmd.Extend)
		r.Caret = true
	case _EditMoveTo:
		e.MoveTo(cmd.Pos, cmd.Extend)
		r.Caret = true
	case _EditSelectAll:
		e.SelectAll()
		// deliberately not Caret: select-all keeps the blink phase (historical)
	case _EditSelectWord:
		e.SelectWordAt(cmd.Pos)
		r.Caret = true
	case _EditDeleteBackward:
		r.Edited = e.DeleteBackward()
		r.Caret = r.Edited
	case _EditDeleteForward:
		r.Edited = e.DeleteForward()
		r.Caret = r.Edited
	case _EditDeleteWordBackward:
		r.Edited = e.DeleteWordBackward()
		r.Caret = r.Edited
	case _EditDeleteWordForward:
		r.Edited = e.DeleteWordForward()
		r.Caret = r.Edited
	case _EditDeleteToLineStart:
		r.Edited = e.DeleteToLineStart()
		r.Caret = r.Edited
	case _EditDeleteSelection:
		r.Edited = e.DeleteSelection()
		r.Caret = r.Edited
	case _EditInsert:
		e.Insert(cmd.Text)
		r.Edited = true
		r.Caret = true
	case _EditCopy:
		if from, to := e.SelRange(); from != to {
			r.Copy = string(e.Runes[from:to])
		}
	case _EditCut:
		if from, to := e.SelRange(); from != to {
			r.Copy = string(e.Runes[from:to])
			r.Edited = e.DeleteSelection()
			r.Caret = r.Edited
		}
	case _EditPaste:
		r.Paste = true
	}
	return r
}

// SelRange returns the selection normalized to from <= to and clamped
// to the buffer.
func (e *_EditState) SelRange() (from, to int) {
	from, to = e.Anchor, e.Cursor
	if to < from {
		from, to = to, from
	}
	from = max(0, from)
	to = min(to, len(e.Runes))
	return from, to
}

// MoveTo places the caret at pos, clamped to the buffer. Unless extend
// is set (shift held), the anchor collapses to the caret.
func (e *_EditState) MoveTo(pos int, extend bool) {
	e.Cursor = max(0, min(pos, len(e.Runes)))
	if !extend {
		e.Anchor = e.Cursor
	}
}

// MoveLeft and MoveRight step one caret stop (a cluster when Bounds is
// set, else a rune). With a selection and no extend, the caret still
// steps from its own position rather than collapsing to the selection
// edge — the historical behavior; revisit deliberately if it bothers.
func (e *_EditState) MoveLeft(extend bool)  { e.MoveTo(e.prevStop(e.Cursor), extend) }
func (e *_EditState) MoveRight(extend bool) { e.MoveTo(e.nextStop(e.Cursor), extend) }

func (e *_EditState) MoveWordLeft(extend bool)  { e.MoveTo(wordLeft(e.Runes, e.Cursor), extend) }
func (e *_EditState) MoveWordRight(extend bool) { e.MoveTo(wordRight(e.Runes, e.Cursor), extend) }

func (e *_EditState) lineStartFor(pos int) int {
	pos = min(max(pos, 0), len(e.Runes))
	if len(e.LineStarts) == 0 {
		return 0
	}
	i := sort.Search(len(e.LineStarts), func(i int) bool {
		return e.LineStarts[i] > pos
	}) - 1
	if i < 0 {
		return 0
	}
	return min(max(e.LineStarts[i], 0), len(e.Runes))
}

func (e *_EditState) lineEndFor(pos int) int {
	pos = min(max(pos, 0), len(e.Runes))
	if len(e.LineStarts) == 0 {
		return len(e.Runes)
	}
	i := sort.Search(len(e.LineStarts), func(i int) bool {
		return e.LineStarts[i] > pos
	})
	if i == len(e.LineStarts) {
		return len(e.Runes)
	}
	nextStart := min(max(e.LineStarts[i], 0), len(e.Runes))
	if nextStart > 0 && e.Runes[nextStart-1] == '\n' {
		return nextStart - 1
	}
	return nextStart
}

func (e *_EditState) MoveLineStart(extend bool) { e.MoveTo(e.lineStartFor(e.Cursor), extend) }
func (e *_EditState) MoveLineEnd(extend bool)   { e.MoveTo(e.lineEndFor(e.Cursor), extend) }

func (e *_EditState) SelectAll() {
	e.Anchor = 0
	e.Cursor = len(e.Runes)
}

// SelectWordAt selects the word run under pos (double-click): the
// maximal run of pos's class, so double-clicking whitespace selects
// the whitespace run, macOS-style.
func (e *_EditState) SelectWordAt(pos int) {
	e.Anchor, e.Cursor = wordRunAt(e.Runes, pos)
}

// --- word boundaries -------------------------------------------------
//
// Class-run segmentation, no dictionary (notes/textinput-plan.md):
// a word motion skips whitespace toward the direction of travel, then
// one maximal run of the same class. Classes: whitespace; punctuation;
// word runes (letters, digits, combining marks, underscore) — with
// han/hiragana/katakana as separate word classes so script transitions
// end a word in Japanese text. Punctuation runs are words of their
// own, so word-jumps walk path components separator by separator.

const (
	classSpace = iota
	classPunct
	classWord
	classHan
	classHiragana
	classKatakana
)

func runeClass(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return classSpace
	case unicode.Is(unicode.Han, r):
		return classHan
	case unicode.Is(unicode.Hiragana, r):
		return classHiragana
	case unicode.Is(unicode.Katakana, r):
		return classKatakana
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
		return classWord
	default:
		return classPunct
	}
}

func wordLeft(runes []rune, from int) int {
	i := min(max(from, 0), len(runes))
	for i > 0 && runeClass(runes[i-1]) == classSpace {
		i--
	}
	if i > 0 {
		c := runeClass(runes[i-1])
		for i > 0 && runeClass(runes[i-1]) == c {
			i--
		}
	}
	return i
}

func wordRight(runes []rune, from int) int {
	n := len(runes)
	i := min(max(from, 0), n)
	for i < n && runeClass(runes[i]) == classSpace {
		i++
	}
	if i < n {
		c := runeClass(runes[i])
		for i < n && runeClass(runes[i]) == c {
			i++
		}
	}
	return i
}

// wordRunAt is the double-click rule: the maximal same-class run
// containing the rune at pos (pos at the very end counts as the last
// rune).
func wordRunAt(runes []rune, pos int) (start, end int) {
	n := len(runes)
	if n == 0 {
		return 0, 0
	}
	pos = min(max(pos, 0), n-1)
	c := runeClass(runes[pos])
	start, end = pos, pos+1
	for start > 0 && runeClass(runes[start-1]) == c {
		start--
	}
	for end < n && runeClass(runes[end]) == c {
		end++
	}
	return start, end
}

// deleteRange removes [from, to) clamped to the buffer, collapsing the
// caret onto the removal point. Reports whether the buffer changed.
func (e *_EditState) deleteRange(from, to int) bool {
	from = max(0, from)
	to = min(to, len(e.Runes))
	count := to - from
	if count <= 0 {
		return false
	}
	g.RemoveAt(&e.Runes, from, count)
	e.Cursor = max(0, min(from, len(e.Runes)))
	e.Anchor = e.Cursor
	return true
}

// DeleteBackward removes the selection, or one caret stop back — the
// whole cluster before the caret when Bounds is set.
func (e *_EditState) DeleteBackward() bool {
	if from, to := e.SelRange(); from != to {
		return e.deleteRange(from, to)
	}
	c := min(max(e.Cursor, 0), len(e.Runes))
	return e.deleteRange(e.prevStop(c), c)
}

// DeleteForward removes the selection, or one caret stop forward.
func (e *_EditState) DeleteForward() bool {
	if from, to := e.SelRange(); from != to {
		return e.deleteRange(from, to)
	}
	c := min(max(e.Cursor, 0), len(e.Runes))
	return e.deleteRange(c, e.nextStop(c))
}

// DeleteSelection removes the selection; with nothing selected it is a
// strict no-op (cut's half — plain deletes fall back to one stop).
func (e *_EditState) DeleteSelection() bool {
	from, to := e.SelRange()
	if from == to {
		return false
	}
	return e.deleteRange(from, to)
}

// The word/line delete flavors remove the selection if there is one —
// same convention as the char deletes.

func (e *_EditState) DeleteWordBackward() bool {
	if from, to := e.SelRange(); from != to {
		return e.deleteRange(from, to)
	}
	c := min(max(e.Cursor, 0), len(e.Runes))
	return e.deleteRange(wordLeft(e.Runes, c), c)
}

func (e *_EditState) DeleteWordForward() bool {
	if from, to := e.SelRange(); from != to {
		return e.deleteRange(from, to)
	}
	c := min(max(e.Cursor, 0), len(e.Runes))
	return e.deleteRange(c, wordRight(e.Runes, c))
}

func (e *_EditState) DeleteToLineStart() bool {
	if from, to := e.SelRange(); from != to {
		return e.deleteRange(from, to)
	}
	c := min(max(e.Cursor, 0), len(e.Runes))
	return e.deleteRange(e.lineStartFor(c), c)
}

// --- undo/redo -------------------------------------------------------
//
// Per-input history of full snapshots: single-line buffers are small,
// so a string copy per undo step is simple and obviously correct.
// Recording happens at the shell's Apply loop (the mutation choke
// point); _EditUndo/_EditRedo are dispatched by the shell too, because
// the history lives per input (a hook), not in the model. Snapshots
// are the revert half of an op record — if the generalized app-state
// undo idea (plan, shelved) ever lands, Record grows an apply half.

type _EditSnapshot struct {
	Text   string
	Cursor int
	Anchor int
}

func snapshotOf(e *_EditState) _EditSnapshot {
	return _EditSnapshot{Text: string(e.Runes), Cursor: e.Cursor, Anchor: e.Anchor}
}

func (e *_EditState) restore(s _EditSnapshot) {
	e.Runes = []rune(s.Text)
	e.Cursor = s.Cursor
	e.Anchor = s.Anchor
}

const editHistoryCap = 100

type _EditHistory struct {
	undo   []_EditSnapshot
	redo   []_EditSnapshot
	lastOp _EditOp // coalescing: what kind of edit the last record absorbed
}

// coalesces reports whether an edit continues the current run instead
// of opening a new undo step: single-rune typing and single-rune
// deletes coalesce with themselves. A selection being replaced always
// opens its own step (undo after select-all-and-type must restore the
// selected text, not the whole typing run), and multi-rune inserts
// (paste, IME commits) always stand alone.
func (h *_EditHistory) coalesces(cmd _EditCommand, pre _EditSnapshot) bool {
	if cmd.Op != h.lastOp || pre.Cursor != pre.Anchor {
		return false
	}
	switch cmd.Op {
	case _EditDeleteBackward, _EditDeleteForward:
		return true
	case _EditInsert:
		return len([]rune(cmd.Text)) == 1
	}
	return false
}

// Record notes an applied editing command, given the state before it.
// Any new edit invalidates the redo stack.
func (h *_EditHistory) Record(cmd _EditCommand, pre _EditSnapshot) {
	h.redo = h.redo[:0]
	if !h.coalesces(cmd, pre) {
		h.undo = append(h.undo, pre)
		if len(h.undo) > editHistoryCap {
			h.undo = h.undo[1:]
		}
	}
	h.lastOp = cmd.Op
	if cmd.Op == _EditInsert && len([]rune(cmd.Text)) > 1 {
		// a paste stands alone: the next keystroke starts a fresh run
		h.lastOp = _EditNone
	}
}

// BreakRun ends the current coalescing run (any caret motion does).
func (h *_EditHistory) BreakRun() { h.lastOp = _EditNone }

func (h *_EditHistory) Undo(e *_EditState) bool {
	if len(h.undo) == 0 {
		return false
	}
	h.redo = append(h.redo, snapshotOf(e))
	e.restore(h.undo[len(h.undo)-1])
	h.undo = h.undo[:len(h.undo)-1]
	h.lastOp = _EditNone
	return true
}

func (h *_EditHistory) Redo(e *_EditState) bool {
	if len(h.redo) == 0 {
		return false
	}
	h.undo = append(h.undo, snapshotOf(e))
	e.restore(h.redo[len(h.redo)-1])
	h.redo = h.redo[:len(h.redo)-1]
	h.lastOp = _EditNone
	return true
}

// Insert replaces the selection (if any) with text and places the
// caret after it.
func (e *_EditState) Insert(text string) {
	if from, to := e.SelRange(); from != to {
		e.deleteRange(from, to)
	}
	e.Cursor = max(0, min(e.Cursor, len(e.Runes)))
	newRunes := []rune(text)
	g.InsertAt(&e.Runes, e.Cursor, newRunes...)
	e.Cursor += len(newRunes)
	e.Anchor = e.Cursor
}
