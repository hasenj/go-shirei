# window

Shirei **extension**: manage desktop window geometry and placement (positioning,
centering, and enforcing minimum window dimensions) on macOS, Windows, Linux
Wayland, and Linux X11.

Module: `go.hasen.dev/shirei/ext/window`

## Usage

Functions can be called during setup (after `app.SetupWindow`), or dynamically
at runtime inside `frameFn` or event handlers.

```go
package main

import (
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/ext/window"
	. "go.hasen.dev/shirei"
)

func main() {
	app.SetupWindow("My App", 800, 600)

	// Center the window on the display
	window.Center()

	// Enforce a minimum window size of 400×300 logical points
	window.SetMinSize(400, 300)

	app.Run(root)
}

func root() {
	Container(Attrs(Viewport, Pad(16)), func() {
		Label("Desktop window with enforced min-size and placement")
	})
}
```

## Functions

* `window.Center()`: Center the window on the display (best-effort across desktop platforms; ignored on Wayland and mobile).
* `window.Position(x, y int)`: Position the top-left corner of the window at `(x, y)` in screen coordinates (logical points).
* `window.SetMinSize(minWidth, minHeight float32)`: Enforce minimum window dimensions (in logical points).

## Mechanism

`window` uses `Host.EscapeHatchBackendContext` to interact with native window
handles:

* **macOS (`cocoabackend`)**: Uses `[NSWindow center]`, `[NSWindow setFrameTopLeftPoint:]`, and `[NSWindow setContentMinSize:]`.
* **Windows (`win32backend`)**: Uses `SetWindowPos` for centering and positioning; subclasses via `SetWindowSubclass` for `WM_GETMINMAXINFO` (`ptMinTrackSize`), adjusting for DPI scale.
* **Linux X11 (`x11backend`)**: Uses `xproto.ConfigureWindow` for positioning/centering; sets `WM_NORMAL_HINTS` (`PMinSize`) for minimum size.
* **Linux Wayland (`waylandbackend`)**: Calls `xdg_toplevel.set_min_size`; top-level window positioning is compositor-managed.
* **Other / Mobile / Headless**: Graceful no-op.
