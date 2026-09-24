# Accessibility for custom widgets

This tutorial adds screen-reader support to a custom on/off control. It assumes
the containers and input helpers in the [custom widgets tutorial](custom-widgets-tutorial.md).
The [accessibility reference](accessibility.md) describes platform support and
native verification tools.

A screen reader needs a control's meaning and an action interface: its spoken
label, role, current state, and the operations it supports. Keyboard handling
alone does not supply that information. Shirei's desktop backends translate
these records and actions into the platform accessibility APIs.

## Start with the widget's existing support

Stock buttons, checkboxes, switches, and sliders supply their roles, state, and
actions. Their `Styled` variants retain that support when you customize paint.
Visible button and checkbox text supplies a spoken label. Add an explicit label
for icon-only buttons and controls whose purpose appears outside the widget:

```go
NextAccessLabel("Refresh")
Button(SymRefresh, "")

NextAccessLabel("Volume")
Slider(&volume, SliderAttrs{Min: 0, Max: 100, Step: 10})
```

For a control with your own container and paint, the shared interaction helpers
handle accessibility actions, while your widget supplies the semantic record.
Both parts belong to the same interactive container.

## Build a custom control

The following complete application draws a toggle as a labelled tile. Its
meaning is a checkbox, regardless of its visual shape. Pointer, keyboard, and
screen-reader activation all use `ProcessToggleEvents` and update the same
boolean.

Save this as `main.go` in a Shirei application module and run `go run .`.

```go
package main

import (
    "go.hasen.dev/shirei/app"

    . "go.hasen.dev/shirei"
    . "go.hasen.dev/shirei/widgets"
)

var sound, locked bool

func main() {
    app.SetupWindow("Accessible custom control", 540, 300)
    app.Run(RootView)
}

func RootView() {
    ModAttrs(UseSurface(SurfaceCanvas), Pad(24), Gap(16))
    Label("Sound preferences", FontSize(22))
    CheckBox(&locked, "Disable the sound control")

    NextAccessName("sound")
    SoundTile("sound", &sound, "Enable sound", locked)
}

func SoundTile(key any, on *bool, label string, disabled bool) {
    ContainerWithKey(key, Attrs(Row, CrossMid, Pad(16), Gap(12),
        Corners(8), BorderWidth(2)), func() {
        st := ProcessToggleEvents(on, disabled)

        NextAccessRole("checkbox")
        NextAccessLabel(label)
        NextAccessChecked(*on)
        NextAccessDisabled(disabled)
        AssignAccess()

        style := CurrentColorScheme.Buttons.Default
        if *on {
            style = CurrentColorScheme.Buttons.Primary
        }
        paint := style.Normal
        switch {
        case st.Disabled:
            paint = style.Disabled
        case st.Active:
            paint = style.Pressed
        case st.Hovered:
            paint = style.Hovered
        }
        ModAttrs(BackgroundVec(paint.Background),
            BorderColorVec(paint.Border),
            AmendTextStyle(TextColorVec(paint.Text)))
        if st.FocusVisible && !disabled {
            ModAttrs(BorderColorVec(CurrentColorScheme.FocusRing))
        }

        state := "Off"
        if *on {
            state = "On"
        }
        Label(state, FontWeight(WeightBold))
        Label(label)
    })
}
```

The order inside `SoundTile` matters:

1. **Process input.** `ProcessToggleEvents` handles pointer and keyboard input,
   accessibility press/focus requests, and disabled behavior. It changes `*on`
   on activation and makes enabled controls keyboard-focusable.
2. **Describe the resulting state.** The role is `checkbox`, its label is
   “Enable sound,” and `Checked` reflects the updated boolean. Disabled metadata
   matches the flag passed to the interaction helper.
3. **Assign it to this container.** `AssignAccess()` consumes the pending
   `NextAccess*` fields. Call it before building children that may consume their
   own metadata. `Container` and `ContainerWithKey` do not consume these fields
   themselves.
4. **Paint the same state.** The visible On/Off text, colors, and focus border
   describe the state the screen reader receives.

