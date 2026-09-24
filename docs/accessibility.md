# Accessibility

Shirei's macOS, Linux Wayland and 64-bit Windows backends expose a native
accessibility tree for static text and basic controls. macOS uses Cocoa
accessibility, Wayland uses AT-SPI over the accessibility D-Bus, and Windows
uses UI Automation (UIA) and Microsoft Active Accessibility (MSAA). Screen readers use the same widget state and interaction helpers as
keyboard and pointer input. Both GPU and software rendering support the bridges.

For a complete custom control with labels, state, and screen-reader actions,
see the [custom widget accessibility tutorial](accessibility-tutorial.md).

## Labels

Visible text supplies the spoken label for buttons, checkboxes and other
text-labelled controls. Standalone `Label`/`Text` content appears as static
text. Icon glyphs are decorative; icon-only controls need an explicit label.
Sliders and switches also need labels when their meaning is not inside the
control:

```go
NextAccessLabel("Refresh")
Button(SymRefresh, "")

NextAccessLabel("Volume")
Slider(&volume, SliderAttrs{Min: 0, Max: 100, Step: 10})
```

`NextAccessName` supplies a drive-query identifier. It is independent of the
spoken label. `NextAccessLabel` overrides the visible label for the next
`AssignAccess`; keep spoken and visible wording consistent when possible.
`NextAccessDescription` supplies additional help.

Custom controls call `AssignAccess` on their interactive container and use
`NextAccessRole` and the other `NextAccess*` setters to describe it. The stock
`ProcessButtonEvents` and `ProcessSlider` helpers handle accessibility actions.
Custom interaction helpers use `ProcessAccessAction` to advertise and consume
supported requests. Unknown role strings appear as groups on macOS and panels
on AT-SPI.

## Snapshot and input contract

`FrameOutputData.Access` is the source-ordered snapshot from the final frame
pass. IDs follow container identity; use keyed containers for controls that
must retain identity across insertion, removal or reordering. `ParentID`
identifies the nearest semantic ancestor. `Bounds` is the full rectangle in
window points, while `Rect` is clipped. `KeyboardFocused` identifies the exact
keyboard target; `Focused` also includes its ancestors for drive queries.

`AccessChanged` is independent of paint changes. A backend retaining a snapshot
copies its slice before the next `RunFrameFn`. Native objects stay stable while
their IDs remain present. Their getters read the backend snapshot; actions
queue one `FrameInputData.AccessAction` per frame. The interaction helpers
reject disabled and stale targets and consume requests once across settling
passes.

`PasswordInput` marks its value protected. Raw values are absent from the
access snapshot and drive output. `NextAccessHidden(true)` excludes a subtree
from the platform tree while retaining its drive-query records. Active focus
traps exclude background nodes from platform accessibility.

## Scope and testing

The macOS bridge supports press, focus, slider increment/decrement and numeric
set-value. Text-input role/value metadata is available, but accessible text
selection, text-range queries and full VoiceOver editing are not implemented.
The Wayland bridge supports press, focus within the active window, numeric
set-value and increment/decrement actions. Text fields expose names and roles;
AT-SPI Text/EditableText interfaces are not implemented. Menus/dialogs,
announcements and virtual-list/table navigation need further screen-reader
validation. X11 does not publish an OS accessibility tree.
The Windows bridge supports names, roles, hierarchy, screen bounds, focus,
Invoke, Toggle and RangeValue on amd64 and arm64. Text editing/selection, radio
selection patterns and table navigation are not implemented. Native Windows
screen-reader acceptance is pending; Wine compatibility checks are supplementary.

Run the native form checks or open the same form for manual VoiceOver use:

```sh
go run ./behavior_test/accessibility-form --close
go run ./behavior_test/accessibility-form --manual
```

On macOS, the automated form exercises native accessibility selectors,
state updates, stable objects, stale actions, window movement and hit testing.
Other platforms exercise the portable action channel. For a manual check,
turn on VoiceOver, navigate the named controls, activate Save and Enable sound,
and adjust Volume. Confirm the spoken state and visible result agree.

### Linux Wayland

