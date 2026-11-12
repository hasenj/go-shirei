# window

Shirei **extension**: manage desktop window geometry and placement (positioning,
centering, and enforcing minimum window dimensions) on macOS, Windows, Linux
Wayland, and Linux X11.

Module: `go.hasen.dev/shirei/ext/window`

## Usage

Functions can be called during setup (after `app.SetupWindow`), or dynamically
on the UI thread inside `frameFn` or event handlers. Setup requests remain queued
until the first frame and belong to that window; each popup has independent
geometry and minimum-size state. `Activate` can also be called from a background
goroutine and always targets the main window on macOS, Windows, and Wayland.

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

	// Optional: set the current content size
	window.SetSize(800, 600)

	app.Run(root)
}

func root() {
	Container(Attrs(Viewport, Pad(16)), func() {
		Label("Desktop window with enforced min-size and placement")
	})
}
```

## Functions

* `window.Center()`: Center the window on the display (best-effort across desktop platforms; supported on Wayland layer windows; ignored on Wayland toplevels and mobile).
* `window.Position(x, y int)`: Position the top-left corner of the window at `(x, y)` in screen coordinates (logical points).
* `window.SetMinSize(minWidth, minHeight float32)`: Enforce minimum window dimensions (in logical points). Does not shrink an already-larger window.
* `window.DragMove(position *shirei.Vec2)`: Make the current container a primary-button drag handle. Keep `position` synchronized with other window-position controls; call every frame inside the handle. Wayland requires layer-shell; positioned layer windows support both relative and absolute pointers.
* `window.DragMoveNonactivating(position *shirei.Vec2)`: Like `DragMove` for a window opened with `PopupWindowOptions.Nonactivating`; the drag starts even when the app is inactive, so a floating widget can move without taking focus.
* `window.CurrentPosition() (shirei.Vec2, bool)`: Return the current window's top-left corner in the coordinates used by `Position`. Call on the UI thread inside a frame function; returns false when the backend cannot report a position.
* `window.Hovered() bool`: Report whether the pointer is inside the current window's content. Best-effort on macOS, Windows, and X11; Wayland reports pointer focus on the window's surface, including non-activating windows. Mobile returns false. Call on the UI thread.
* `window.Activate()`: Bring the main window forward. Wayland restores a hidden background window and requests activation; the compositor can deny focus.
* `window.SetSize(width, height float32)`: Request the current content size (logical points). Wayland supports layer windows and ordinary floating windows; ordinary requests are discarded while the compositor reports tiling, maximization, fullscreen, or interactive resizing. They are not retried after restoring the window. Mobile backends ignore it.

## Mechanism

`window` uses `Host.EscapeHatchBackendContext` to interact with native window
handles:

* **macOS (`cocoabackend`)**: Uses `[NSWindow center]`, `[NSWindow setFrameTopLeftPoint:]`, `[NSWindow setContentMinSize:]`, and `[NSWindow setContentSize:]`.
* **Windows (`win32backend`)**: Uses `SetWindowPos` for centering and positioning; subclasses via `SetWindowSubclass` for `WM_GETMINMAXINFO` (`ptMinTrackSize`), adjusting for DPI scale.
* **Linux X11 (`x11backend`)**: Uses `xproto.ConfigureWindow` for positioning/centering; sets `WM_NORMAL_HINTS` (`PMinSize`) for minimum size.
* **Linux Wayland (`waylandbackend`)**: Calls `xdg_toplevel.set_min_size`. Layer windows support size, centering, and placement between outputs; surface replacement during a drag waits for release. Ordinary windows support content-size requests subject to compositor constraints, while positioning is compositor-managed. See [Wayland windows](../../docs/wayland-windows.md).
* **Other / Mobile / Headless**: Graceful no-op.

## A custom drag handle

Keep the requested screen position as application state. The same handle code
works across desktop backends:

```go
var origin = shirei.Vec2{40, 80}

func title() {
    shirei.Container(shirei.Attrs(shirei.FixHeight(40)), func() {
        window.DragMove(&origin)
        shirei.Label("Drag here")
    })
}
```

Set the initial native origin with `window.Position` during setup, and update
`origin` when another control repositions the window. Wayland layer windows use
accelerated relative-pointer motion when the device supplies it. Absolute
pointers use ordered position-commit markers to keep screen coordinates stable
across asynchronous window movement.
Other backends preserve the grabbed local point as the native window moves.
Ordinary Wayland toplevels do not support explicit position requests.
