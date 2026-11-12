package widgets

import (
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// TextDocument is editable UTF-8 text, selection, and replacement-based undo
// history. Its zero value is an empty document. Keep it in application state;
// TextEditor renders it without exporting or shaping the entire document.
// Like other widget state, access it on the UI thread or under the frame lock.
type TextDocument struct {
	pieces                  []textPiece
	starts                  []int // byte offsets of logical lines, including the final empty line
	size                    int
	anchor, cursor          LinePos
	undo, redo              []documentEdit
	serial, revision, saved uint64
	generation              uint64
	selectionVersion        uint64
	lastOp                  _EditOp
	newline                 string
}

// Pieces view the original string or inserted fragments. End is cumulative,
// so a line lookup starts with a binary search instead of scanning all edits.
type textPiece struct {
	text string
	end  int
}

type documentEdit struct {
	start                         int
	removed, inserted             string
	before, after                 [2]LinePos
	beforeRevision, afterRevision uint64
}

// SetText replaces the document and clears selection, undo, and modified state.
// Existing LF and CRLF line endings are preserved. New lines use the first
// newline's convention (LF for an empty document).
func (d *TextDocument) SetText(text string) {
	generation, selectionVersion := d.generation+1, d.selectionVersion+1
	*d = TextDocument{generation: generation, selectionVersion: selectionVersion, newline: "\n"}
	if text != "" {
		d.pieces = []textPiece{{text, len(text)}}
	}
	d.size = len(text)
	d.starts = []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			if len(d.starts) == 1 && i > 0 && text[i-1] == '\r' {
				d.newline = "\r\n"
			}
			d.starts = append(d.starts, i+1)
		}
	}
}

// Text materializes the document. Use it for export, not on every frame.
func (d *TextDocument) Text() string { return d.slice(0, d.size) }

