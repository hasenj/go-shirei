package widgets

import "testing"

// The editing model is pure, so these tests need no fonts, frames, or
// focus — just state in, state out. All indices are rune indices;
// multibyte cases pin that.

func es(text string, cursor, anchor int) _EditState {
	return _EditState{Runes: []rune(text), Cursor: cursor, Anchor: anchor}
}

func check(t *testing.T, name string, e _EditState, want string, wantCursor, wantAnchor int) {
	t.Helper()
	if string(e.Runes) != want || e.Cursor != wantCursor || e.Anchor != wantAnchor {
		t.Errorf("%s: got %q cursor=%d anchor=%d, want %q %d %d",
			name, string(e.Runes), e.Cursor, e.Anchor, want, wantCursor, wantAnchor)
	}
}

func TestEditMotion(t *testing.T) {
	cases := []struct {
		name           string
		in             string
		cursor, anchor int
		move           int // -1 left, +1 right
		extend         bool
		wantC, wantA   int
	}{
		{"right", "hello", 1, 1, +1, false, 2, 2},
		{"left", "hello", 2, 2, -1, false, 1, 1},
		{"right clamps at end", "hello", 5, 5, +1, false, 5, 5},
		{"left clamps at start", "hello", 0, 0, -1, false, 0, 0},
		{"shift+right extends", "hello", 1, 1, +1, true, 2, 1},
		{"shift+left extends", "hello", 3, 5, -1, true, 2, 5},
		{"plain motion collapses selection to caret step", "hello", 4, 1, -1, false, 3, 3},
		{"multibyte counts runes", "日本語", 1, 1, +1, false, 2, 2},
		{"empty buffer", "", 0, 0, +1, false, 0, 0},
	}
	for _, c := range cases {
		e := es(c.in, c.cursor, c.anchor)
		if c.move < 0 {
			e.MoveLeft(c.extend)
		} else {
			e.MoveRight(c.extend)
		}
		check(t, c.name, e, c.in, c.wantC, c.wantA)
	}
}

func TestEditDelete(t *testing.T) {
	cases := []struct {
		name           string
		in             string
		cursor, anchor int
		forward        bool
		want           string
		wantCursor     int
		wantChanged    bool
	}{
		{"backspace mid", "hello", 2, 2, false, "hllo", 1, true},
		{"backspace at start is a no-op", "hello", 0, 0, false, "hello", 0, false},
		{"forward delete mid", "hello", 2, 2, true, "helo", 2, true},
		{"forward delete at end is a no-op", "hello", 5, 5, true, "hello", 5, false},
		{"backspace deletes selection only", "hello", 4, 1, false, "ho", 1, true},
		{"inverted selection deletes the same", "hello", 1, 4, false, "ho", 1, true},
		{"forward delete with selection deletes selection", "hello", 1, 4, true, "ho", 1, true},
		{"selection to end", "hello", 5, 2, false, "he", 2, true},
		{"multibyte backspace", "日本語", 1, 1, false, "本語", 0, true},
		{"empty buffer backspace", "", 0, 0, false, "", 0, false},
	}
	for _, c := range cases {
		e := es(c.in, c.cursor, c.anchor)
		var changed bool
		if c.forward {
			changed = e.DeleteForward()
		} else {
			changed = e.DeleteBackward()
		}
		if changed != c.wantChanged {
			t.Errorf("%s: changed = %v, want %v", c.name, changed, c.wantChanged)
		}
		// deletes always collapse the selection onto the caret
		check(t, c.name, e, c.want, c.wantCursor, c.wantCursor)
	}
}

func TestEditDeleteSelection(t *testing.T) {
	e := es("hello", 2, 2)
	if e.DeleteSelection() {
		t.Errorf("DeleteSelection with no selection reported a change")
	}
	check(t, "no selection", e, "hello", 2, 2)

	e = es("hello", 4, 1)
	if !e.DeleteSelection() {
		t.Errorf("DeleteSelection with selection reported no change")
	}
	check(t, "with selection", e, "ho", 1, 1)
}

