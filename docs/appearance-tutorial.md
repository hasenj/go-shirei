# Appearance: color schemes, dark mode, and widget styles

This tutorial covers application appearance: choosing light and dark palettes,
matching surfaces and text, and customizing stock widgets. It assumes the
containers, attributes, and widget calls in the [main tutorial](tutorial.md).

Shirei starts with the cool light preset, so appearance setup is optional.
When customizing shared colors, start with a complete preset and edit its
fields; a widget's `Styled` entry point supplies paint for one control. For
custom control geometry and interaction, continue with the
[custom widgets tutorial](custom-widgets-tutorial.md).

- [A complete application](#a-complete-application)
- [Color schemes and mode selection](#color-schemes)
- [Surfaces and inherited text](#container-surfaces-and-inherited-text)
- [Customizing a scheme](#customize-a-scheme)
- [Button properties](#button-properties)
- [Explicit widget styles](#explicit-widget-styles)
- [Checkboxes](#checkbox-color-schemes) and [other widgets](#other-widget-styles)
- [Focus appearance](#focus-appearance)

## A complete application

This application follows the OS appearance by default. Turn off **Follow system
appearance** to choose light or dark mode within the application. The preference
stays in application state, so OS changes do not override a manual choice.

```go
package main

import (
    "go.hasen.dev/shirei/app"
    "go.hasen.dev/shirei/ext/darkmode"

    . "go.hasen.dev/shirei"
    . "go.hasen.dev/shirei/widgets"
)

var (
    followSystem = true
    manualDark   bool
    name         = "World"
)

func main() {
    app.SetupWindow("Appearance preferences", 520, 360)
    app.Run(RootView)
}

func RootView() {
    dark := manualDark
    if followSystem {
        dark = darkmode.OSDarkMode()
    }
    SetDarkMode(dark)

    ModAttrs(UseSurface(SurfaceCanvas), Pad(24), Gap(16))
    Label("Appearance preferences", FontSize(22))
    CheckBox(&followSystem, "Follow system appearance")
    if !followSystem {
        CheckBox(&manualDark, "Dark mode")
    }

    Container(Attrs(UseSurface(SurfacePanel), Expand, Pad(16), Gap(8),
        BorderWidth(1), Corners(8)), func() {
        Label("Your name")
        TextInput(&name)
        Label("Hello, " + name)
        NextButtonType(ButtonPrimary)
        if Button(NoIcon, "Reset name") {
            name = "World"
            RequestNextFrame()
        }
    })
}
```

`SetDarkMode` runs before any surfaces or widgets resolve their colors.
`UseSurface` pairs each container's background with a suitable foreground;
labels inherit it, and stock controls use their own scheme fields.

The `ext/darkmode` module detects system appearance and requests a frame when
it changes. See its [README](../ext/darkmode/README.md) for installation and
platform details. An application with only manual light/dark selection can
pass its setting directly to `SetDarkMode` without importing that extension.

The runnable [dark-mode probe](../demos/darkmode-probe/main.go) adds a warm-palette
switch. From the Shirei directory, run:

```sh
go run ./demos/darkmode-probe
```

## Color schemes

`CurrentColorScheme` is a global `widgets.ColorScheme`. Assign it on the UI
thread before building the UI:

```go
CurrentColorScheme = WarmColorScheme()
```

The four presets return independent, complete values:

| Preset | Surfaces | Primary actions |
| --- | --- | --- |
| `LightColorScheme()` | Cool light | Blue |
| `WarmColorScheme()` | Cream | Green |
| `DarkColorScheme()` | Cool charcoal | Blue |
| `WarmDarkColorScheme()` | Warm charcoal | Green |

Choose a preset by assigning `CurrentColorScheme`; dark mode uses the same
scheme mechanism as any other palette. Regular buttons, compact buttons, and
menu triggers share the same semantic styles.

For light/dark switching, configure the preferred pair and select a mode:

```go
SetLightColorScheme(WarmColorScheme())
SetDarkColorScheme(WarmDarkColorScheme())
SetDarkMode(true) // false selects the preferred light scheme
```

The initial preferences are `LightColorScheme()` and `DarkColorScheme()`, with
light mode active. The setters copy complete values. Changing the active
preference applies it immediately; changing the inactive preference stores it
for the next switch. Only an effective color change requests a frame, so
`SetDarkMode` can run each frame without keeping an idle app rendering.

The [complete application](#a-complete-application) selects the mode from either
the OS preference or an application setting. Mode selection belongs to the
application.

All three setters run on the UI thread or under the frame lock. Direct writes
to `CurrentColorScheme` remain available; they do not update the preferred pair
or the selected mode. Calling `SetDarkMode` again applies the stored preference,
even when its boolean argument is unchanged. The OS probe demonstrates manual
overrides and following system appearance: `go run ./demos/darkmode-probe`.

## Container surfaces and inherited text

`UseSurface` applies a semantic surface role to an ordinary container:

```go
ModAttrs(UseSurface(SurfaceCanvas))

Container(Attrs(UseSurface(SurfaceToolbar), Row, Pad(12), Gap(8)), func() {
    Icon(SymGrid)
    Label("Documents")
    NextButtonType(ButtonPrimary)
    Button(NoIcon, "Save")
})

Container(Attrs(UseSurface(SurfacePanel), Pad(16), BorderWidth(1)), func() {
    Container(Attrs(Gap(8)), func() {
        Label("Details")
        Label("Ordinary text inherits the panel's foreground.")
    })
})
```

`CurrentColorScheme.Surfaces` holds `Canvas`, `Panel`, and `Toolbar` values.
Each `SurfaceColors` value pairs `Background`, `Text`, and `Border` colors.
This lets a dark toolbar use light text within an otherwise light application.
Edit these values directly to customize a preset.

The helper sets background and border colors and amends the inherited text
color. It leaves fonts, border width, spacing, and layout under caller control.
Plain nested containers inherit text without acquiring a background. Labels
and icons use that text style; buttons use their own semantic paint.

Setters compose left to right. Both font amendments below survive, while the
surface supplies the foreground:

```go
Attrs(
    AmendTextStyle(FontWeight(WeightBold)),
    UseSurface(SurfacePanel),
    AmendTextStyle(FontSize(16)),
)
```

A later `AmendTextStyle(TextColor(...))` overrides the surface foreground.
`SetTextStyle` replaces the entire text style. `ModAttrs(UseSurface(...))`
works at the beginning of an existing container's body, before adding children.

`UseSurface` reads the global scheme when the setter runs. Retained setters
therefore follow live changes; an already-built `AttrSet` holds resolved colors.
Apply the helper inside popup or modal bodies to theme those containers.
Painting the application canvas alone does not recolor controls or popup
surfaces that specify their own colors.

## Customize a scheme

Each role's `ButtonStyle` has `Normal`, `Hovered`, `Pressed`, and `Disabled`
paints. Each `ButtonPaint` specifies `Background`, `Gradient`, `Text`, `Border`,
and `Elevation`. `Gradient` is an HSLA delta from the top to the bottom of the
face; `Elevation` colors the lip beneath it. Focused enabled controls use the
scheme's shared `FocusRing` color. `ButtonLook` controls geometry: text size,
padding scale, and the press/elevation distance. The regular look uses a
0.5-point bottom lip with a matching press movement; the compact look has no
lip. Preset button faces use solid fills. Explicit styles can supply gradients.

Customize a preset as ordinary data:

```go
scheme := LightColorScheme()
scheme.Buttons.Primary = ButtonStyleWithAccent(
    scheme.Buttons.Primary, Vec4{275, 45, 42, 1},
)
scheme.Buttons.Primary.Disabled.Text = Vec4{0, 0, 45, 1}
scheme.FocusRing = Vec4{275, 60, 45, 1}
CurrentColorScheme = scheme
```

For an application that calls `SetDarkMode`, store the customized light scheme
with `SetLightColorScheme(scheme)` during setup. Customize a dark preset and
store it with `SetDarkColorScheme` as well. Mode selection then uses those
preferred values.

`ButtonStyleWithAccent` sets the normal face to the supplied accent, preserves
other enabled states' lightness offsets, tints elevation, and derives text
colors. It retains the scheme's gradients, borders, and disabled paint.
`NextButtonAccent` uses the same operation for one button. For exact colors,
assign the state paints directly. All colors use HSLA `Vec4`; zero colors are
valid transparent values, not inheritance markers.

Widgets resolve paint on each build, so an open popup also follows the active
scheme. Assignments preserve focus, press handling, and widget identity. Call
`RequestNextFrame()` after a direct assignment in a handler or outside the
frame callback. The scheme setters request that frame themselves.

The scheme supplies application surfaces, all stock widget paint, and the
common focus color. Its fields are complete values: changing `Surfaces.Panel`
does not recompute fields such as `TextInput` or `Table`. Start with a preset
and edit the widget styles you want to customize. `DefaultAccent` and
`DefaultBackground` are explicit color presets; they do not control widget
defaults. The theme demo has a live scheme switch:

```sh
go run ./demos/theme
go run ./demos/theme --dark
go run ./demos/theme --dark --warm
```

The demo’s independent **Warm colors** and **Dark mode** switches select all
four presets.

## Button properties

Use `NextButton` setters for optional properties while keeping the ordinary
button call:

```go
NextButtonType(ButtonPrimary)
NextButtonDisabled(!canSave)
if Button(SymPass, "Save") {
    save()
}
Button(NoIcon, "Cancel")
```

The next button consumes all pending properties and resets them to defaults.
Cancel therefore uses the ordinary style and is enabled. Repeated setters use
the last value, including `NextButtonDisabled(false)` and
`NextButtonType(ButtonDefault)`. Put setters inside the same conditional as the
button they configure. Unused non-default properties produce a diagnostic and
reset at the end of the frame pass.

`ButtonPrimary` and `ButtonDestructive` select styles from
`CurrentColorScheme.Buttons`; ordinary buttons use its `Default` style.
`NextButtonAccent` tints the enabled states of the selected style.
`NextButtonTextSize` and `NextButtonTextStyle` customize typography;
`NextButtonAttrs` replaces the entire pending `ButtonAttrs` value.

`CtrlButton`, `MenuButton`, and `CtrlMenuButton` consume the same properties.
Compact geometry and semantic role are independent. A disabled menu trigger
closes its popup. `CtrlButton`'s explicit `enabled=false` always disables it;
pass true when using only `NextButtonDisabled` to control its state.

The icon argument supplies content. `ButtonWithAccent` and
`CtrlButtonWithAccent` give their explicit accent argument precedence.
`ButtonExt` and `MenuButtonExt` take a complete explicit configuration: they
clear pending properties and use their supplied struct without merging.
`MenuItem` and custom controls using `ProcessButtonEvents` do not consume
`NextButton` properties.

## Explicit widget styles

The naming convention for theme-aware widgets has three layers:

| Layer | Configuration | Color source |
| --- | --- | --- |
| `Widget` | Essential arguments plus supported `Next...` properties | Active scheme |
| `WidgetExt` | Explicit attributes | Active scheme with supported overrides |
| `WidgetStyled` | Explicit attributes, geometry, paint, and focus color | Supplied colors |

Every stock widget with its own paint has a `Styled` entry point. Related
controls share style data: checkboxes, radios, switches, and segmented controls
use `SelectionStyle`; text areas, password fields, and compact inputs use
`TextInputStyled` with the appropriate `TextInputAttrs`. Busy indicators and
plain labels inherit text color and accept `TextStyleFn` overrides.

```go
paint := ButtonPaint{
    Background: Vec4{280, 35, 90, 1},
    Gradient:   Vec4{0, 0, -6, 0},
    Text:       Vec4{280, 40, 20, 1},
    Border:     Vec4{280, 30, 50, 1},
    Elevation:  Vec4{280, 30, 60, 1},
}
style := ButtonStyle{
    Normal: paint, Hovered: paint, Pressed: paint, Disabled: paint,
}
style.Hovered.Background = Vec4{280, 40, 95, 1}
style.Pressed.Background = Vec4{280, 35, 82, 1}
style.Disabled.Text = Vec4{280, 10, 50, 1}

if ButtonStyled("Preview", ButtonAttrs{Icon: SymSearch},
    DefaultButtonLook(), style, Vec4{280, 70, 40, 1}) {
    preview()
}
```

`ButtonStyled` uses the same interaction, accessibility, and geometry as the
themed buttons. Pass `DefaultCtrlButtonLook()` for compact geometry. `ButtonStyle`
is the same data type stored in a scheme, so callers can also copy a preset's
style and edit it. The focus color is a separate explicit argument.

`Styled` calls replace and clear pending `NextButton` properties. `ButtonAttrs.Type`
and `Accent` are theme-resolution hints and have no effect at this layer; all
other button attributes apply. Every supplied color is literal, including
transparent zero values. The renderer does not read or modify the active scheme.

`MenuButtonStyled(label, attrs, look, style, focusColor, body)` styles the trigger.
Popup surfaces and the widgets in `body` have their own styling. For entirely
custom button geometry, use `ProcessButtonEvents` inside your own container.

## Checkbox color schemes

`CheckBox` and `CheckBoxExt` resolve `CurrentColorScheme.CheckBox` on each build.
Its `SelectionStyle` holds `Unselected` and `Selected` states; each has `Normal`,
`Hovered`, and `Pressed` paint. Active press paint takes precedence over hover.
Each `SelectionPaint` supplies `Background`, `Gradient` (an HSLA delta), `Border`,
and `Indicator` (the checkmark). Focus uses the scheme's `FocusRing` color.

```go
scheme := WarmColorScheme()
scheme.CheckBox.Selected.Normal.Indicator = Vec4{45, 70, 95, 1}
CurrentColorScheme = scheme

CheckBox(&showHidden, "Show hidden files")
CheckBoxExt(&pinSelection, "Pin selection", CheckBoxAttrs{
    Size: 20,
    Accent: AccentMeadow,
})
```

`CheckBoxAttrs.Accent` uses `SelectionStyleWithAccent`: it tints selected fills
and all borders, preserves the selected states' lightness offsets, and derives
contrasting checkmark colors. Unselected fills and all gradients retain their
scheme values. A zero accent uses the scheme directly.

For complete explicit paint, use the Styled layer:

```go
style := LightColorScheme().CheckBox
style.Selected.Normal = SelectionPaint{
    Background: Vec4{280, 40, 35, 1},
    Border:     Vec4{280, 40, 20, 1},
    Indicator:  Vec4{280, 20, 95, 1},
}
CheckBoxStyled(&customOption, "Custom option", CheckBoxAttrs{Size: 20},
    style, Vec4{280, 70, 50, 1})
```

`CheckBoxStyled` uses the supplied state paint and focus color without reading
the scheme. All colors are literal, including transparent zeros; `Accent` has
no effect at this layer. Labels in every checkbox entry point inherit the
surrounding text color, including light text on a dark toolbar. To override a
label color, amend the surrounding container's text style.

## Other widget styles

| Scheme field | Widgets | Explicit renderer |
| --- | --- | --- |
| `Radio` | Radio options | `OptionButtonStyled` |
| `Switch` | Toggle switches | `ToggleSwitchStyled` |
| `Segmented` | Segmented controls and their cells | `SegmentedControlStyled` |
| `Slider` | Slider track and handle | `SliderStyled` |
| `Progress` | Progress bars | `ProgressBarStyled` |
| `TextInput` | Text fields, text areas, password fields | `TextInputStyled` |
| `Menu` | Menu rows, separators, popup surfaces | `MenuItemStyled`, `MenuSeparatorStyled`, `PopupPanelStyled` |
| `ScrollBar` | Scrollbars and virtual lists | `ScrollBarStyled`, `VirtualListViewStyled`, `LargeTextStyled` |
| `Table` | Table body, headers, sorting, scrollbar | `TableStyled` |
| `Log` | Log selection, copy control, scrollbar | `LogViewStyled` |
| `Toast` | Notification cards | `ToastStyled` |
| `ImageWipe` | Image comparison chrome and tags | `ImageWipeStyled` |
| `List` | File and path pickers | Composite renderers below |
| `Overlay` | FPS and profiler panels | `FPSCounterStyled`, `ProfileButtonStyled` |

Each renderer uses the same interaction machinery as its themed counterpart.
`Styled` colors are literal; color overrides in an attrs argument do not
replace the supplied style. Labels that inherit their parent's foreground
still do so. Table cell builders and other caller-provided content own their
paint.

Radios, switches, and segmented controls use the same selected/unselected and
normal/hovered/pressed structure as checkboxes. A switch's `Indicator` and
`IndicatorGradient` paint its knob; a segmented cell's `Indicator` paints its
label. Each family has an independent scheme field. The segmented frame uses
`Unselected.Normal.Background` and `Unselected.Normal.Border`; the selected
inset face uses the selected paint's background and border. Keyboard focus
uses a thin translucent edge without changing the cell's size.

`SliderStyle.Track` colors the filled portion up to the handle, and `Remainder`
colors the rest. `Handle`, `HandleGradient`, and `HandleBorder` describe the
thumb. All colors in `SliderStyled` are literal, including transparent zeros.
Keyboard focus outlines the thumb without changing the track geometry.

Try the four primary controls together with `go run ./demos/controls`.
Its light/dark and warm switches apply the preferred color schemes live.
Default segmented controls match the height of default buttons. The selected
cell uses `SelectionPaint.Shadow` for subtle depth; its dimensions scale with
comfort settings. A zero shadow disables it, and `Border` can supply an explicit
selected-cell border.

`TextInputStyle` covers the field background, idle and hovered borders, text,
placeholder, caret, selection, and inset. Focus uses `FocusRing`, or a nonzero
`TextInputAttrs.Accent` override. IME underlines use the caret color. The lower
`ProcessTextInput` / `DrawTextInputPlain` path accepts
`TextInputConfigWithStyle(cfg, style)`; `LiteralColors` allows transparent zero
colors. `ShapedTextLayoutStyled` supplies an explicit selection color for
custom text renderers.

```go
style := LightColorScheme().TextInput
style.Background = Vec4{270, 25, 18, 1}
style.Text = Vec4{270, 20, 95, 1}
style.Caret = style.Text
style.Selection = Vec4{280, 60, 55, .5}
TextInputStyled(&query, DefaultTextInputAttrs(), style, Vec4{280, 60, 65, 1})
```

File pickers combine lists, fields, buttons, scrollbars, and modal surfaces.
`DirectoryBrowseStyled`, `FileBrowserPanelStyled`, `FuzzyPathFinderStyled`, and
`FileSelectorStyled` take an explicit `ColorScheme` value for all their stock
children. `ProfileButtonStyled` does the same for its panel and button. They
do not install that value globally.

Ordinary scrolling widgets retain the app's `DefaultScrollBar` callback.
Their `Styled` variants use the supplied scrollbar paint; the callback is an
independent override for the themed layer.

`Toast` and `ToastExt` resolve paint when the card is drawn, so queued toasts
follow live scheme changes. `ToastStyled` stores a copy of its explicit style.

With `widgets` imported, `shirei.Modal` resolves its panel and scrim from the
active scheme through `shirei.DefaultModalStyle`. For an explicit surface,
call `ModalStyled(width, dismiss, ModalStyleForScheme(scheme), body)` or pass a
`shirei.ModalStyle` directly. The body owns its widget styling.

## Focus appearance

The active scheme's `FocusRing` supplies the shared focus color. Buttons,
checkboxes, sliders, and segmented cells use a two-logical-point edge at half
that color's opacity. The outline stays inside the face or its reserved focus
space, so it does not change layout. `Styled` controls accept an explicit
focus color.

Custom skins built with `ProcessButtonEvents`, `ProcessSegmentEvents`, or
`ProcessSlider` use the returned `FocusVisible` field for the ordinary focus
outline and `HasFocus` for keyboard behavior. Text editors use `HasFocus` for
their caret, selection, and editing appearance even after a mouse click. See
[focus and keyboard](tutorial.md#focus-and-keyboard) for requesting focus and
enabling its indicator during custom keyboard navigation.
