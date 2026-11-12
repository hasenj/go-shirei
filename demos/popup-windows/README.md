# Native popup windows

Run from the repository root:

```sh
go run ./shirei/demos/popup-windows
SHIREI_GPU=0 go run ./shirei/demos/popup-windows
```

The demo opens an ordinary utility window and a transparent, undecorated floating
panel. It uses the same source and controls on macOS, Windows, Wayland, and X11;
individual requests follow the backend's capabilities and desktop policy.

Use **Utility window** and **Floating panel** for quick comparisons. The configured
popup controls let you combine transparency, decorations, floating, top-level,
and focus requests. Choose default placement, owner-relative offsets, or screen
coordinates, with optional exact placement.

Each panel has independent text input and opacity, screen-position sliders with
**Move panel**, centering, size buttons, and **Activate main**. **Open child**
creates a panel owned by the calling panel; closing an owner also closes its
children. Shared clicks update every window. **Close panel** and **Quit** work
without native decorations. Panels scroll when their contents do not fit.

To explore startup and lifetime options:

```sh
# Begin with just the controls window
go run ./shirei/demos/popup-windows -open=false

# Request background lifetime and quiet startup
go run ./shirei/demos/popup-windows -background -quiet

# Brief unattended smoke run
go run ./shirei/demos/popup-windows -close-after=5s
```

With background lifetime enabled, use the native main-window close control and
**Activate main** in a surviving panel to explore the backend's close/restore
behavior. **Quit** exits the whole app from any window. Ordinary startup uses the
backend's default close behavior.

```go
var popup app.PopupWindow
popup, err := app.OpenPopupWindow(app.PopupWindowOptions{
    Title:       "Inspector",
    Width:       400,
    Height:      340,
    Transparent: true,
    Undecorated: true,
    OnClose:     func() { popup = 0 },
}, func() {
    // Build an ordinary Shirei root view here.
    if widgets.Button(widgets.NoIcon, "Close") {
        app.ClosePopupWindow(popup)
    }
})
```

Open once from a UI interaction after `app.Run` starts. The calling window is
the owner, and the first popup frame runs after `OpenPopupWindow` returns.
Opening returns an error for invalid dimensions, a nil root, an unavailable
owner, an off-thread call, or an unsupported backend.

Sizes use logical content points. `Position: &shirei.Vec2{x, y}` places the
panel's top-left relative to the owner's content top-left. Omit it to center
over the owner. Placement is clamped to that display's visible area on macOS
and Windows. On Wayland, `Position` applies to layer popups with explicitly
positioned layer owners; other windows use compositor placement.
`SetupTransparent` and `SetupUndecorated` configure only the main window.

On macOS, popups are keyboard-capable `NSPanel` windows with tabbing disabled.
They float above their owner, hide when the app deactivates, and remain open
when focus changes. On Windows, popups use owned `WS_POPUP | WS_EX_TOOLWINDOW`
windows with no separate taskbar or Alt-Tab entry. They stay above their owner
and remain open when focus changes.

On Wayland, undecorated panels use centered top-layer surfaces when layer-shell
v4 is available. They have no normal window chrome or task-switcher entry,
reserve no desktop space, stay above normal apps across workspaces, and accept
keyboard focus on demand. Supply a close button and use `window.SetSize` for size
controls; native move/resize operations do not apply. Main windows select the
same role with `app.SetupUndecorated()`.

`Floating` selects a top-layer surface, including client-drawn chrome for a
decorated panel. `TopLevel` selects an ordinary toplevel even for an undecorated
panel, unless `Floating` is also true. `ScreenPosition`, `ExactScreenPosition`,
and `app.MovePopupWindow` support placement within a layer window's initial output.
See [Wayland window controls](../../docs/wayland-windows.md) for coordinates,
activation, and background behavior.

Other decorated panels, and panels without layer-shell v4, use toplevels.
A native parent is set when both owner and child are toplevels and the child
does not request `TopLevel`. App ownership
and descendant cleanup apply to both roles. Focus loss leaves panels open.
Compositor policy controls toplevel placement, activation, stacking, task-switcher
entries, and chrome. `SHIREI_LAYER_SHELL=0` selects toplevels explicitly.

Each panel owns its widget identity, focus, text input,
accessibility tree, and rendering state. Application data and resources are
shared; call `shirei.RequestNextFrame()` after changing data shown in other
windows. The color scheme is application-wide.

Closing is deferred and safe from any goroutine. Zero, stale, and already-closed
handles are harmless, including a close before the first frame. `OnClose` runs
once on the UI thread, in the owner's context (or the main window's if the owner
also closes). Closing the main window closes its panels and exits by default.
`app.SetupBackground()` keeps the process running and hides the Wayland main
window while its UI and auxiliary windows remain available.
Application quit (including `app.Quit()` and Cmd-Q) exits the process directly
and does not call popup `OnClose` hooks.

Native popup windows are supported on macOS, Windows, Wayland, and X11. Other backends return
an error. Windows supports D3D11 and software rendering; transparent windows
preserve per-pixel alpha with `UpdateLayeredWindow`.

The behavior harness exercises simultaneous styles, independent text and
composition, and repeated owner/child cleanup. macOS and Windows also check
native pixels, accessibility ownership, and resizing. Wayland checks distinct
native roles, per-window scenes, and layer resizing; physical input and
compositor interaction need manual review:

```sh
go -C shirei/behavior_test run ./popup-windows --close
go -C shirei/behavior_test run ./popup-windows --owner-exit

# Exercise a layer-shell main window and mixed-role popups on Wayland
go -C shirei/behavior_test run ./popup-windows --close -undecorated -require-layer-shell

# Require GPU rendering in every window
go -C shirei/behavior_test run ./popup-windows --close -require-gpu
```

With Wine installed, cross-build the demo or harness and run the executable:

```sh
GOOS=windows GOARCH=amd64 go build -o /tmp/shirei-popups.exe ./shirei/demos/popup-windows
SHIREI_GPU=0 wine /tmp/shirei-popups.exe
```

Wine smoke tests cover window lifecycle and software presentation. Use native
Windows to check the actual D3D11 driver, desktop compositor, task switching,
physical mixed-DPI displays, and interactive IME candidate windows.

Decorated toplevels stay below top-layer windows, including their own layer
owners. Use undecorated popups when a layer main window needs auxiliary panels
in the same desktop layer. Ordering between surfaces in that layer follows
compositor policy.