func TestEditInsert(t *testing.T) {
	cases := []struct {
		name           string
		in             string
		cursor, anchor int
		text           string
		want           string
		wantCursor     int
	}{
		{"insert mid", "hello", 2, 2, "XY", "heXYllo", 4},
		{"insert at start", "hello", 0, 0, "-", "-hello", 1},
		{"insert at end", "hello", 5, 5, "!", "hello!", 6},
		{"replace selection", "hello", 4, 1, "-", "h-o", 2},
		{"replace inverted selection", "hello", 1, 4, "-", "h-o", 2},
		{"out-of-range caret clamps", "hi", 99, 99, "!", "hi!", 3},
		{"negative caret clamps", "hi", -3, -3, "!", "!hi", 1},
		{"multibyte insert", "aあ", 1, 1, "い", "aいあ", 2},
		{"into empty buffer", "", 0, 0, "ab", "ab", 2},
	}
	for _, c := range cases {
		e := es(c.in, c.cursor, c.anchor)
		e.Insert(c.text)
		check(t, c.name, e, c.want, c.wantCursor, c.wantCursor)
	}
}

// The word-boundary rule (notes/textinput-plan.md): skip whitespace
// toward the motion direction, then one maximal same-class run.
// Punctuation runs are their own words (path components!), and
// han/hiragana/katakana transitions end a word.
func TestWordBoundaries(t *testing.T) {
	cases := []struct {
		text      string
		from      int
		wantLeft  int
		wantRight int
	}{
		{"hello world", 0, 0, 5},
		{"hello world", 5, 0, 11}, // from between: left to word start, right past the space through "world"
		{"hello world", 6, 0, 11}, // whitespace is skipped, then the run
		{"hello world", 8, 6, 11}, // from mid-word: to its edges
		{"hello world", 11, 6, 11},
		{"/opt/brew", 1, 0, 4}, // separators are their own runs
		{"/opt/brew", 4, 1, 5},
		{"foo, bar", 3, 0, 4}, // punct run after "foo"
		{"foo, bar", 4, 3, 8},
		{"a_b2 c", 0, 0, 4},   // underscore and digits are word runes
		{"漢字かなカナab", 0, 0, 2}, // script transitions end words
		{"漢字かなカナab", 2, 0, 4},
		{"漢字かなカナab", 4, 2, 6},
		{"漢字かなカナab", 6, 4, 8},
		{"cafés x", 0, 0, 6}, // combining mark stays in the word
		{"   ", 1, 0, 3},      // all-whitespace: skip to the edge
		{"", 0, 0, 0},
	}
	for _, c := range cases {
		runes := []rune(c.text)
		if got := wordLeft(runes, c.from); got != c.wantLeft {
			t.Errorf("wordLeft(%q, %d) = %d, want %d", c.text, c.from, got, c.wantLeft)
		}
		if got := wordRight(runes, c.from); got != c.wantRight {
			t.Errorf("wordRight(%q, %d) = %d, want %d", c.text, c.from, got, c.wantRight)
		}
	}
}

func TestWordAndLineOps(t *testing.T) {
	cases := []struct {
		name           string
		in             string
		cursor, anchor int
		cmd            _EditCommand
		want           string
		wantC, wantA   int
		wantEdited     bool
	}{
		{"word left", "hello world", 8, 8, _EditCommand{Op: _EditMoveWordLeft}, "hello world", 6, 6, false},
		{"word right extend", "hello world", 0, 0, _EditCommand{Op: _EditMoveWordRight, Extend: true}, "hello world", 5, 0, false},
		{"line start", "hello world", 8, 8, _EditCommand{Op: _EditMoveLineStart}, "hello world", 0, 0, false},
		{"line end extend", "hello world", 3, 3, _EditCommand{Op: _EditMoveLineEnd, Extend: true}, "hello world", 11, 3, false},

		{"delete word backward", "hello world", 11, 11, _EditCommand{Op: _EditDeleteWordBackward}, "hello ", 6, 6, true},
		{"delete word backward mid-word", "hello world", 8, 8, _EditCommand{Op: _EditDeleteWordBackward}, "hello rld", 6, 6, true},
		{"delete word backward at start", "hello", 0, 0, _EditCommand{Op: _EditDeleteWordBackward}, "hello", 0, 0, false},
		{"delete word backward with selection deletes it", "hello world", 8, 2, _EditCommand{Op: _EditDeleteWordBackward}, "herld", 2, 2, true},
		{"delete word forward", "hello world", 0, 0, _EditCommand{Op: _EditDeleteWordForward}, " world", 0, 0, true},
		{"delete to line start", "hello world", 8, 8, _EditCommand{Op: _EditDeleteToLineStart}, "rld", 0, 0, true},
		{"delete to line start at start", "hello", 0, 0, _EditCommand{Op: _EditDeleteToLineStart}, "hello", 0, 0, false},

		{"select word (double-click)", "hello world", 0, 0, _EditCommand{Op: _EditSelectWord, Pos: 8}, "hello world", 11, 6, false},
		{"select word on whitespace", "hello world", 0, 0, _EditCommand{Op: _EditSelectWord, Pos: 5}, "hello world", 6, 5, false},
		{"select word at text end", "hello", 0, 0, _EditCommand{Op: _EditSelectWord, Pos: 5}, "hello", 5, 0, false},
		{"select word on path component", "/opt/brew", 0, 0, _EditCommand{Op: _EditSelectWord, Pos: 6}, "/opt/brew", 9, 5, false},
	}
	for _, c := range cases {
		e := es(c.in, c.cursor, c.anchor)
		r := e.Apply(c.cmd)
		if r.Edited != c.wantEdited {
			t.Errorf("%s: Edited = %v, want %v", c.name, r.Edited, c.wantEdited)
		}
		check(t, c.name, e, c.want, c.wantC, c.wantA)
	}
}

