package widgets

import (
	"slices"
	"testing"

	. "go.hasen.dev/shirei"
)

// decodeEditKeys is pure, so key bindings are pinned as tables — no
// fonts, frames, or focus. Both primary-modifier mappings (Cmd on mac,
// Ctrl elsewhere) are exercised regardless of the host platform.

func TestDecodeEditKeys(t *testing.T) {
	cases := []struct {
		name     string
		key      KeyCode
		mods     Modifiers
		text     string
		primary  Modifiers
		noUpDown bool
		vertical bool
		newlines bool
		want     []_EditCommand
	}{
		// char motion and deletion
		{name: "left", key: KeyLeft, primary: ModCmd,
			want: []_EditCommand{{Op: _EditMoveLeft}}},
		{name: "shift+right extends", key: KeyRight, mods: ModShift, primary: ModCmd,
			want: []_EditCommand{{Op: _EditMoveRight, Extend: true}}},
		{name: "backspace", key: KeyDeleteBackward, primary: ModCmd,
			want: []_EditCommand{{Op: _EditDeleteBackward}}},
		{name: "forward delete", key: KeyDeleteForward, primary: ModCmd,
			want: []_EditCommand{{Op: _EditDeleteForward}}},
		{name: "typed text", key: KeyA, text: "a", primary: ModCmd,
			want: []_EditCommand{{Op: _EditInsert, Text: "a"}}},
		{name: "typed text is sanitized as single-line", key: KeyCodeNone, text: "a\nb\tc", primary: ModCmd,
			want: []_EditCommand{{Op: _EditInsert, Text: "a b c"}}},
		{name: "empty sanitized text emits no insert", key: KeyCodeNone, text: "\x00\x7f", primary: ModCmd,
			want: nil},

		// word granularity: Option everywhere, Ctrl where Ctrl is primary
		{name: "opt+left = word left", key: KeyLeft, mods: ModAlt, primary: ModCmd,
			want: []_EditCommand{{Op: _EditMoveWordLeft}}},
		{name: "opt+shift+right extends by word", key: KeyRight, mods: ModAlt | ModShift, primary: ModCmd,
			want: []_EditCommand{{Op: _EditMoveWordRight, Extend: true}}},
		{name: "ctrl+left = word left (non-mac)", key: KeyLeft, mods: ModCtrl, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditMoveWordLeft}}},
		{name: "opt+backspace = delete word", key: KeyDeleteBackward, mods: ModAlt, primary: ModCmd,
			want: []_EditCommand{{Op: _EditDeleteWordBackward}}},
		{name: "ctrl+backspace = delete word (non-mac)", key: KeyDeleteBackward, mods: ModCtrl, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditDeleteWordBackward}}},
		{name: "opt+forward-delete = delete word forward", key: KeyDeleteForward, mods: ModAlt, primary: ModCmd,
			want: []_EditCommand{{Op: _EditDeleteWordForward}}},

		// line edges: Cmd+arrows on mac, Home/End everywhere
		// (mac laptops send Home/End for fn+Left/Right)
		{name: "cmd+left = line start (mac)", key: KeyLeft, mods: ModCmd, primary: ModCmd,
			want: []_EditCommand{{Op: _EditMoveLineStart}}},
		{name: "cmd+shift+right extends to line end", key: KeyRight, mods: ModCmd | ModShift, primary: ModCmd,
			want: []_EditCommand{{Op: _EditMoveLineEnd, Extend: true}}},
		{name: "home", key: KeyHome, primary: ModCmd,
			want: []_EditCommand{{Op: _EditMoveLineStart}}},
		{name: "shift+end extends", key: KeyEnd, mods: ModShift, primary: ModCmd,
			want: []_EditCommand{{Op: _EditMoveLineEnd, Extend: true}}},
		{name: "ctrl+home = document start (non-mac)", key: KeyHome, mods: ModCtrl, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditMoveTo, Pos: 0}}},
		{name: "ctrl+shift+home extends to document start (non-mac)", key: KeyHome, mods: ModCtrl | ModShift, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditMoveTo, Pos: 0, Extend: true}}},
		{name: "ctrl+end = document end (non-mac)", key: KeyEnd, mods: ModCtrl, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditMoveTo, Pos: int(^uint(0) >> 1)}}},
		{name: "ctrl+shift+end extends to document end (non-mac)", key: KeyEnd, mods: ModCtrl | ModShift, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditMoveTo, Pos: int(^uint(0) >> 1), Extend: true}}},
		{name: "cmd+backspace = delete to line start (mac)", key: KeyDeleteBackward, mods: ModCmd, primary: ModCmd,
			want: []_EditCommand{{Op: _EditDeleteToLineStart}}},

		// up/down are line edges unless the widget reserves them
		{name: "up = line start", key: KeyUp, primary: ModCmd,
			want: []_EditCommand{{Op: _EditMoveLineStart}}},
		{name: "shift+down extends to end", key: KeyDown, mods: ModShift, primary: ModCmd,
			want: []_EditCommand{{Op: _EditMoveLineEnd, Extend: true}}},
		{name: "up reserved for the app", key: KeyUp, primary: ModCmd, noUpDown: true,
			want: nil},

		// multiline opts: Up/Down are geometry commands, and primary
		// Up/Down target document edges with MoveTo.
		{name: "vertical up", key: KeyUp, primary: ModCmd, vertical: true,
			want: []_EditCommand{{Op: _EditMoveUp}}},
		{name: "vertical shift+down extends", key: KeyDown, mods: ModShift, primary: ModCmd, vertical: true,
			want: []_EditCommand{{Op: _EditMoveDown, Extend: true}}},
		{name: "cmd+up = document start", key: KeyUp, mods: ModCmd, primary: ModCmd, vertical: true,
			want: []_EditCommand{{Op: _EditMoveTo, Pos: 0}}},
		{name: "cmd+shift+down extends to document end", key: KeyDown, mods: ModCmd | ModShift, primary: ModCmd, vertical: true,
			want: []_EditCommand{{Op: _EditMoveTo, Pos: int(^uint(0) >> 1), Extend: true}}},

		// chords outside the rule do nothing (no char-step fallback)
		{name: "cmd+alt+left is nothing", key: KeyLeft, mods: ModCmd | ModAlt, primary: ModCmd,
			want: nil},
		{name: "ctrl+left on mac is nothing", key: KeyLeft, mods: ModCtrl, primary: ModCmd,
			want: nil},

		// undo/redo
		{name: "cmd+z undoes", key: KeyZ, mods: ModCmd, primary: ModCmd,
			want: []_EditCommand{{Op: _EditUndo}}},
		{name: "cmd+shift+z redoes", key: KeyZ, mods: ModCmd | ModShift, primary: ModCmd,
			want: []_EditCommand{{Op: _EditRedo}}},
		{name: "ctrl+z undoes (non-mac)", key: KeyZ, mods: ModCtrl, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditUndo}}},
		{name: "ctrl+y redoes (non-mac)", key: KeyY, mods: ModCtrl, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditRedo}}},
		{name: "ctrl+shift+z redoes (non-mac)", key: KeyZ, mods: ModCtrl | ModShift, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditRedo}}},
		{name: "cmd+y is nothing on mac", key: KeyY, mods: ModCmd, primary: ModCmd,
			want: nil},

		// clipboard combos: primary modifier, exactly
		{name: "cmd+v pastes", key: KeyV, mods: ModCmd, primary: ModCmd,
			want: []_EditCommand{{Op: _EditPaste}}},
		{name: "cmd+c copies", key: KeyC, mods: ModCmd, primary: ModCmd,
			want: []_EditCommand{{Op: _EditCopy}}},
		{name: "cmd+x cuts", key: KeyX, mods: ModCmd, primary: ModCmd,
			want: []_EditCommand{{Op: _EditCut}}},
		{name: "cmd+a selects all", key: KeyA, mods: ModCmd, primary: ModCmd,
			want: []_EditCommand{{Op: _EditSelectAll}}},
		{name: "ctrl+v pastes (non-mac)", key: KeyV, mods: ModCtrl, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditPaste}}},
		{name: "shift+insert pastes (non-mac)", key: KeyInsert, mods: ModShift, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditPaste}}},
		{name: "ctrl+insert copies (non-mac)", key: KeyInsert, mods: ModCtrl, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditCopy}}},
		{name: "shift+delete cuts (non-mac)", key: KeyDeleteForward, mods: ModShift, primary: ModCtrl,
			want: []_EditCommand{{Op: _EditCut}}},
		{name: "ctrl+shift+insert is nothing", key: KeyInsert, mods: ModCtrl | ModShift, primary: ModCtrl,
			want: nil},
		{name: "plain insert is nothing", key: KeyInsert, primary: ModCtrl,
			want: nil},
		{name: "cmd+shift+v is not paste", key: KeyV, mods: ModCmd | ModShift, primary: ModCmd,
			want: nil},
		{name: "ctrl+v on mac is not paste", key: KeyV, mods: ModCtrl, primary: ModCmd,
			want: nil},
		{name: "shift+insert on mac is nothing", key: KeyInsert, mods: ModShift, primary: ModCmd,
			want: nil},
		{name: "ctrl+insert on mac is nothing", key: KeyInsert, mods: ModCtrl, primary: ModCmd,
			want: nil},
		{name: "plain v with text is just insert", key: KeyV, text: "v", primary: ModCmd,
			want: []_EditCommand{{Op: _EditInsert, Text: "v"}}},

		// newline insertion is opt-in for multiline fields
		{name: "enter ignored in single-line fields", key: KeyEnter, primary: ModCmd,
			want: nil},
		{name: "plain enter inserts newline when enabled", key: KeyEnter, primary: ModCmd, newlines: true,
			want: []_EditCommand{{Op: _EditInsert, Text: "\n"}}},
		{name: "enter key text does not duplicate newline", key: KeyEnter, text: "\n", primary: ModCmd, newlines: true,
			want: []_EditCommand{{Op: _EditInsert, Text: "\n"}}},
		{name: "modified enter remains unbound", key: KeyEnter, mods: ModCmd, primary: ModCmd, newlines: true,
			want: nil},
		{name: "modified enter text remains unbound", key: KeyEnter, mods: ModCmd, text: "\n", primary: ModCmd, newlines: true,
			want: nil},
		{name: "multiline text keeps newlines and tabs", key: KeyCodeNone, text: "a\r\nb\tc\x00d", primary: ModCmd, newlines: true,
			want: []_EditCommand{{Op: _EditInsert, Text: "a\nb\tcd"}}},

		{name: "no input, no commands", key: KeyCodeNone, primary: ModCmd,
			want: nil},
	}
	for _, c := range cases {
		opts := editKeyOpts{UpDownLineEdges: !c.noUpDown}
		if c.vertical {
			opts.VerticalMotion = true
			opts.UpDownLineEdges = false
		}
		if c.newlines {
			opts.Newlines = true
		}
		got := decodeEditKeys(c.key, c.mods, c.text, c.primary, opts)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: decode = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestSanitizeSingleLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"日本語 ok", "日本語 ok"},
		{"a\nb", "a b"},
		{"a\r\nb", "a b"}, // CRLF is one break, one space
		{"a\rb", "a b"},
		{"a\tb", "a b"},
		{"one\ntwo\r\nthree", "one two three"},
		{"a\x1b[0m", "a[0m"}, // escape dropped, printable rest kept
		{"\x7f\x00", ""},
	}
	for _, c := range cases {
		if got := sanitizeSingleLine(c.in); got != c.want {
			t.Errorf("sanitizeSingleLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeMultiline(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"a\nb", "a\nb"},
		{"a\r\nb", "a\nb"}, // CRLF is one break
		{"a\rb", "a\nb"},
		{"a\tb", "a\tb"},
		{"a\x1b[0m", "a[0m"}, // escape dropped, printable rest kept
		{"\x7f\x00", ""},
	}
	for _, c := range cases {
		if got := sanitizeEditText(c.in, true); got != c.want {
			t.Errorf("sanitizeEditText(%q, true) = %q, want %q", c.in, got, c.want)
		}
	}
}
