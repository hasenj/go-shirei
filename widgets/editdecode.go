package widgets

// Keyboard → edit-command decoding: the input-analysis half of the
// text input, kept apart from both the pure model (editcore.go) and
// the widget shell (textinput.go). A pure function of one frame's
// input values, so key bindings are testable as tables without fonts
// or frames. Design contract: notes/textinput-architecture.md.

import (
	"runtime"
	"strings"

	. "go.hasen.dev/shirei"
)

type editKeyOpts struct {
	UpDownLineEdges bool // Up/Down jump to edges, the single-line convention
	VerticalMotion  bool // Up/Down become geometry-resolved vertical moves
	Newlines        bool // plain Enter and pasted newlines insert '\n'
}

// sanitizeEditText makes arbitrary incoming text (typed or pasted —
// both arrive as FrameInput.Text) safe for the configured input mode.
// Single-line fields turn newlines and tabs into spaces; multiline
// fields keep '\n' and '\t' while normalizing CRLF/CR to LF. Other
// control runes are always dropped.
func sanitizeEditText(s string, keepNewlines bool) string {
	clean := true
	for _, r := range s {
		switch {
		case keepNewlines && (r == '\n' || r == '\t'):
		case r < 0x20 || r == 0x7f:
			clean = false
		}
		if !clean {
			break
		}
	}
	if clean {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case keepNewlines && r == '\n':
			b.WriteRune('\n')
		case keepNewlines && r == '\r':
			b.WriteRune('\n')
		case keepNewlines && r == '\t':
			b.WriteRune('\t')
		case !keepNewlines && (r == '\n' || r == '\r' || r == '\t'):
			b.WriteRune(' ')
		case r < 0x20 || r == 0x7f:
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sanitizeSingleLine(s string) string { return sanitizeEditText(s, false) }

// the primary shortcut modifier: Cmd on macOS, Ctrl elsewhere
var editPrimaryMod = func() Modifiers {
	if runtime.GOOS == "darwin" {
		return ModCmd
	}
	return ModCtrl
}()

// decodeEditKeys turns one frame's key/modifiers/typed-text into edit
// commands, in application order. Clipboard combos require the primary
// modifier exactly (Cmd+Shift+V is not paste — matches the historical
// exact-combo matching).
//
// Motions and deletions follow the modifier decode rule
// (notes/textinput-plan.md): shift always means extend; the remaining
// modifier picks granularity — none = char, Alt/Option = word, and the
// primary modifier = line edge on mac (Cmd+arrows) or word elsewhere
// (Ctrl+arrows). Home/End are line edges directly (mac laptops send
// them for fn+Left/Right). Arrow chords outside the rule (e.g.
// Cmd+Alt+Left) do nothing rather than falling back to a char step.
func decodeEditKeys(key KeyCode, mods Modifiers, text string, primary Modifiers, opts editKeyOpts) (cmds []_EditCommand) {
	if mods == primary {
		switch key {
		case KeyV:
			cmds = append(cmds, _EditCommand{Op: _EditPaste})
		case KeyC:
			cmds = append(cmds, _EditCommand{Op: _EditCopy})
		case KeyX:
			cmds = append(cmds, _EditCommand{Op: _EditCut})
		case KeyA:
			cmds = append(cmds, _EditCommand{Op: _EditSelectAll})
		case KeyZ:
			cmds = append(cmds, _EditCommand{Op: _EditUndo})
		case KeyY:
			if primary == ModCtrl { // Ctrl+Y redo, the win/linux convention
				cmds = append(cmds, _EditCommand{Op: _EditRedo})
			}
		}
	}
	if mods == primary|ModShift && key == KeyZ {
		cmds = append(cmds, _EditCommand{Op: _EditRedo})
	}

	extend := mods&ModShift != 0
	motion := mods &^ ModShift
	word := motion == ModAlt || (primary == ModCtrl && motion == ModCtrl)
	lineEdge := primary == ModCmd && motion == ModCmd

	emit := func(op _EditOp) {
		cmds = append(cmds, _EditCommand{Op: op, Extend: extend})
	}

	switch key {
	case KeyLeft:
		switch {
		case lineEdge:
			emit(_EditMoveLineStart)
		case word:
			emit(_EditMoveWordLeft)
		case motion == 0:
			emit(_EditMoveLeft)
		}
	case KeyRight:
		switch {
		case lineEdge:
			emit(_EditMoveLineEnd)
		case word:
			emit(_EditMoveWordRight)
		case motion == 0:
			emit(_EditMoveRight)
		}
	case KeyHome:
		emit(_EditMoveLineStart)
	case KeyEnd:
		emit(_EditMoveLineEnd)
	case KeyUp:
		switch {
		case opts.VerticalMotion && motion == primary:
			cmds = append(cmds, _EditCommand{Op: _EditMoveTo, Pos: 0, Extend: extend})
		case opts.VerticalMotion && motion == 0:
			emit(_EditMoveUp)
		case opts.UpDownLineEdges && motion == 0:
			emit(_EditMoveLineStart)
		}
	case KeyDown:
		switch {
		case opts.VerticalMotion && motion == primary:
			cmds = append(cmds, _EditCommand{Op: _EditMoveTo, Pos: int(^uint(0) >> 1), Extend: extend})
		case opts.VerticalMotion && motion == 0:
			emit(_EditMoveDown)
		case opts.UpDownLineEdges && motion == 0:
			emit(_EditMoveLineEnd)
		}
	case KeyEnter:
		if opts.Newlines && mods == 0 {
			cmds = append(cmds, _EditCommand{Op: _EditInsert, Text: "\n"})
		}
		// Enter is handled as a key, not text. That avoids duplicate
		// newlines from backends that report both and keeps modified
		// Enter chords unbound for app-level submit conventions.
		text = ""
	case KeyDeleteBackward:
		switch {
		case lineEdge:
			emit(_EditDeleteToLineStart)
		case word:
			emit(_EditDeleteWordBackward)
		case motion == 0:
			emit(_EditDeleteBackward)
		}
	case KeyDeleteForward:
		switch {
		case word:
			emit(_EditDeleteWordForward)
		case motion == 0:
			emit(_EditDeleteForward)
		}
	}

	if text != "" {
		text = sanitizeEditText(text, opts.Newlines)
		if text != "" {
			cmds = append(cmds, _EditCommand{Op: _EditInsert, Text: text})
		}
	}
	return cmds
}