func TestEditLineStarts(t *testing.T) {
	t.Run("hard lines", func(t *testing.T) {
		lineStarts := []int{0, 4, 8} // "one\n", "two\n", "three"

		e := es("one\ntwo\nthree", 5, 5)
		e.LineStarts = lineStarts
		e.MoveLineStart(false)
		check(t, "home on middle hard line", e, "one\ntwo\nthree", 4, 4)

		e = es("one\ntwo\nthree", 5, 5)
		e.LineStarts = lineStarts
		e.MoveLineEnd(false)
		check(t, "end on middle hard line", e, "one\ntwo\nthree", 7, 7)

		e = es("one\ntwo\nthree", 7, 7)
		e.LineStarts = lineStarts
		if !e.DeleteToLineStart() {
			t.Fatal("DeleteToLineStart on middle hard line reported no change")
		}
		check(t, "delete to middle hard line start", e, "one\n\nthree", 4, 4)
	})

	t.Run("soft wrapped lines", func(t *testing.T) {
		lineStarts := []int{0, 3, 6}

		e := es("abcdefghi", 1, 1)
		e.LineStarts = lineStarts
		e.MoveLineEnd(false)
		check(t, "end on soft-wrapped line", e, "abcdefghi", 3, 3)

		e = es("abcdefghi", 3, 3)
		e.LineStarts = lineStarts
		e.MoveLineStart(false)
		check(t, "home at soft-wrap start", e, "abcdefghi", 3, 3)

		e.MoveLineEnd(false)
		check(t, "end from soft-wrap start uses next visual line", e, "abcdefghi", 6, 6)
	})

	t.Run("empty hard line", func(t *testing.T) {
		lineStarts := []int{0, 2, 3} // "a\n", "\n", "b"

		e := es("a\n\nb", 2, 2)
		e.LineStarts = lineStarts
		e.MoveLineStart(false)
		check(t, "home on empty line", e, "a\n\nb", 2, 2)

		e.MoveLineEnd(false)
		check(t, "end on empty line", e, "a\n\nb", 2, 2)

		if e.DeleteToLineStart() {
			t.Fatal("DeleteToLineStart on empty line reported a change")
		}
		check(t, "delete to empty line start", e, "a\n\nb", 2, 2)
	})

	t.Run("nil line starts stay single-line", func(t *testing.T) {
		e := es("one\ntwo", 5, 5)
		e.MoveLineStart(false)
		check(t, "nil home", e, "one\ntwo", 0, 0)
		e.MoveLineEnd(false)
		check(t, "nil end", e, "one\ntwo", 7, 7)
	})
}

