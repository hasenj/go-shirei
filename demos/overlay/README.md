# Desktop overlay demo

A small transparent card demonstrates `app.SetupOverlay()` on macOS, Windows,
and Linux. The demo uses the same Go code on every desktop platform.

From the repository root:

```sh
go run ./shirei/demos/overlay
```

Switch to another application and move its window behind the card. On supported
desktops the card stays above ordinary windows even while the other app has
keyboard focus. Click the card's text field to type, try the click counter, and
lower the opacity to see the window underneath. Drag the title area to move the
card; the controls below it keep their usual mouse behavior. The title uses
`window.DragMove` with a shared logical screen position. Reset position
returns the card near the upper-left corner. Close exits the process.

The overlay occupies a small, movable window and reserves no desktop space.

## Launch options

```sh
# Open without requesting keyboard focus.
go run ./shirei/demos/overlay -quiet

# In a second terminal, open an ordinary decorated window for comparison.
go run ./shirei/demos/overlay -normal

# Exit automatically for a launch check.
go run ./shirei/demos/overlay -quiet -close-after=5s
```

`SHIREI_GPU=0` selects software rendering. On Windows PowerShell, set
`$env:SHIREI_GPU = '0'` before running the command.

## Desktop behavior

| Desktop | Overlay behavior |
| --- | --- |
| macOS | Floats above normal windows and joins all Spaces. |
| Windows | Stays above normal windows on its assigned virtual desktop. Fully transparent pixels pass mouse input through. |
| Wayland with layer-shell v4 | Uses a top-layer surface on one output, visible across workspaces. Click the card for keyboard focus. |
| Other Wayland compositors | Uses a transparent, undecorated toplevel; always-on-top placement, title dragging, and explicit positioning are unavailable. |
| X11 | Uses a transparent, undecorated window with ordinary stacking; transparency requires a compositor. |

On Wayland, use the card's title area and Close button: layer surfaces do not
appear in the normal task switcher or use ordinary compositor move shortcuts.
Title dragging supports relative mice and absolute pointers such as VM tablet
devices. Position-commit markers keep absolute coordinates aligned with the
window origin while the compositor applies moves asynchronously.
Moving to another output replaces the native surface after the primary button
is released. Until then, the overlay stays on its current output and can be
clipped at that output's edge. Rendering follows the destination's preferred
scale, including fractional scaling when supported by the compositor.
`SHIREI_LAYER_SHELL=0` selects the fallback toplevel path. Reset position has no
effect on Wayland toplevels, including the `-normal` comparison window.

Transparent areas follow the desktop's input rules. This is an interactive
overlay, not a window that passes all mouse input to the application below it.
Windows does not automatically pin the overlay to every virtual desktop.

The Windows implementation also runs under Wine, but checking native Windows
desktop behavior requires Windows itself.
With Wine 10.0 on macOS, quiet startup can activate the overlay despite the
non-activating window request.

## Drag diagnostics

Set `SHIREI_DRAG_TRACE` to a file path to record pointer events, drag calculations,
position requests, and Wayland commit-marker callbacks:

```sh
SHIREI_DRAG_TRACE=/tmp/shirei-overlay-drag.log go run ./shirei/demos/overlay
```

Each run replaces the file. Records share sequence numbers and monotonic
microsecond timestamps. The trace contains pointer coordinates and window state;
it does not record keyboard events or typed text. Tracing is disabled when the
environment variable is unset.
