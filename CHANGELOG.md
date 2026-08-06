# Changelog

## v0.6.6 — 2026-08-06

**Release packaging for all supported platforms** via `shirei_bundle`, including
desktop **`Resources/`** bundling with `app.ResourcePath`, plus library polish
(IconGlyph, default clipping, text editing, fonts) and a web preview backend.

v0.6.0 could install iOS/Android apps in **dev mode** with `mobilerun`, but had
no release-bundle path. This release adds that path: signed IPAs, release APKs,
and desktop archives for macOS, Linux, and Windows.

Use **`@v0.6.6`** (or `@latest`). Do not use `v0.6.5` — see below.

### Release packaging

- **`shirei_bundle`:** GUI and CLI to produce release artifacts for a
  `package main` — signed **IPA**, release **APK** (`debuggable=false`, your
  keystore), macOS **.app / zip / pkg** (Developer ID notarize and/or App Store),
  Linux **tar.gz**, Windows **zip** (GUI subsystem, no console). Writes packages
  on disk; store upload and Android App Bundles (AAB) stay outside the tool.
  Docs: [`cmd/shirei_bundle/README.md`](cmd/shirei_bundle/README.md),
  [android](docs/android.md), [ios](docs/ios.md).
- **`Resources/`:** assets next to `package main` ship into the desktop package
  resource root; apps load them with `app.ResourcePath` / `app.ResourcesDir` in
  both `go run` and release builds. Docs: [resources](docs/resources.md).
- **`shirei_mobilerun`:** former top-level `mobilerun`, now under `cmd/` — still
  the fast **dev** install path (debug signing, simulator/device iteration).

### Upgrading

- **Tools path:** `go install go.hasen.dev/shirei/cmd/shirei_bundle@v0.6.6` and
  `…/cmd/shirei_mobilerun@v0.6.6` (also `shirei_tester`, `shirei_web`).