func TestEditApply(t *testing.T) {
	cases := []struct {
		name           string
		in             string
		cursor, anchor int
		cmd            _EditCommand
		want           string
		wantC, wantA   int
		wantR          _EditResult
	}{
		{"move left", "hello", 2, 2, _EditCommand{Op: _EditMoveLeft}, "hello", 1, 1, _EditResult{Caret: true}},
		{"move right extend", "hello", 2, 2, _EditCommand{Op: _EditMoveRight, Extend: true}, "hello", 3, 2, _EditResult{Caret: true}},
		{"move to (mouse)", "hello", 0, 0, _EditCommand{Op: _EditMoveTo, Pos: 4}, "hello", 4, 4, _EditResult{Caret: true}},
		{"move to extend (drag)", "hello", 1, 1, _EditCommand{Op: _EditMoveTo, Pos: 4, Extend: true}, "hello", 4, 1, _EditResult{Caret: true}},
		{"select all keeps blink", "hello", 2, 2, _EditCommand{Op: _EditSelectAll}, "hello", 5, 0, _EditResult{}},
		{"backspace", "hello", 2, 2, _EditCommand{Op: _EditDeleteBackward}, "hllo", 1, 1, _EditResult{Edited: true, Caret: true}},
		{"backspace at start: nothing", "hello", 0, 0, _EditCommand{Op: _EditDeleteBackward}, "hello", 0, 0, _EditResult{}},
		{"insert", "hello", 5, 5, _EditCommand{Op: _EditInsert, Text: "!"}, "hello!", 6, 6, _EditResult{Edited: true, Caret: true}},
		{"copy selection", "hello", 4, 1, _EditCommand{Op: _EditCopy}, "hello", 4, 1, _EditResult{Copy: "ell"}},
		{"copy without selection", "hello", 2, 2, _EditCommand{Op: _EditCopy}, "hello", 2, 2, _EditResult{}},
		{"cut selection", "hello", 4, 1, _EditCommand{Op: _EditCut}, "ho", 1, 1, _EditResult{Edited: true, Caret: true, Copy: "ell"}},
		{"cut without selection: nothing", "hello", 2, 2, _EditCommand{Op: _EditCut}, "hello", 2, 2, _EditResult{}},
		{"paste is a request", "hello", 2, 2, _EditCommand{Op: _EditPaste}, "hello", 2, 2, _EditResult{Paste: true}},
	}
	for _, c := range cases {
		e := es(c.in, c.cursor, c.anchor)
		r := e.Apply(c.cmd)
		if r != c.wantR {
			t.Errorf("%s: result = %+v, want %+v", c.name, r, c.wantR)
		}
		check(t, c.name, e, c.want, c.wantC, c.wantA)
	}
}

// Bounds turn cluster boundaries into the caret vocabulary: motions
// and single-target deletes snap to them. Simulated here with explicit
// boundary lists — e.g. "a👨‍👩‍👧‍👦b": the family emoji is 7 runes
// (4 people + 3 ZWJ) in one cluster, bounds {0,1,8,9}.
func TestEditBoundsSnapping(t *testing.T) {
	family := "a👨‍👩‍👧‍👦b" // runes: a at 0, cluster 1..7, b at 8
	bounds := []int{0, 1, 8, 9}

	e := es(family, 1, 1)
	e.Bounds = bounds
	e.MoveRight(false)
	if e.Cursor != 8 {
		t.Errorf("MoveRight over the cluster: cursor = %d, want 8", e.Cursor)
	}
	e.MoveLeft(false)
	if e.Cursor != 1 {
		t.Errorf("MoveLeft back over the cluster: cursor = %d, want 1", e.Cursor)
	}

	// backspace from after the cluster removes the whole cluster
	e = es(family, 8, 8)
	e.Bounds = bounds
	if !e.DeleteBackward() {
		t.Fatal("DeleteBackward reported no change")
	}
	check(t, "cluster backspace", e, "ab", 1, 1)

	// forward delete from before the cluster removes it too
	e = es(family, 1, 1)
	e.Bounds = bounds
	e.DeleteForward()
	check(t, "cluster forward delete", e, "ab", 1, 1)

	// a stale cursor inside the cluster still moves out to a boundary
	e = es(family, 4, 4)
	e.Bounds = bounds
	e.MoveLeft(false)
	if e.Cursor != 1 {
		t.Errorf("MoveLeft from mid-cluster: cursor = %d, want 1", e.Cursor)
	}
	e = es(family, 4, 4)
	e.Bounds = bounds
	e.MoveRight(false)
	if e.Cursor != 8 {
		t.Errorf("MoveRight from mid-cluster: cursor = %d, want 8", e.Cursor)
	}

	// nil bounds keep plain rune stepping
	e = es("abc", 1, 1)
	e.MoveRight(false)
	if e.Cursor != 2 {
		t.Errorf("nil-bounds MoveRight: cursor = %d, want 2", e.Cursor)
	}
}

// history helper: apply an editing command through the same
// record-at-the-choke-point flow the widget shell uses.
func histApply(h *_EditHistory, e *_EditState, cmd _EditCommand) {
	pre := snapshotOf(e)
	r := e.Apply(cmd)
	if r.Edited {
		h.Record(cmd, pre)
	} else if r.Caret {
		h.BreakRun()
	}
}

