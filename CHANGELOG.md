# Changelog

Notable changes to Shirei. This is the first maintained changelog; earlier
releases predate it, which is why the history begins at v0.5.0.

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