The explicit label keeps the spoken name stable as the visible On/Off text
changes. `NextAccessChecked` supplies the changing state separately. For a
button with a single visible label, Shirei can derive the name from its child
text when the assigned record has no explicit label. Icon glyphs are decorative.

`NextAccessName("sound")` is an optional query name for automation; it is not
the spoken label or the control's identity. `ContainerWithKey` supplies identity
within its parent. Use stable item keys for controls in lists that can reorder,
so native accessibility objects continue to refer to the same item.

`NextAccessDescription` can add helpful context. Set metadata on every build,
including false state values, so the completed frame describes the current
control. Shirei derives bounds, focus, and hierarchy from its container tree.

## Reuse actions, or implement them explicitly

`ProcessButtonEvents` supports press and focus. `ProcessToggleEvents` adds the
boolean change to that path. A custom button can return `st.Clicked` to its
caller and assign role `button` using the same pattern as the tile.

`ProcessSlider` supports focus, increment, decrement, and numeric set-value.
After processing its input, a custom slider supplies role `slider`, a spoken
label, disabled state, `NextAccessRange(value, min, max, step)`, and a display
value through `NextAccessValue`, then calls `AssignAccess`. See the
[stock slider](../widgets/slider.go) for the interaction and metadata sequence.

If your control owns its input handling, use `ProcessAccessAction` inside its
interactive container, before reading focus:

```go
action, requested := ProcessAccessAction(AccessPress|AccessFocus, disabled)
pressedByScreenReader := requested && action.Kind == AccessPress
```

Feed `pressedByScreenReader` into the same activation path as pointer and
keyboard input. `ProcessAccessAction` advertises the supplied actions and
consumes matching requests once across frame settling passes. It handles focus
requests through Shirei's normal focus/reveal path; your code applies other
returned actions to application state. Your interaction code also owns keyboard
navigation, pointer behavior, and disabled handling for those input sources.

Use one action handler per interactive container. Calling `ProcessAccessAction`
again alongside `ProcessButtonEvents` or `ProcessSlider` can replace the helper's
advertised actions. The helpers already call it for you. Supplying a role or
`Focusable` alone does not implement activation.

For numeric controls, advertise only operations you implement, and constrain
requested values to the control's valid range and step. Use the standard role
that matches the control's behavior. Arbitrary application-specific role names
do not create new screen-reader interaction patterns.

## Check the result

Run the application and verify these behaviors with your platform's screen
reader:

1. Navigate to **Enable sound**. Confirm a meaningful label, checkbox role, and
   checked/unchecked state; exact wording depends on the screen reader.
2. Activate it through the screen reader. The tile changes once, and reading
   the control again reports the new state.
3. Use Tab and Space to reach and toggle it with the keyboard. Confirm the
   visible focus indicator and state agree with what is spoken.
4. Enable **Disable the sound control**. Confirm the control exposes its
   disabled state and cannot toggle through the screen reader or pointer.
   Keyboard navigation skips it.

For automated checks, inspect the `sound` node in the completed frame's access
tree: role, label, `Checked`, `Disabled`, bounds, and supported actions. Send
accessibility actions using that node's stable `ID` and verify the resulting
state. Include repeated frame builds and disabled requests to catch duplicate
activation or stale metadata. A query name makes the control easy to find with
`QueryContainer` or the [drive tools](drive-tutorial.md).

Tree and action checks verify Shirei's semantic path. Native accessibility
clients and a screen reader also exercise the platform bridge and spoken
experience. The [accessibility reference](accessibility.md#scope-and-testing)
describes the native clients and manual form checks.

## Platform scope

Basic controls have native bridges on macOS, Linux Wayland, and 64-bit Windows.
Native Windows screen-reader acceptance is pending. Consult the
[platform support details](accessibility.md#scope-and-testing) when validating
your application.

Text-field metadata does not supply native accessible text editing, caret or
selection queries. Those interfaces, advanced list/table navigation, and X11
accessibility require additional framework support. Custom metadata alone does
not fill these gaps.