func typeRunes(h *_EditHistory, e *_EditState, s string) {
	for _, r := range s {
		histApply(h, e, _EditCommand{Op: _EditInsert, Text: string(r)})
	}
}

func TestEditHistoryCoalescing(t *testing.T) {
	var h _EditHistory
	e := es("", 0, 0)

	typeRunes(&h, &e, "abc")
	if !h.Undo(&e) {
		t.Fatal("undo after typing failed")
	}
	check(t, "typing coalesces into one step", e, "", 0, 0)

	if !h.Redo(&e) {
		t.Fatal("redo failed")
	}
	check(t, "redo restores the run", e, "abc", 3, 3)

	// motion breaks the run: two separate undo steps
	typeRunes(&h, &e, "de") // "abcde" — redo stack cleared... but coalesces with?
	histApply(&h, &e, _EditCommand{Op: _EditMoveLeft})
	typeRunes(&h, &e, "X") // "abcdXe"... cursor was 4 after moveLeft
	if string(e.Runes) != "abcdXe" {
		t.Fatalf("setup: %q", string(e.Runes))
	}
	h.Undo(&e)
	check(t, "undo removes only the post-motion typing", e, "abcde", 4, 4)
	h.Undo(&e)
	check(t, "next undo removes the earlier run", e, "abc", 3, 3)

	// backspace runs coalesce, separately from typing
	e = es("hello", 5, 5)
	h = _EditHistory{}
	histApply(&h, &e, _EditCommand{Op: _EditDeleteBackward})
	histApply(&h, &e, _EditCommand{Op: _EditDeleteBackward})
	h.Undo(&e)
	check(t, "backspace run undone in one step", e, "hello", 5, 5)
}

func TestEditHistoryPasteAndSelection(t *testing.T) {
	var h _EditHistory
	e := es("", 0, 0)

	typeRunes(&h, &e, "ab")
	histApply(&h, &e, _EditCommand{Op: _EditInsert, Text: "PASTE"}) // multi-rune: own step
	typeRunes(&h, &e, "c")
	if string(e.Runes) != "abPASTEc" {
		t.Fatalf("setup: %q", string(e.Runes))
	}
	h.Undo(&e)
	check(t, "undo the post-paste typing", e, "abPASTE", 7, 7)
	h.Undo(&e)
	check(t, "undo the paste alone", e, "ab", 2, 2)
	h.Undo(&e)
	check(t, "undo the initial typing", e, "", 0, 0)

	// replacing a selection is its own step even mid-typing-run
	e = es("", 0, 0)
	h = _EditHistory{}
	typeRunes(&h, &e, "ab")
	e.SelectAll()
	histApply(&h, &e, _EditCommand{Op: _EditInsert, Text: "c"})
	if string(e.Runes) != "c" {
		t.Fatalf("setup: %q", string(e.Runes))
	}
	h.Undo(&e)
	check(t, "undo select-all-and-type restores the selection", e, "ab", 2, 0)
}

func TestEditHistoryEdges(t *testing.T) {
	var h _EditHistory
	e := es("x", 1, 1)
	if h.Undo(&e) || h.Redo(&e) {
		t.Error("undo/redo on empty history reported a change")
	}

	// a new edit invalidates redo
	typeRunes(&h, &e, "y") // "xy"
	h.Undo(&e)             // "x"
	typeRunes(&h, &e, "z") // "xz"
	if h.Redo(&e) {
		t.Error("redo survived a new edit")
	}
	check(t, "state after new edit", e, "xz", 2, 2)
}

func TestEditSelRangeAndSelectAll(t *testing.T) {
	e := es("hello", 4, 1)
	if from, to := e.SelRange(); from != 1 || to != 4 {
		t.Errorf("SelRange = %d..%d, want 1..4", from, to)
	}
	e = es("hello", 1, 4) // inverted normalizes
	if from, to := e.SelRange(); from != 1 || to != 4 {
		t.Errorf("inverted SelRange = %d..%d, want 1..4", from, to)
	}
	e = es("hi", 99, -5) // stale indices clamp to the buffer
	if from, to := e.SelRange(); from != 0 || to != 2 {
		t.Errorf("clamped SelRange = %d..%d, want 0..2", from, to)
	}

	e = es("hello", 2, 2)
	e.SelectAll()
	check(t, "select all", e, "hello", 5, 0)
}
