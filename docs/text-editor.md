# Document text editing

`widgets.TextEditor` edits a persistent `TextDocument` in the space available
from its parent. It paints visible logical lines, with line numbers and both
scrollbars. `TextArea` is the string-bound choice for short form entries.

```go
var document widgets.TextDocument

func load(text string, line int) {
    document.SetText(text)
    pos := widgets.LinePos{Line: line - 1}
    document.SetSelection(pos, pos)
}

func view() {
    // Under the window root, the editor receives the height left by the toolbar.
    toolbar()
    widgets.TextEditor(&document, shirei.FontSize(14))
}
```

The parent needs a height budget. `Expand` stretches across the parent's axis;
it does not provide height in a column. Use `Grow(1)` with an extrinsic parent
when nesting the editor inside another panel. The editor itself uses `Viewport`.

## Document ownership

Keep the document in application state and pass its address on each frame.
Do not copy a populated document or call `SetText` on every frame.

| Operation | Meaning |
| --- | --- |
| `SetText(text)` | Load UTF-8 text, reset selection and history, mark clean |
| `LineCount()` / `Line(i)` | Read logical lines without joining the document |
| `Selection()` | Read anchor and caret as zero-based `LinePos` values |
| `SetSelection(anchor, caret)` | Clamp positions and request caret reveal |
| `SelectedText()` | Export the selected bytes |
| `ReplaceSelection(text)` | Apply one undoable replacement |
| `Undo()` / `Redo()` | Traverse edit groups, restoring their selection |
| `Modified()` / `MarkSaved()` | Compare or set the saved undo state |
| `Text()` / `WriteTo(writer)` | Export the whole document explicitly |

`LinePos.Rune` counts Unicode code points, not bytes or displayed columns.
`Line` excludes its LF or CRLF terminator. A trailing newline creates an empty,
editable final line. Existing line endings are preserved; inserted newlines
use the first newline's convention. Incoming CRLF/CR is normalized to that
convention by `ReplaceSelection`.

Selection and undo survive focus changes and hiding the widget. Scroll belongs
to the view; remounting reveals the document's caret. `SetSelection` does not
take focus. Applications can use `FocusImmediateOn(GetLastId())` after the
widget to focus an explicitly opened document.

## View options and styling

```go
state := widgets.TextEditorExt(&document, widgets.TextEditorAttrs{
    TextStyle: shirei.TextStyle(shirei.Fonts(shirei.Monospace...), shirei.FontSize(13)),
    LineNumbers: true,
    ReadOnly: false,
    NoAutoFocus: true,
})
```

`TextEditor` supplies line numbers and monospace fonts. In `TextEditorExt`,
the zero text style inherits the current style with monospace fonts; other
flags are independent. Read-only views retain navigation, selection, and copy.
`ReadOnly` can change each frame without replacing the document or its undo
history. It blocks typing, paste, cut, deletion, undo/redo and IME commits through
that view. Document methods remain available to the application; read-only is
an interaction policy, not a document lock.
`TextEditorStyled` accepts the existing `TextInputStyle` and `ScrollBarStyle`
types for explicit colors, including transparent values.

The returned `TextEditorState` reports focus, edits during this build, scroll,
visible line indexes, and the caret's screen rectangle. The document remains
the authority for text and selection.

## Gutter, decoration and navigation

`GutterWidth` reserves an extra slot before the line numbers. `GutterView(line)`
builds ordinary Shirei UI inside that slot for each visible zero-based line.
Gutter clicks do not change text selection. The width uses design units, like
the editor's font size.

`LineBackground(line)` supplies a row color behind the gutter and text.
`LineSpans(line, base)` returns `[]shirei.StyleSpan`, built with `ResolveSpan`
against the provided style. These spans paint text color, background, underline
and strike; they do not change font metrics. Their offsets use document rune
coordinates, including when an IME composition is displayed.

`OnPointer(pos)` receives the document position under the mouse over the text.
It can inspect the current input for modifier-click actions. Returning true
suppresses the editor's mouse selection handling for that frame. The callback
must not mutate the document or build UI; line decoration callbacks can use
the position it reports.

Call `TextEditorScrollToLine(&document, line, viewportFrac)` before building
the editor to reveal a zero-based line without moving the selection or taking
focus. A fraction of 0 aligns to the top, .5 to the middle, and a negative value
scrolls only if needed. Like virtual-list scroll commands, the last request
wins and a request to an unrendered view expires after one frame.

## Interaction

- Typing, Enter, Tab, Backspace, forward delete, word deletion, and selection
  replacement operate on the document. Shift+Tab leaves the editor.
- Arrows, Home/End, Page Up/Down, and the platform's document-edge chords move
  the caret. Shift extends selection; vertical movement keeps a preferred x.
- Click places the caret, double-click selects a word, triple-click selects a
  line. Dragging past the viewport scrolls while extending selection.
- Cmd/Ctrl+A/C/X/V/Z and redo use the same keyboard decoder as TextInput.
  Clipboard paste arrives through the host's normal text-input channel.
- Cluster-aware movement/deletion uses Shirei's shaping geometry. Japanese
  IME composition remains display state until its commit arrives as text.
- Wheel scrolling can leave the caret offscreen. Editing and caret movement
  reveal it again. Scrolling alone does not modify selection.

## Storage and scope

The document uses shared string pieces and integer line offsets. Loading does
not allocate a string per line. Replacements retain changed payloads for at
most 100 undo groups; small neighboring pieces coalesce. Index updates are
linear in line/piece count. Whole-document export is a deliberate operation.

Rendering shapes visible lines and navigation targets. Logical lines do not
soft-wrap; a very long individual line still requires whole-line shaping.
The editor provides plain text, one selection, and logical bidi movement.
Syntax highlighting, soft wrapping, multi-cursor editing, and language tools
are outside this API.

The optional [`editorfile`](../editorfile) package supplies file operations for
applications using `TextDocument`. The [standalone demo](../demos/text-editor)
and [Haystack](../examples/haystack) use this shared machinery. They accept
regular UTF-8 files up to 32 MiB, preserve LF/CRLF and permission bits, check
for conflicting disk changes, and write through a sibling temporary file.
Save-copy refuses to overwrite an existing destination. Atomic replacement
changes the file's inode; hard-link relationships and extended metadata are
not preserved. The conflict check does not lock out other writers.

Normal application exit records unsaved buffers in the user's cache directory
for the next launch. This recovery is for normal close/quit, not a periodic
crash-recovery journal. The example applications use one recovery session per
application; run one editing instance at a time for that recovery workflow.