- **`IconGlyph`:** `Button` / `CtrlButton` / `MenuItem` take `IconGlyph` (font
  family + codepoint), not a bare `rune`, so custom icon fonts cannot silently
  rematch Microns/Typicons PUA runes. Use `NoIcon` for no icon; `Sym*` / `Typ*`
  keep working. Custom fonts: `UseFontBytes` then
  `IconGlyph{Font: "family", Rune: …}` (issue #11; see `demos/custom-icon-fonts`).
- **`ButtonExt`:** replaces `AccentButton` as the skinnable button entry
  (`Button` / `CtrlButton` remain thin wrappers).
- **Clip defaults on:** `Attrs()` sets `Clip = true`. Opt out with **`NoClip`**.
  Prefer `Popup` for overlays. Borders paint fully **inside** the container rect
  (stroke grows inward) so they stay visible under default clipping.

### Bug fixes

- **Menu:** filterable dropdowns no longer highlight the first item on open.
  Keyboard selection starts empty; **Down** selects the first row, **Up** from
  there clears selection. Enter still activates only a keyboard-selected row.
- **TextArea / text editing:** hard-break caret bounds, empty-line geometry,
  Ctrl+Home/End (where applicable); **`KeyInsert`** mapped on backends;
  Windows-style clipboard aliases (Shift+Insert paste, Ctrl+Insert copy,
  Shift+Delete cut) on non-mac platforms (issues #6, #9, #10).

### Fonts

- **Critical-path sync load + background system scan:** a small per-GOOS path
  list loads before first paint; remaining system fonts walk on a goroutine.
  Shape-cache keys include a font-lookup epoch so fallbacks refresh when the
  scan adds faces.

### Host / render

- **`Host.PixelOrder`:** soft-render writes a presentable channel layout
  (default BGRA; web and Android use RGBA). Image cache stores ordered bytes;
  `ToRGBA` inverts for snapshots.
- **`CenterWindow` / `PositionWindow`:** best-effort initial placement on
  macOS, Windows, and X11; no-op on Wayland and mobile (issue #4).
- **Win32:** skip render+present when content hash and client size match the
  previous frame.

### Widgets

- **`ImageWipe`:** wipe-to-compare two images; optional **`MaxSize`** (demos
  under `demos/image-diff-*`).
- **`EditorSetSelection`:** programmatic selection on text editors (PR #14).
- **`VirtualListAttrs.AvgSampleTop` / `AvgSampleBottom`:** how many head/tail
  rows feed the average-height total (default top 50). Docs:
  `docs/virtual-list.md` §5.

### Web (preview)

- **`jsbackend`:** browser/wasm host for landing-page demos.
- **Web audio** via AudioWorklet + SharedArrayBuffer ring.
- **`shirei_web`:** build static wasm pages; gallery mode.
- **`Host.PrimaryMod`:** Cmd on Mac browsers for shortcuts.

### Other tools

- **`shirei_tester`:** snapshot runner with ImageWipe compare (replaces
  snapreview).

### Dependencies

- **`go.hasen.dev/textsearch` v0.2.0** — package scanner used by `shirei_bundle`.
- **`golang.org/x/image` v0.43.0** (PR #13).

### Examples / site

- `git_history` and `dir_weight` polish; landing-site demo gallery redesign
  (not required for module consumers).

## v0.6.5 — 2026-08-06

**Do not use.** Tag was published without declaring `go.hasen.dev/textsearch` in
`go.mod`, so `go install` / `go run` of `cmd/shirei_bundle@v0.6.5` fails. Use
**v0.6.6** instead (same release content).

## v0.6.0 — 2026-07-22

* iOS/Android backend, with builtin tool `mobilerun` to install apps to iOS/Android in dev mode.
* Escape hatch provides platform context to allow platform extension development.
* Touch events available raw as well as synthesizing mouse/wheel events (with a flag set: `MouseFromTouch`).
* Size constraints cascade along the cross-axis: max width cascades on columns, max height on rows.
* Text styles are now container attributes and they cascade to direct children.
* Allow measuring a view function without actually rendering it.
* Restructure default widget input processing mechanism to allow reuse in userland custom widgets.
* Step-by-step tutorial for creating chat-app like layout witha custom "compose" field.
* New example programs: git_history and hacker-news-reader

Details below:

### Upgrading from 0.5.x

- **`DecorationFn` / `DecorationHeight` are gone.** Window chrome (Wayland
  client-side titlebar, mobile keyboard accessory bars) is owned by the
  backend. Apps that never set those hooks need no change.
- **TextInput fills leftover row space by default.** Compose bars of the form
  `[TextInput][Send]` no longer need a manual min-width. Pin a compact field
  with `TextInputExt` and `FixedWidth: true` (or an explicit `MinWidth` /
  `MaxWidth` as needed).
- **Cross-axis max size and text style cascade** from parent containers when
  unset on the child. Opt out of the size cascade with `UnsetMaxCross` in
  `Attrs(...)` or `ModAttrs` (same pattern as `YesAnimate`). Prefer
  `AmendTextStyle` / `SetTextStyle` for text.
- **VirtualList `ItemHeight` may be `nil`:** each row is measured with
  `Measure` (no height cache). Pass a cheap `ItemHeight` when you already
  know row heights.

### Host

- `Host.PreferredOrientation` (`OrientationAny` / `Portrait` / `Landscape`):
  sticky app → backend policy. Mobile backends lock the OS interface
  orientation so window size, safe area, and the soft keyboard follow it.
- `Host.HardwareKeyboard`: backend → app flag for a physical keyboard
  (not soft IME). May change at runtime when a keyboard is attached or
  detached; desktop defaults true.
- `Host.ComfortScale`: multiplies default control chrome for touch density
  (button/input text and padding, segmented height/min width, slider handle,
  checkbox / radio / toggle size). Design units are authored at scale `1`;
  widgets do `size * ComfortScale` (no zero-sentinel). Desktop and headless
  default to `1`; iOS and Android set `1.25` at `Run`. Apps may override.

### iOS backend

- New `iosbackend`: UIKit host with the core software renderer. Touch fills
  multi-contact `InputState.Touches` and, in parallel, synthesizes primary
  finger → mouse + scroll/fling so pointer-based UIs keep working.
- Soft keyboard via `UITextInput` (IME composition verified with Japanese),
  system clipboard, and a keyboard accessory bar (arrows / select all / copy /
  paste / done). Content area shrinks for the keyboard and orientation changes.
- Honors `Host.PreferredOrientation` via the root VC's
  `supportedInterfaceOrientations` (and iOS 16+ geometry update).
- Defers system edge gestures (`preferredScreenEdgesDeferringSystemGestures`
  → all edges) so short taps on top/bottom chrome reach the app as live
  contacts instead of a delayed begin+end on lift.
- `app.StartAudio` via AudioQueue.

### Android backend

- New `androidbackend`: NativeActivity (vendored `native_app_glue`) with the
  core software renderer drawing straight into the locked `ANativeWindow`
  buffer. Touch uses the same multi-contact table + primary-finger synthesizer
  as iOS.
- Soft keyboard with basic IME composition (composing text renders inline),
  backed by one small Java activity subclass compiled into the APK; system
  clipboard; keyboard accessory bar above the keyboard.
- Honors `Host.PreferredOrientation` via `Activity.setRequestedOrientation`.
- `app.StartAudio` backed by AAudio (loaded at runtime; devices below the
  supported API level run silent with an error instead of failing to load).
- `WindowSize` follows the window content rect: status/nav bars and the soft
  keyboard shrink the app area instead of being drawn under.
- Networking works out of the box: generated manifests declare `INTERNET`.
- Docs: [Running on Android](docs/android.md), [Running on iOS](docs/ios.md).

### Mobile runner (`mobilerun`)

- New **`mobilerun`**: GUI and CLI to build a `package main` and launch it on
  **iOS or Android** from one tool — platform picker, package scan, per-app
  id / name / icon (defaults to `<package>/icon.png`), global app id prefix.
- iOS: Simulator or USB device, team picker, embedded `ios-run.sh` + UIKit
  host — no monorepo layout required.
- Android: NDK cross-compile, aapt2 / zipalign / apksigner, adb install and
  launch, launcher icons via aapt2 mipmap, `--screencap` / `-logcat`. No
  Gradle, no Android Studio. Hosts: **macOS, Linux, and Windows**.
- **Dev-mode only** for this release: debug signing, debuggable packages,
  ad-hoc ids — not a production or store packaging path.

### Backend chrome is backend business

- `DecorationFn` / `DecorationHeight` removed from core: chrome (the Wayland
  CSD titlebar, mobile accessory bars) is now plain frame wrapping inside the
  backend that owns it; core always sizes the root to `WindowSize`.
- Dropdown menus cap their height to the window and scroll inside instead of
  extending off-screen.

### VirtualList

- **`OutFirstVisible` / `OutLastVisible`** on `VirtualListAttrs`: optional
  `*int` outs for the inclusive index range of rows the list actually built
  this frame (empty list → `-1`). Same timing as `OutScrollOffset`.
- **`VirtualListView_ScrollToIndex(listKey, index)`**: pin that item to the
  top of the viewport (clamped when the tail is short); uses the list’s own
  height walk.

### Customizable widgets (process vs paint)

Default controls split interaction from chrome so apps can skin without
reimplementing hit-testing:

- **`ProcessButtonEvents`** / continuous **`ButtonLook`** — default buttons are
  thin wrappers; demos: `custom-buttons`, `custom-checkboxes`.
- **`ProcessToggleEvents`** — demos: `custom-toggles`.
- **`ProcessSegmentEvents`** — radio and segmented controls; demos:
  `custom-radios`, `custom-segmented`.
- **`ProcessTextInput`** — interaction + plain draw path; demo:
  `custom-textinputs`.
- **`ProcessSlider`** — demo: `custom-sliders`.
- CheckBox / OptionButton / ToggleSwitch share the process helpers.
- **Scrollbars:** `ScrollBarExt` chrome attrs, `GetScrollingState`, per-container
  scroll activity, app-wide **`DefaultScrollBar`** / `SetDefaultScrollBar` /
  `DefaultScrollBarStyle`. **Package default is a modern overlay**
  (transparent track, thin rounded neutral-gray thumb, darker on hover/drag).
  The former white-track + grip look lives in the `custom-scrollbars` demo as
  “Classic.”
- TextInput fills leftover row space by default (compose bars need less glue).
- TextInput **`Placeholder`** (attrs and `TextInputConfig`, plus
  `PlaceholderColor` on the config): dimmed hint text drawn while the buffer
  is empty; draw-only, never masked on password fields.
- Docs: [Custom widgets tutorial](docs/custom-widgets-tutorial.md) (process vs
  paint, chat compose, dark shell + optional bar tint).

### Tutorials and demos

- [Layout shell tutorial](docs/layout-tutorial.md) — progressive multi-panel
  chat chrome with per-step demos and screenshots (`demos/layout-shell/stepNN`).
- [Virtual lists and Measure](docs/virtual-list.md).
- Custom-widget gallery demos under `demos/custom-*`.

### UI runtime packaging (`*UI` / Host / Measure)

- Package frame/runtime state onto a per-window `*UI` with nested `Host` for
  backend I/O (input, window size, IME anchors, …). Process-shared
  caches live on `Resources` / `SharedResources()`.
- New **`Measure(maxSize, fn) Vec2`**: layout-only on a fresh `*UI`, then restore
  the active UI. Nested-safe inside `RunFrameFn`. No process resource sweeps.
- **VirtualList optional height:** `ItemHeight` may be `nil`; the list measures
  each row with `Measure` on `ItemView` under the content width. **No height
  cache** — callers that need a custom/cheap height still pass `ItemHeight`.
- Demo: `demos/measure-list`. Example: `examples/hacker-news-reader` uses
  auto-height for feed, post header, and comments.

### Animation flags

- Per-channel **`AnimFlags`** with explicit `animationsSet` so child attrs can
  OR channels without wiping parent cascade unintentionally.
- `Attrs Animate()` ORs flags; scroll thumbs snap when appropriate.

### Size constraint cascade

- The maximum content size on the cross axis cascades from parent to direct
  children for any child that does not set the max size for that axis.
    * column → (max width - horizontal padding)
    * row → (max height - vertical padding)
  Opt out with `UnsetMaxCross` in `Attrs(...)` or `ModAttrs` (flag survives
  open-time cascade).
- Text layout wraps to the current container's max width instead of taking an
  explicit "Max Width" parameter. For offline measuring, use `ShapeTextMax`.

### Text style cascade

- TextStyle is now an attribute on Containers, and also cascades when unset by
  child container.
- At build time, use `AmendTextStyle` to inherit with modification, or
  `SetTextStyle` to set custom style without inheriting anything.

### Resource cleanup

- Unused image handles are reclaimed after a short period without use, so
  long-running UIs that load many images (e.g. scrolling lists) no longer keep
  every past image in memory for the lifetime of the process.
- Unreferenced immediate-mode file content (and related directory listings) is
  cleaned up on the same schedule, bounding cache growth for path-based reads.

## v0.5.2 — 2026-07-13

### Text input and IME

- **Wayland IME** via `zwp_text_input_v3` (inline preedit + commits).
- **X11 IME via IBus** (Linux): Japanese composition on GNOME Classic / Xorg
  through IBus D-Bus (same path as GTK). Inline preedit is correct.
- **Known issue:** the system candidate/suggestions window may sit off the
  caret on X11 — GNOME Text Editor shows the same offset; not treated as a
  shirei layout bug.
- Expanded default font fallbacks for CJK and Arabic on Linux (e.g. Noto Sans
  CJK JP, Noto Arabic faces; DejaVu with lowest priority as last resort).
- **Bug Fix:** Composition underline no longer bridges into a following RTL run
  (e.g. Japanese preedit before Arabic).

## v0.5.1 — 2026-07-13

A small feature release on top of v0.5.0: inline text styling, a streaming log
view, virtual-list scroll-to-end, and two fixes that mattered once apps started
updating from background goroutines.

### Style spans

- Text can carry **inline style spans** over rune ranges: color, size, weight,
  family, underline, strikethrough, and a per-glyph **background highlight**.
- Build spans with `Span(from, to, base, mods...)` and attach them via
  `WithSpans` / `TextAttrSet.Spans`. Overlapping spans compose as deltas against
  the paragraph base (so bold and a highlight can stack on the same range).
- New text attribute helpers: `TextBackground`, `TextUnderline`, `TextStrike`.
- Demo: `demos/style-spans`. Used in **haystack** to highlight exact match
  substrings in result lines.

### Log view and text ring

- New widgets: **`TextRing`** (fixed-capacity, append-only byte + line store for
  log-like streams) and **`LogView`** (virtualized display of a ring).
- LogView stays **pinned to the bottom** while content arrives; scrolling up
  unpins, and scrolling back to the bottom re-pins. Lines wrap; drag-select
  across lines and copy with Cmd/Ctrl+C.
- Background appends: mutate under `WithFrameLock`, then `RequestNextFrame`
  (same pattern as other async UI).

### Virtual list: scroll to end

- `VirtualListView_ScrollToEnd` scrolls a virtual list so the true tail is
  visible, including while total height is still being learned from partial
  measurement — the foundation LogView's pin behavior uses.
- Related wheel-to-bottom / pin edge cases tightened with tests and a
  `demos/vlist-pin` playground.

### Bug fixes

- **Linux and Windows: background `RequestNextFrame` now wakes a settled
  window.** On Wayland (and the same class of bug on X11/Win32), once a frame
  settled the loop only redrew on input, so apps like process_monitor stopped
  updating until the mouse moved. Idle loops now honor `FrameRequested()` the
  way the macOS display link already did.
- **System fonts initialize on package import.** `InitFontSubsystem` runs from
  `init()`, so font enumeration and headless/render paths work even when a
  backend never called it explicitly. Backends no longer need to call it at
  `Run` time.

### Misc

- Early **behavioral tests** for a few sticky UI paths (text input, virtual-list
  scrolling, streaming log pin). Not unit tests and not full end-to-end app
  runs: they exercise how widget state evolves over successive frames in
  response to user input.
- README rewrite (motivation, features, getting started).
- Example READMEs refreshed; haystack gains match highlighting.

## v0.5.0 — 2026-07-10

This release replaces Shirei's rendering foundation. The previous release
(v0.0.3-alpha) leaned on Gio as a stopgap backend, and the cross-platform
story was rough. Shirei now renders entirely in Go and ships its own platform
backends: the same UI looks identical on every platform, and an idle window
costs almost nothing.

### Rendering and platforms

- Own platform backends for macOS, Windows, and Linux (X11 and Wayland). Gio
  is no longer a dependency.
- Cross-compiling from macOS to Linux and Windows now produces working
  binaries — the cross-platform story is real, not aspirational.
- The entire UI is rendered in software, in Go. The platform layer only opens
  the window, routes input events, and hands Shirei a buffer to draw into. As a
  result the UI is pixel-for-pixel identical across platforms.

### Performance and resource use

- Near-zero CPU when idle: when nothing changes, Shirei does no work. (The old
  Gio backend consumed noticeable CPU even at rest.)
- The renderer blits directly into the OS-managed buffer, avoiding an extra
  copy on the way to the screen.
- The rendering pipeline was reworked at the architectural level to avoid
  repeating work it has already done from one frame to the next.

### Text input and IME

- Text editing is now mature: all cursor movements and the keyboard shortcuts
  you'd expect are supported. Because the editor depends on no OS text
  facility, its behavior is identical on every platform by default.
- IME (composition input) support landed for macOS and Windows. IME requires
  backend cooperation and is not yet implemented for the X11 and Wayland
  backends.
- Multi-line editing, with no separate widget: the same text field is single-
  or multi-line by configuration. A single-line field caps to one line with
  wrapping off; a multi-line field wraps with no line cap; intermediate line
  caps also work, with wrapping on or off.

### Container identity and messaging

- The system that matches containers from frame to frame — so they retain their
  state, including the React-like local state you can hook onto them — was
  reworked. Identity now derives from a parallel position tree, scoped by
  container type, so temporarily inserting a different kind of container between
  siblings no longer steals their identity.
- Fixed a subtle bug where an identity could change on its own when the value
  behind a user-supplied id was copied, changing the underlying bytes.
- User-supplied identifiers are now **keys**, not ids: a key no longer has to be
  globally unique. It simply lets a container move within its parent and be
  addressed from your code.
- New messaging channel between your code and widgets, addressed by a
  `{widget, key, command}` tuple carrying arbitrary data. Widgets consume
  messages sent to them the same way. This is what lets you, for example, save
  and restore a virtual list's scroll position — used by the find-in-files
  example to remember each search tab's scroll offset.

### Correct first-frame sizing

- Resolved a long-standing immediate-mode quirk: on a container's first frame
  its size isn't known yet, so a child that sizes itself relative to its parent
  received wrong information. The frame cycle now detects this and re-runs the
  UI builder — without presenting the result — so that on the second run sizes
  have resolved and children size themselves correctly, with no visible flash.
- Related "scroll to" glitches on the virtual list view (flashing, jumping) are
  resolved, building on the multi-pass logic above plus several subtler fixes.

### Widgets and theming

- The built-in widgets were restyled: lighter and more colorful, away from the
  previous gray, bulky look.
- Many built-in widgets now accept an accent color to retheme them.
- Several important widgets were promoted out of the examples and into the
  widgets package.

### Example programs

- A set of example desktop programs that demonstrate building genuinely useful
  tools with Shirei, along with effective techniques. They compile quickly,
  produce small binaries, and cross-compile to every supported platform.
  Highlights:
  - a recursive disk-usage tool that computes sizes across an entire tree
    quickly;
  - a find-in-files search that returns matches quickly without depending on
    ripgrep;
  - see_pprof, which renders a flame graph using Shirei's own drawing
    primitives and stays smooth while doing it.

### Audio

- A basic audio-output interface, driven mainly by the piano example.

### API changes (upgrading from v0.0.3)

The public surface was cleaned up, documented with godoc comments, and made
more self-explanatory. The most visible renames:

- `Layout` / `LayoutId` → `Container` / `ContainerWithKey`; `Element` /
  `ElementId` → `Element` / `ElementWithKey`.
- The attribute struct `Attrs` → `AttrSet` (and its terse fields were given
  full names, e.g. `Sz` → `FontSize`, `Clr` → `TextColor`, `BG` → `Background`);
  `TextAttrs` → `TextAttrSet`.
- The attribute builders moved into the core package as `Attrs()`,
  `AttrsWith()`, `TextAttrs()`, `TextAttrsWith()`.
- Container identifiers are now keys (see *Container identity and messaging*),
  and the opaque frame-to-frame handle is `ContainerId`.
- Popups are drained automatically by the frame loop; applications no longer
  call a popups host.

### Still in progress

- IME for the X11 and Wayland backends.

## v0.0.3-alpha20260215 — 2026-02-15

- Added a Z-index for controlling container draw order.
- Added a disk-usage example utility.

## v0.0.2-alpha20251104 — 2025-11-04

- Added shadows, scrollbars, a virtual list view, and a large-text view.

## v0.0.1-alpha20250930 — 2025-09-30

The first public release — the foundation of the framework.

- Immediate-mode layout engine: flexbox-style containers with main- and
  cross-axis alignment, growth and expansion, wrapping, scrolling, and clipping,
  rendered through a Gio backend.
- HSLA color model (CSS-style ranges) paired with an animation system — animated
  sizes, positions, and cascading properties — chosen so animating between
  colors behaves sensibly.
- Text: shaping, bidirectional (RTL) text, line breaking, and wrapping, with lazy
  font loading, system fonts, and font-collection support.
- Text input: a text field with cursor movement, selection, and cut/copy/paste,
  plus password and directory-picker variants, tab focus cycling, and autofocus.
- Widgets: buttons, checkboxes, toggle switches, context and action menus, popup
  panels, and reusable drag-and-drop.
- Styling: borders, gradients, transparency, and a concise attribute-builder API.