AT-SPI support uses standard desktop services and has no distribution-specific
code. The session needs a D-Bus session bus and the AT-SPI accessibility bus and
registry, commonly provided by `at-spi2-core`. Orca and a working speech backend
provide spoken output. Package installation and session startup depend on the
distribution and desktop. No GTK dependency is added to Shirei applications.

The bridge discovers `org.a11y.Bus` on the session bus, registers the application
on its accessibility bus, and retries when services are unavailable or restart.
It also accepts `AT_SPI_BUS_ADDRESS`. Missing accessibility services do not
prevent opening or interacting with the application.

Window-relative and parent-relative component geometry is available. Absolute
screen positions are unavailable in the Wayland backend: screen extents return
the AT-SPI unknown-position sentinel, and screen-coordinate hit tests return
an error. Screen-reader mouse review and desktop-wide navigation also depend
on the compositor; ordinary control semantics and actions travel over D-Bus.

For an external protocol check, build the form and run the Python client in
the same Linux Wayland session:

```sh
go build -o /tmp/shirei-accessibility-form ./behavior_test/accessibility-form
python3 behavior_test/accessibility-form/check_atspi.py /tmp/shirei-accessibility-form
```

The client requires Python GObject bindings and AT-SPI introspection data. It
launches the form, discovers it through the desktop registry using its process
ID, verifies labels/roles/bounds, invokes controls, and checks events and visible
results through libatspi. Focus the form when prompted. The client closes its
form process on completion. It does not enable Orca or change system settings.
This checks the real platform connection independently of Shirei's drive API;
spoken output remains a separate Orca smoke test.

### Windows

The Win32 backend publishes UIA and MSAA providers through `WM_GETOBJECT`, independently
of GPU or software rendering. Providers use physical screen coordinates,
including the window's position and DPI scale. Disabled controls reject actions;
retained removed objects return `UIA_E_ELEMENTNOTAVAILABLE`. Actions enter the
normal widget input path on the window thread. Focus, structure and property
notifications describe completed accessibility snapshots.
MSAA exposes the same names, roles, state, hierarchy and actions through
`IAccessible`, with WinEvents for focus and changes. It operates independently
of UIA client support. MSAA numeric values support reading and setting; full
text editing, caret/selection queries and IAccessible2 are not implemented.

Cross-build a form and an independent Windows UIA client from the repository root:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o accessibility-form.exe ./behavior_test/accessibility-form
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o check-uia.exe ./behavior_test/accessibility-form/check-uia
```

On Windows, launch a fresh `accessibility-form.exe --manual`, then run
`check-uia.exe` separately. The client checks OS discovery, names, roles,
bounds, hit testing, focus, Invoke/Toggle/RangeValue actions and native events.
Keep the form unobscured for hit testing and focus it when prompted. It needs
no Python installation and does not enable a screen reader. Its actions save
once, toggle sound and change the form's volume. Restart the form before
repeating the check.

For a speech check, use NVDA or Narrator and Tab through Save, Enable sound
and Volume. Verify names, checkbox state and slider value against the form.
Compatibility runtimes can expose only parts of UIA: a missing client method
or an NVDA UIA initialization error is a runtime limitation, not a passing
bridge result. `SHIREI_ACCESS_DEBUG=1` logs native provider discovery requests.

The Win32 package's `TestAccessibilityProvider` exercises native COM callback
calling conventions, object ownership and queued actions. The form's `--close`
mode exercises the portable widget/action path; neither replaces the independent
client or the spoken-output check.

For clients using MSAA, build the independent MSAA checker:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o check-msaa.exe ./behavior_test/accessibility-form/check-msaa
```

With a fresh `accessibility-form.exe --manual` open, run `check-msaa.exe`. It
uses `AccessibleObjectFromWindow`, `AccessibleChildren`, native hit testing,
focus/default/value actions and a WinEvent hook. It verifies that event targets
resolve back to the expected controls through `AccessibleObjectFromEvent`.
The checker is DPI-aware, like the form, and needs the form unobscured for its
screen-point checks. It requires neither a screen reader nor working UIA client
APIs. Passing this check does not replace a separate NVDA listening check.
