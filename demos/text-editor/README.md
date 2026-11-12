# Text Editor

A standalone document editor with open/new/save, line numbers, two-way
scrolling, selection, clipboard, undo/redo, and Japanese IME input.

From the `shirei` directory:

```sh
go run ./demos/text-editor
go run ./demos/text-editor path/to/file.go
go run ./demos/text-editor -png /tmp/text-editor.png
```

The initial playground has 2,009 lines. **Open…** chooses a UTF-8 file;
**New** creates an empty document. Enter a new path in the toolbar to save an
untitled document or a copy. Existing destinations are protected.

Cmd/Ctrl+S saves, Cmd/Ctrl+O opens the picker, and Cmd/Ctrl+Z undoes.
Tab inserts indentation; Shift+Tab leaves the editing surface. New/open asks
before replacing an unsaved buffer. Normal quit stores unsaved text for the
next launch in `shirei-text-editor/editor-drafts.json` under the user cache.
`SHIREI_TEXT_EDITOR_RECOVERY_PATH` selects another recovery file for an isolated
session.

See the [document editor API](../../docs/text-editor.md) for storage, layout,
file-format limits, and the extent of draft recovery.
