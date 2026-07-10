# Changelog

Notable changes to Shirei. This is the first maintained changelog; earlier
releases predate it, which is why the history begins at v0.5.0.

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
