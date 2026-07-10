# internal/wayland

Wayland protocol bindings, vendored from github.com/neurlang/wayland
v0.4.3 (MIT — see LICENSE) and modified for shirei. Upstream is a UI
library; we only need its protocol layer, and owning the code lets the
event loop grow what shirei's backend needs (deadline-based dispatch,
compositor flush provocation — see wl/context.go).

Local changes vs upstream:
- import paths rewritten to go.hasen.dev/shirei/internal/wayland/...
- yalue/native_endian replaced with encoding/binary's LittleEndian
  (every shirei target is little-endian)
- wl.Context gains RunTimeout (read-deadline dispatch); readEvent clears
  the deadline after the header read so a timeout can never split an
  event mid-read
- cursorshape/ is shirei's own package (not from upstream): the staging
  wp-cursor-shape-v1 protocol, originally hand-written in the backend
- wlcursor/ + wlcursor/xcursor/ (the cursor-theme loader) have the
  swizzle assembly deleted outright: Xcursor pixels are already wl_shm
  ARGB8888 little-endian, so the raw bytes pass through — upstream's
  R<->B swap both crashed on arm64 (hand-written NEON) and shipped
  color-swapped cursors (invisible on grayscale arrows). xcursor's
  parser is rewritten (bounds-checked, single Pix slice) and
  nearestImages actually picks the nearest size (ties to the larger),
  instead of exact-match-or-first
