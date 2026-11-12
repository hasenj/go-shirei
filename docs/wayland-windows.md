# Wayland window controls

Shirei uses ordinary `xdg_toplevel` windows and optional layer-shell v4 surfaces.
Layer surfaces supply decorationless panels above normal applications. Their
stacking and workspace behavior differ from ordinary utility windows.

| Request | Wayland behavior |
| --- | --- |
| `Undecorated` | Uses a top-layer surface when layer-shell v4 is available. Otherwise requests an ordinary undecorated toplevel, subject to compositor policy. |
| `TopLevel` | Uses an ordinary toplevel without a native parent, including when undecorated. Logical ownership and descendant cleanup still apply. |
| `Floating` | Uses a top-layer surface, taking precedence over `TopLevel`. Decorated floating windows have client-drawn chrome. Without layer-shell, stacking follows compositor policy. |
| `Focus` | Requests activation after the popup presents its first frame. Focus remains compositor-controlled. |
| `ScreenPosition` | Positions a layer window in logical desktop coordinates. Ordinary toplevels ignore it. |
| `ExactScreenPosition` | Keeps layer margins unclamped, including negative coordinates. The compositor can clip pixels outside the output. |
| `app.MovePopupWindow` | Queues movement of a layer popup, including between outputs. Safe from any goroutine. |
| `app.SetupBackground` | Closing the main window hides it and keeps its UI and auxiliary windows running. `window.Activate()` restores it. |
| `app.SetupOverlay` | Uses a transparent borderless top-layer surface anchored at logical desktop origin `(0, 0)`, including reserved desktop areas. |

`Floating: false` does not turn an undecorated layer surface into an ordinary
window. Use `TopLevel: true` for normal window stacking. The compositor can add
chrome to an ordinary toplevel. Layer surfaces avoid normal window decorations.
Ordering among surfaces in the same layer is compositor-controlled.

## Coordinates and sizing

With `xdg-output`, screen coordinates use the compositor's logical desktop
layout, including negative monitor origins, rotation, and fractional desktop
scaling. Wayland has no portable primary-monitor designation. A layer popup
selects the output containing its screen position, or the nearest output
when outside all outputs. Equal-distance ties prefer the current output, then
the lower registry identifier. Shared edges belong to the output whose bounds
start at that edge. Without logical output geometry, coordinates are local
to the compositor-selected output.

Movement across outputs recreates the native layer surface while retaining the
UI, logical owner, popup identifier and close callback. Non-exact placement clamps to the selected output's
full bounds, including reserved desktop areas. Exact placement uses unclamped
margins. Positioned layer windows do not reserve desktop space. A window cannot
span outputs through one layer surface.

During a primary-button drag, cross-output surface replacement waits for release
so the native pointer grab continues delivering motion and the release event.
The window remains on its current output during that drag, with the usual exact
or clamped placement. Output removal cannot preserve the removed surface's grab.

`Position` is owner-relative only when both windows use layer-shell and the owner
has an explicit position. Other owner-relative hints use compositor placement.
`window.Position` and `window.Center` operate on layer windows.
`window.SetSize` also resizes ordinary floating toplevels. Requests are discarded
when the compositor reports tiling, maximization, fullscreen, or interactive
resizing; restoring a window does not replay them. Startup requests wait for the
initial configure so Shirei can determine whether client sizing is available.
Compositor placement rules can override client sizes. Minimum-size hints remain
window properties and include client-drawn chrome when present.
Centering uses the current output's usable area. Sizes are logical content points,
including for floating windows with client-drawn title bars.

Rendering scale belongs to each native surface. With fractional-scale v1 and
viewporter, the backend renders at the compositor's preferred fractional scale,
rounds buffer dimensions to device pixels, and sets the viewport destination to
the logical surface size. Otherwise it uses the preferred integer surface scale
or the highest integer scale of outputs that surface enters. Outputs containing
other windows do not raise its rendering scale. Pointer and IME coordinates stay
in logical surface units. `SHIREI_FRACTIONAL_SCALE=0` exercises the integer fallback.