// WriteTo writes the document's pieces without allocating a joined copy.
func (d *TextDocument) WriteTo(w io.Writer) (int64, error) {
	var total int64
	for _, p := range d.pieces {
		n, err := io.WriteString(w, p.text)
		total += int64(n)
		if err != nil {
			return total, err
		}
		if n != len(p.text) {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func (d *TextDocument) slice(from, to int) string {
	if from >= to {
		return ""
	}
	i := sort.Search(len(d.pieces), func(i int) bool { return d.pieces[i].end > from })
	p := d.pieces[i]
	start := p.end - len(p.text)
	if to <= p.end {
		return p.text[from-start : to-start]
	}
	var b strings.Builder
	b.Grow(to - from)
	for ; i < len(d.pieces) && from < to; i++ {
		p = d.pieces[i]
		start = p.end - len(p.text)
		end := min(to, p.end)
		b.WriteString(p.text[from-start : end-start])
		from = end
	}
	return b.String()
}

// LineCount includes the empty line after a trailing newline.
func (d *TextDocument) LineCount() int { return max(1, len(d.starts)) }

// Line returns one logical line without its LF or CRLF terminator.
// An out-of-range index returns an empty string.
func (d *TextDocument) Line(index int) string {
	if index < 0 || index >= len(d.starts) {
		return ""
	}
	start, end := d.starts[index], d.size
	if index+1 < len(d.starts) {
		end = d.starts[index+1] - 1
		if end > start && d.slice(end-1, end) == "\r" {
			end--
		}
	}
	return d.slice(start, end)
}

// Selection returns the anchor and moving caret in zero-based line/rune units.
func (d *TextDocument) Selection() (anchor, cursor LinePos) { return d.anchor, d.cursor }

// SetSelection clamps both endpoints and requests caret reveal in the view.
// It does not change focus. Equal endpoints place a caret.
func (d *TextDocument) SetSelection(anchor, cursor LinePos) {
	d.anchor, d.cursor = d.clamp(anchor), d.clamp(cursor)
	d.selectionVersion++
	d.lastOp = _EditNone
}

func (d *TextDocument) clamp(p LinePos) LinePos {
	p.Line = min(max(0, p.Line), d.LineCount()-1)
	p.Rune = min(max(0, p.Rune), utf8.RuneCountInString(d.Line(p.Line)))
	return p
}

func (d *TextDocument) offset(p LinePos) int {
	p = d.clamp(p)
	start := 0
	if len(d.starts) > 0 {
		start = d.starts[p.Line]
	}
	line, n := d.Line(p.Line), 0
	for i := range line {
		if n == p.Rune {
			return start + i
		}
		n++
	}
	return start + len(line)
}

func (d *TextDocument) position(offset int) LinePos {
	offset = min(max(offset, 0), d.size)
	if len(d.starts) == 0 {
		return LinePos{}
	}
	line := sort.Search(len(d.starts), func(i int) bool { return d.starts[i] > offset }) - 1
	end := min(offset, d.starts[line]+len(d.Line(line)))
	return LinePos{line, utf8.RuneCountInString(d.slice(d.starts[line], end))}
}

// Modified reports whether the current undo state differs from the saved state.
func (d *TextDocument) Modified() bool { return d.revision != d.saved }

// MarkSaved marks the current state clean and ends a typing undo group.
func (d *TextDocument) MarkSaved() { d.saved = d.revision; d.lastOp = _EditNone }

// SelectedText returns the exact selected bytes, including line terminators.
func (d *TextDocument) SelectedText() string {
	a, b := d.offset(d.anchor), d.offset(d.cursor)
	return d.slice(min(a, b), max(a, b))
}

// ReplaceSelection replaces the selection and leaves the caret after text.
// Incoming line endings follow this document's convention. Each call is one
// undo step; the widget groups consecutive single-rune typing and deletion.
func (d *TextDocument) ReplaceSelection(text string) {
	d.lastOp = _EditNone
	d.replaceSelection(text, _EditNone)
}

func (d *TextDocument) replaceSelection(text string, op _EditOp) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if d.newline == "\r\n" {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	a, b := d.offset(d.anchor), d.offset(d.cursor)
	from, to := min(a, b), max(a, b)
	d.replace(from, to, text, op)
}

func (d *TextDocument) replace(from, to int, text string, op _EditOp) {
	if from == to && text == "" {
		return
	}
	e := documentEdit{start: from, removed: d.slice(from, to), inserted: text,
		before: [2]LinePos{d.anchor, d.cursor}, beforeRevision: d.revision}
	d.splice(from, to, text)
	d.cursor = d.position(from + len(text))
	d.anchor = d.cursor
	d.selectionVersion++
	d.serial++
	d.revision = d.serial
	e.after, e.afterRevision = [2]LinePos{d.anchor, d.cursor}, d.revision
	d.redo = nil
	merged := false
	if len(d.undo) > 0 && op == d.lastOp && op != _EditNone {
		prev := &d.undo[len(d.undo)-1]
		// Bound typing groups so concatenating their small payloads stays cheap.
		if len(prev.inserted)+len(prev.removed) < 4096 {
			switch {
			case op == _EditInsert && e.removed == "" && prev.removed == "" && utf8.RuneCountInString(text) == 1 && e.start == prev.start+len(prev.inserted):
				prev.inserted += text
				merged = true
			case op == _EditDeleteBackward && text == "" && prev.inserted == "" && e.start+len(e.removed) == prev.start:
				prev.start = e.start
				prev.removed = e.removed + prev.removed
				merged = true
			case op == _EditDeleteForward && text == "" && prev.inserted == "" && e.start == prev.start:
				prev.removed += e.removed
				merged = true
			}
		}
		if merged {
			prev.after, prev.afterRevision = e.after, e.afterRevision
		}
	}
	if !merged {
		d.undo = append(d.undo, e)
		if len(d.undo) > editHistoryCap {
			copy(d.undo, d.undo[1:])
			d.undo[len(d.undo)-1] = documentEdit{}
			d.undo = d.undo[:len(d.undo)-1]
		}
	}
	d.lastOp = op
	if e.before[0] != e.before[1] || (op == _EditInsert && utf8.RuneCountInString(text) != 1) {
		d.lastOp = _EditNone
	}
}

// splice replaces bytes and updates the piece/line indexes. Callers supply
// UTF-8 boundaries and own selection, history, and newline policy.
func (d *TextDocument) splice(from, to int, text string) {
	pieces := make([]textPiece, 0, len(d.pieces)+2)
	end := 0
	add := func(s string) {
		if s != "" {
			end += len(s)
			// Keep sustained typing from producing one retained piece per key.
			// Large original spans stay shared; only small neighbors are joined.
			if n := len(pieces); n > 0 && len(pieces[n-1].text)+len(s) <= 2048 {
				pieces[n-1].text += s
				pieces[n-1].end = end
				return
			}
			pieces = append(pieces, textPiece{s, end})
		}
	}
	inserted := false
	for _, p := range d.pieces {
		start := p.end - len(p.text)
		if p.end <= from {
			add(p.text)
			continue
		}
		if !inserted {
			if from > start {
				add(p.text[:from-start])
			}
			add(text)
			inserted = true
		}
		if start >= to {
			add(p.text)
		} else if p.end > to {
			add(p.text[to-start:])
		}
	}
	if !inserted {
		add(text)
	}
	d.pieces = pieces
	if len(d.starts) == 0 {
		d.starts = []int{0}
	}
	lo := sort.Search(len(d.starts), func(i int) bool { return d.starts[i] > from })
	hi := sort.Search(len(d.starts), func(i int) bool { return d.starts[i] > to })
	added := strings.Count(text, "\n")
	n := len(d.starts)
	delta := added - (hi - lo)
	if delta > 0 {
		d.starts = append(d.starts, make([]int, delta)...)
	}
	copy(d.starts[lo+added:], d.starts[hi:n])
	d.starts = d.starts[:n+delta]
	shift := len(text) - (to - from)
	for i := lo + added; i < len(d.starts); i++ {
		d.starts[i] += shift
	}
	for i := range text {
		if text[i] == '\n' {
			d.starts[lo] = from + i + 1
			lo++
		}
	}
	d.size += shift
}

// Undo restores one edit group and its selection. It reports whether it acts.
func (d *TextDocument) Undo() bool {
	if len(d.undo) == 0 {
		return false
	}
	e := d.undo[len(d.undo)-1]
	d.undo[len(d.undo)-1] = documentEdit{}
	d.undo = d.undo[:len(d.undo)-1]
	d.splice(e.start, e.start+len(e.inserted), e.removed)
	d.anchor, d.cursor = e.before[0], e.before[1]
	d.revision = e.beforeRevision
	d.redo = append(d.redo, e)
	d.selectionVersion++
	d.lastOp = _EditNone
	return true
}

// Redo reapplies one undone edit group and its selection.
func (d *TextDocument) Redo() bool {
	if len(d.redo) == 0 {
		return false
	}
	e := d.redo[len(d.redo)-1]
	d.redo[len(d.redo)-1] = documentEdit{}
	d.redo = d.redo[:len(d.redo)-1]
	d.splice(e.start, e.start+len(e.removed), e.inserted)
	d.anchor, d.cursor = e.after[0], e.after[1]
	d.revision = e.afterRevision
	d.undo = append(d.undo, e)
	d.selectionVersion++
	d.lastOp = _EditNone
	return true
}

// apply resolves line-local editing through the same rune/cluster vocabulary
// as TextInput. Cross-line replacements go through the document's splice.
func (d *TextDocument) apply(cmd _EditCommand, bounds []int) (r _EditResult) {
	switch cmd.Op {
	case _EditInsert:
		d.replaceSelection(cmd.Text, cmd.Op)
		r.Edited = true
	case _EditUndo:
		r.Edited = d.Undo()
	case _EditRedo:
		r.Edited = d.Redo()
	case _EditCopy:
		r.Copy = d.SelectedText()
	case _EditCut:
		r.Copy = d.SelectedText()
		if r.Copy != "" {
			d.replaceSelection("", _EditNone)
			r.Edited = true
		}
	case _EditPaste:
		r.Paste = true
	case _EditSelectAll:
		last := d.LineCount() - 1
		d.SetSelection(LinePos{}, LinePos{last, utf8.RuneCountInString(d.Line(last))})
		r.Caret = true
	case _EditDeleteSelection:
		if d.anchor != d.cursor {
			d.replaceSelection("", _EditNone)
			r.Edited = true
		}
	default:
		deleting := cmd.Op == _EditDeleteBackward || cmd.Op == _EditDeleteForward || cmd.Op == _EditDeleteWordBackward || cmd.Op == _EditDeleteWordForward || cmd.Op == _EditDeleteToLineStart
		if !cmd.Extend && d.anchor != d.cursor && (cmd.Op == _EditMoveLeft || cmd.Op == _EditMoveRight) {
			selection := LineSelection{Anchor: d.anchor, Head: d.cursor}
			from, to := selection.Ordered()
			if cmd.Op == _EditMoveRight {
				from = to
			}
			d.SetSelection(from, from)
			r.Caret = true
			break
		}
		if deleting && d.anchor != d.cursor {
			d.replaceSelection("", _EditNone)
			r.Edited = true
			break
		}
		original := cmd.Op
		switch cmd.Op {
		case _EditDeleteBackward:
			cmd.Op = _EditMoveLeft
		case _EditDeleteForward:
			cmd.Op = _EditMoveRight
		case _EditDeleteWordBackward:
			cmd.Op = _EditMoveWordLeft
		case _EditDeleteWordForward:
			cmd.Op = _EditMoveWordRight
		case _EditDeleteToLineStart:
			cmd.Op = _EditMoveLineStart
		}
		e := _EditState{Runes: []rune(d.Line(d.cursor.Line)), Cursor: d.cursor.Rune, Anchor: d.cursor.Rune, Bounds: bounds}
		target := d.cursor
		left := cmd.Op == _EditMoveLeft || cmd.Op == _EditMoveWordLeft
		right := cmd.Op == _EditMoveRight || cmd.Op == _EditMoveWordRight
		word := cmd.Op == _EditMoveWordLeft || cmd.Op == _EditMoveWordRight
		// Word motion treats line separators and indentation as whitespace.
		// Skip whole blank prefixes/suffixes before applying the shared word rule.
		if word {
			for {
				i := e.Cursor
				if left {
					for i > 0 && runeClass(e.Runes[i-1]) == classSpace {
						i--
					}
				} else {
					for i < len(e.Runes) && runeClass(e.Runes[i]) == classSpace {
						i++
					}
				}
				if left && i == 0 && target.Line > 0 {
					target.Line--
					target.Rune = utf8.RuneCountInString(d.Line(target.Line))
				} else if right && i == len(e.Runes) && target.Line < d.LineCount()-1 {
					target.Line++
					target.Rune = 0
				} else {
					break
				}
				e = _EditState{Runes: []rune(d.Line(target.Line)), Cursor: target.Rune, Anchor: target.Rune}
			}
		}
		if !word && ((left && e.Cursor == 0 && target.Line > 0) || (right && e.Cursor == len(e.Runes) && target.Line < d.LineCount()-1)) {
			if left {
				target.Line--
				target.Rune = utf8.RuneCountInString(d.Line(target.Line))
			} else {
				target.Line++
				target.Rune = 0
			}
			cmd.Op = _EditNone
		}
		if cmd.Op != _EditNone {
			e.Apply(cmd)
			target.Rune = e.Cursor
			if cmd.Op == _EditSelectWord {
				d.SetSelection(LinePos{target.Line, e.Anchor}, target)
				r.Caret = true
				return
			}
		}
		if deleting {
			if target != d.cursor {
				a, b := d.offset(target), d.offset(d.cursor)
				d.replace(min(a, b), max(a, b), "", original)
				r.Edited = true
			}
		} else {
			anchor := target
			if cmd.Extend {
				anchor = d.anchor
			}
			d.SetSelection(anchor, target)
			r.Caret = true
		}
	}
	r.Caret = r.Caret || r.Edited
	return
}
