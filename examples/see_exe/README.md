# see_exe

Inspect a built Go executable: which modules landed in the binary, and how much
code each one accounts for.

![see_exe](see_exe.webp)

## What it does

Open a Go binary (or launch with no args and pick from a list of recent builds
under the current tree). You get:

- A stacked bar of code size (stdlib / main / large deps / small pool / rest)
- A sortable module table: code bytes, %, cumulative, function count, version, path
- Click a module for a “why is this here?” detail: require-chain breadcrumb and
  requires / required-by edges (from the module cache when available)

Data comes only from the binary and the standard library
(`debug/buildinfo`, `debug/gosym`, Mach-O/ELF). No toolchain and no source tree
required on the machine doing the inspection. Rebuilds are picked up via
`fsnotify` while the window stays open.

CLI extras: `--text` for a terminal report; a second argument for a module
“why” query without the GUI. PE (Windows) binaries may appear in the picker but
full size attribution is not there yet.

## What it shows (shirei)

A two-screen tool (picker → inspect), a draggable split, a dense table, and
file watching that feeds UI state.

### Screens as plain state

```go
if browsing {
    PickerView()
} else {
    InspectView()
}
```

No router object — a bool (and the loaded model) decide the tree. Esc returns
to the picker.

See `gui.go`: `RootView`, `InspectView`.

### Draggable split from layout primitives

Top and bottom panes get heights from a ratio; a thin middle container uses
`PressAction` + `IsActive` + `FrameInput.Motion` to drag the ratio. Same idea as
see_pprof’s sidebar/main splits.

See `gui.go`: `InspectView` (splitter block).

### Selection is a string in app state

`selectedPath` is written from the bar, the table, and the detail breadcrumb.
Every pane reads it and styles accordingly. Immediate mode: one field, many
views.

### Reload from a watcher

`watchExe` debounces filesystem events, reloads the model under the frame lock,
and requests a frame. Pattern for any “live file” tool.

See `gui.go`: `watchExe` / load path in `main.go`.

## Run it

```shell
go run .                         # picker; inside examples/see_exe
go run . /path/to/binary
go run . /path/to/binary --text
go run . /path/to/binary some.module/path
go run . --png out.png
```
