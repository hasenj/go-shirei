# Migrating from v0.7.0 to v0.8.0

This guide covers code changes that may be needed when upgrading an application
from v0.7.0. In v0.8.0, stock controls take their colors from a shared scheme,
and keyboard focus has a separate visible indicator. The built-in light scheme
is active by default, so apps that use stock controls without customizing their
colors need no appearance changes. Update module versions, then follow only the
sections that apply to your code.

## Module versions

Update `go.hasen.dev/shirei` to v0.8.0. If your application imports a Shirei
extension (`ext/darkmode`, `ext/window`, or `ext/camera`), update it to v0.8.0
as well. Update separately versioned Shirei examples or demos only if your
project depends on them.

## Removed button and focus APIs

If your code refers to the removed `widgets.ButtonAccent` or
`widgets.FocusRing` globals, use the corresponding fields of the active
scheme. For example, a global accent and focus-color customization can use:

```go
widgets.CurrentColorScheme.Buttons.Default = widgets.ButtonStyleWithAccent(
    widgets.CurrentColorScheme.Buttons.Default, shirei.Vec4{204, 70, 48, 1},
)
widgets.CurrentColorScheme.FocusRing = shirei.Vec4{210, 85, 52, 1}
```

Make changes before building the UI. `ButtonStyleWithAccent` changes enabled
button paints and preserves disabled paint. If the app calls `SetDarkMode`,
customize its preferred light and dark schemes instead; each mode selection
applies the stored scheme. The [appearance tutorial](appearance-tutorial.md)
shows that setup. `ButtonWithAccent`, `CtrlButtonWithAccent`, and
`ButtonAttrs.Accent` remain available for individual buttons.

If your code sets the removed `ButtonLook.TopBoost` or
`ButtonLook.ElevationDrop` fields, remove those assignments and use
`ButtonPaint.Gradient` and `ButtonPaint.Elevation` to control the face and lip
colors. There is no direct numeric conversion. `ButtonLook` still controls
geometry through `TextSize`, `PushDown`, and `PadScale`.

If your app assigns `DefaultAccent` or `DefaultBackground` to change stock
controls or surfaces, move that customization to the relevant
`CurrentColorScheme` fields. Those globals remain color presets for explicit
use, but assigning them does not change the stock appearance.

## Custom controls that paint focus

If a custom control uses `HasFocus()` to paint its focus outline, use
`HasVisibleFocus()` for that paint so pointer focus does not show the keyboard
focus indicator. Keep `HasFocus()` for keyboard handling and text-editor carets.
The stock interaction helpers expose visible focus through `FocusVisible`.

If custom keyboard navigation requests focus programmatically, show the focus
indicator after the focus call:

```go
FocusImmediateOn(nextControl)
ShowFocusIndicator()
```

The [focus section of the main tutorial](tutorial.md#focus-and-keyboard)
describes the behavior in detail.