Removing a layer window's output recreates its surface on the containing or
nearest remaining output. Non-exact placement clamps to that output; exact
placement retains the desktop position and may put the surface off-screen.
An output-close notification waits for ordered output-removal events before
the backend treats it as an application window close.

## Activation and lifetime

`window.Activate()` maps a hidden main window and requests activation through
`xdg-activation` when available. `Focus` requests popup activation after its first
presentation. Requests use available input/focus serials. Launch activation
consumes and unsets `XDG_ACTIVATION_TOKEN`; `SetupQuiet` suppresses launch-token
activation. Quiet mapping itself remains subject to compositor focus policy.

The compositor can deny activation, particularly without recent user input.
Layer surfaces accept keyboard focus on demand, but activation requests for
layers are also compositor-dependent. Background helpers cannot guarantee focus
merely by issuing a request from a timer or global-shortcut callback.

Background mode retains the main UI while its native surface is hidden. Restoring
can recreate native objects; retain `waylandbackend.Context`, rather than caching
its native handles across hide/restore or output migration. A hidden UI processes requested frames
without presenting buffers. Without background mode, closing the main window
closes descendants and exits. Explicit application quit exits in either mode.

Held keys repeat with the compositor's delay and rate, subject to the keymap's
per-key repeat setting. Repeats stop on release, focus loss, and hiding, and do not
inject editing text during IME composition.

## Checking behavior

```sh
go -C shirei/behavior_test run ./wayland-window-sizing --close
go -C shirei/behavior_test run ./wayland-window-options --close
go -C shirei/behavior_test run ./wayland-window-options --close --normal-main
go -C shirei/behavior_test run ./wayland-window-options --close --overlay
SHIREI_GPU=0 go -C shirei/behavior_test run ./wayland-window-options --close
go -C shirei/behavior_test run ./wayland-window-options --manual
```

The options harness requires layer-shell v4. The general popup harness also
supports the ordinary-toplevel fallback:

```sh
SHIREI_LAYER_SHELL=0 go -C shirei/behavior_test run ./popup-windows --close
```

The sizing harness exercises startup/runtime content sizes, minimum sizes and
maximize/fullscreen transitions on ordinary floating windows. On Hyprland with
Lua dispatch support, it also floats only its own windows, inspects compositor
dimensions, and checks tiling. Other compositors report these extra inspections
unavailable. Startup dimensions are reported separately on Hyprland because
opening rules can replace the application's initial size. Hyprland 0.56.2 reports
maximized/tiled protocol states even for floating windows. When the initial
protocol state is constrained, the harness reports floating-size transitions as
skipped and checks that resize requests preserve the compositor-selected sizes.
On an unconstrained compositor, it checks that requests issued during maximize,
fullscreen or tiling do not reappear on restore, and that fresh floating requests
still work.

The options harness asserts native roles, content sizes, continued UI frames,
and close callbacks. On Hyprland it also checks mapped client/layer surfaces and
final coordinates. `--overlay --migrate` requires two outputs and checks movement
in both directions, exact overlay placement and clamped popup placement. Other
compositors report mapping/placement inspection unavailable. Pointer dragging and
focus policy still require interactive acceptance.

Protocol references: [layer-shell](https://raw.githubusercontent.com/swaywm/wlr-protocols/master/unstable/wlr-layer-shell-unstable-v1.xml),
[xdg-activation](https://raw.githubusercontent.com/wayland-mirror/wayland-protocols/main/staging/xdg-activation/xdg-activation-v1.xml),
[xdg-output](https://raw.githubusercontent.com/wayland-mirror/wayland-protocols/main/unstable/xdg-output/xdg-output-unstable-v1.xml),
[fractional-scale](https://raw.githubusercontent.com/wayland-mirror/wayland-protocols/main/staging/fractional-scale/fractional-scale-v1.xml),
and [viewporter](https://raw.githubusercontent.com/wayland-mirror/wayland-protocols/main/stable/viewporter/viewporter.xml).
