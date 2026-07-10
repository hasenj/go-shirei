# cocoabackend — a direct-macOS backend for shirei

Status: M0–M6 done (window, run loop, full rendering, input, clipboard).
M7 (IME — the payoff) and M8 (hidpi/resize polish) remain; M8 frame-economy
is already in place. This file is the build plan + reference. Update the
checkboxes as milestones land.

Author of plan: Claude Opus 4.8 — 2026-06-28.

Progress log:
- 2026-06-28: M0–M2 landed. Rect rendering (solid, rounded per-corner,
  vertical gradient, border, clip-to-rounded, transparency layers) verified
  via an offscreen `RenderToPNG` snapshot — all primitives correct, coordinate
  system right. Live window launches and runs the AppKit loop without crashing.
  Files: `cocoa.h`, `cocoa_darwin.m`, `cocoa_darwin.go`, `example/`.
- 2026-06-28: M4 (text/glyphs) — outline fill per glyph, ported transform.
  Verified offscreen: Latin, bold/sizes, Japanese kanji+hiragana, mixed-script
  fallback all correct. Glyph bitmap cache still deferred (perf only).
- 2026-06-28: M5 (images + shadows) — CGImage draw, upright orientation
  verified; drop shadow renders. M6 (clipboard) — NSPasteboard copy/paste with
  one-frame-deferred paste; builds clean.
- 2026-06-28: M3 (input) — mouse/scroll/keys/modifiers wired; example gained a
  TextInput + button. Code-complete, compiles, live app runs without crashing.
  NOT yet interactively tested: clicks/typing/scroll behavior, and the scroll
  sign is a guess. Real text entry moves to NSTextInputClient in M7.
- Verification method note: all rendering verified via `example --png out.png`
  (offscreen CGBitmapContext, same drawSurface code). Live/interactive behavior
  and IME require a hands-on session.
- 2026-06-28 (hands-on feedback fixes): (1) window background — drawRect now
  clears to white before painting surfaces (live path was drawing onto the dark
  system window; offscreen already cleared white). (2) clicks — discrete input
  events (button down/up, key down) now run a synchronous frame via
  `cocoa_displayNow` so down+up can't coalesce into one frame and drop the click
  when idle. Known-still-high CPU is the missing glyph bitmap cache (deferred M4
  item): every frame re-fills each glyph outline; a focused TextInput's cursor
  blink forces continuous frames.
- 2026-06-28 (clicks CONFIRMED working): verified with a SHIREI_TRACE build —
  each mouseDown/Up runs its own synchronous frame ([draw] logs 1ms after the
  event, before the next event); the button sees hovered=true + the right Mouse
  value; coordinates match the screenRect. M3 input is now hands-on confirmed.
  Lesson: the earlier "clicks don't register" report was a STALE BINARY built
  before the cocoa_displayNow fix — rebuild demos after backend changes. (On
  this machine [view display] is synchronous, so the layer-backing worry was
  moot.) Scroll sign still unverified; typing works via keyDown.
- 2026-06-28 (the "click twice" bug — ACTUAL root cause, supersedes the display
  theories above): the real cause was **macOS `acceptsFirstMouse`**. When the
  window isn't the active/key window — common when launched from a terminal,
  especially after a slow `go clean -cache` build steals focus — the first click
  only activates the window and is swallowed; you must click again. The tell was
  that a *cached* build worked but a *clean* (slow) build needed two clicks:
  identical binary, so it had to be environment/timing, not code. Fix: override
  `- (BOOL)acceptsFirstMouse: { return YES; }` on the view. All the earlier
  display-timing fixes (cocoa_displayNow, decoupled produce/paint, post-input
  render window, displayIfNeeded + CATransaction flush) were chasing a symptom
  and were removed. Lesson for future debugging: a single screenshot via
  computer-use could NOT observe this, and `printf`/SHIREI_TRACE appeared to
  "fix" it (red herrings) — trust the cached-vs-clean signal.
- Rendering model (final): the timer repaints only while a frame is wanted
  (`gWantsFrame` from NextFrameRequested) or input was recent (0.5s window);
  idle otherwise. Confirmed hands-on: clicks register first-try, idle CPU drops.
  Remaining CPU during interaction is the still-missing glyph bitmap cache (M4).
  M3 input is done. Scroll sign still unverified.

## Why this exists

See `../notes/opus-ime-assessment.txt` for the full reasoning. Short version:

1. **IME.** Gio folds the IME composing/marked region into the buffer as
   committed text and never exposes the compose region (its own `widget.Editor`
   has no concept of preedit either). For Japanese input the underlined
   uncommitted region is part of the expected flow, not cosmetic. A direct
   `NSTextInputClient` view hands us `setMarkedText:` with the marked range,
   so we can finally fill `shirei.InputState.Composition`.

2. **CPU.** Gio runs `textureCache.frame()`
   (`gioui.org@v0.8.0/gpu/caches.go:73`) every frame: a full map scan + per-entry
   map write-back over every cached texture (≈ one per painted glyph), whether
   or not anything changed. This is the profiled CPU sink — a resource-GC design
   choice, *not* an immediate-mode tax. A custom backend repaints only when
   shirei reports a change and never sweeps the glyph cache per frame.

3. **Control.** shirei already thinks like an editor (it has the cursor-geometry
   functions the OS asks for). Talking to AppKit directly lets shirei answer the
   OS's queries from its own layout instead of maintaining a shadow editor for
   Gio.

This backend is **additive**. The Gio backend stays. Keep API parity so demos
swap by changing one import:

    app "go.hasen.dev/shirei/giobackend"   ->   app "go.hasen.dev/shirei/cocoabackend"

i.e. expose the same `SetupWindow(title string, w, h int)` and
`Run(frameFn shirei.FrameFn)`.

## Constraints

- Package is **darwin-only**. Every file gets `//go:build darwin`. The rest of
  shirei (and other backends) must still build on other platforms. Precedent:
  the empty `ebitenbackend`.
- Implementation via cgo + an Objective-C `.m` file, following the same pattern
  Gio uses (`gioui.org@v0.8.0/app/os_macos.m` + `os_macos.go` with `//export`
  Go callbacks). This is the proven, dependency-free path. Avoid a heavyweight
  Go<->ObjC binding lib unless the raw cgo proves unworkable.
- AppKit main-thread rules: window/view creation, event handling, and
  `NSTextInputClient` callbacks must run on the main thread. `Run` should take
  over the main goroutine (lock it with `runtime.LockOSThread`) and run
  `[NSApp run]`, like Gio's `app.Main()`.

## The backend contract (what shirei needs from us)

From `../notes/architecture.md` and the Gio backend `../giobackend/gioshirei.go`:

A backend must, per frame:
1. Set `shirei.WindowSize` (in points, not pixels).
2. Translate platform input into:
   - `shirei.InputState` — cumulative: `MousePoint`, `MouseButton`, `DownKeys`,
     `Modifiers`, `Composition`.
   - `shirei.FrameInput` — this-frame-only: `Mouse` (click/release), `Motion`,
     `Scroll`, `Key`, `Text`.
3. Call `out := shirei.RunFrameFn(frameFn)`.
4. Draw `out.Surfaces` in order, maintaining a clip stack and a transparency
   stack (the surfaces carry push/pop ops).
5. Honor clipboard: write `out.Copy` to the pasteboard; if `out.Paste`, read the
   pasteboard and feed it back as `FrameInput.Text` next frame.
6. Re-render / schedule another frame only when
   `out.NextFrameRequested || out.FrameHasChanges`.

`shirei.CaretPos` (set by the text widget) is where the IME candidate window
should anchor.

That's the whole surface. Layout, hover, focus, animation, text shaping all
happen inside the core.

---

## Milestones

Order favors visible progress early (mirrors the layout-test rect rasterizer),
then input, then text, then the IME payoff.

### M0 — window + run loop, blank
- [x] `//go:build darwin` skeleton: `cocoa_darwin.go` (cgo + Go API + `//export`
      callbacks) and `cocoa_darwin.m` (NSApplication, NSWindow, custom NSView).
- [x] `SetupWindow` creates the window; `Run` locks the main thread, sets up the
      app, and runs the event loop.
- [x] A `ShireiView : NSView` subclass. Override `isFlipped` → `YES` so the view
      uses a **top-left origin, y-down** coordinate system that matches shirei
      (avoids flipping every rect).
- [x] `drawRect:` exists, clears to a test color. Window appears.
- **Done when:** a window opens and shows a solid background; closing it exits
      cleanly.

### M1 — plain filled rects
- [x] Minimal frame driver: in `drawRect:` (or a display callback), set
      `WindowSize` from the view bounds, call `RunFrameFn(frameFn)`, iterate
      `out.Surfaces`, and fill each `Surface.Rect` with `HSLAColor(Color1)`
      using `CGContextFillRect` (ignore corners/gradient/border/glyph/image/clip
      for now — exactly what `../layout_tests/bitmap.go` does).
- [x] Convert `shirei.Vec4` HSLA → `CGColor` via `shirei.HSLAColor` (gives
      `color.NRGBA`) → `CGColorCreateGenericRGB`.
- **Done when:** a hand-written `frameFn` of nested `Layout`/`Element` rectangles
      renders with correct positions/sizes (compare against the Gio backend).

### M2 — full rect rendering (corners, gradient, border, clip, alpha)
- [x] **Rounded corners.** Build a `CGPath` with per-corner radii
      (`Surface.Corners` = `[topLeft, topRight, bottomRight, bottomLeft]`, same
      order as `gioshirei.go`'s RRect NW/NE/SE/SW). Reuse the arc construction
      already in `../shadow.go` (`q = 4*(sqrt2-1)/3`, MoveTo/LineTo/CubeTo per
      corner). Fill the path instead of the rect.
- [x] **Gradient.** When `Color2 != Color1`, fill with a **vertical** linear
      gradient (top = `Color1`, bottom = `Color2`) clipped to the rounded path:
      `CGContextSaveGState` → add path → `CGContextClip` →
      `CGContextDrawLinearGradient` (start = rect top, end = rect bottom) →
      restore. Matches `gioshirei.go:279`.
- [x] **Border.** When `Surface.Stroke > 0`, stroke the rounded path with
      `Stroke` width and `Color1` (border surfaces carry the border color in
      `Color1`/`Color2`).
- [x] **Clip stack.** `Surface.Clip == ClipPush` → `CGContextSaveGState` +
      add rounded path + `CGContextClip`; `ClipPop` → `CGContextRestoreGState`.
      Keep a depth counter; assert balanced at end of frame (Gio panics on
      imbalance — do the same to catch bugs early).
- [x] **Transparency stack.** `Surface.Transparency > 0` →
      `CGContextSetAlpha(1 - t)` wrapped in `CGContextBeginTransparencyLayer`;
      `Surface.PopTransparency` → `CGContextEndTransparencyLayer`. (Transparency
      layers compose the subtree, then apply alpha once — the correct semantics
      for `out.Surfaces`' push/pop model.)
- **Done when:** demos that use rounded cards, gradients, borders, and clipped
      scroll regions render correctly without text.

### M3 — input plumbing
- [x] Mouse: `mouseMoved/Dragged/Down/Up`, `rightMouse*`. Convert
      `locationInWindow` → view coords (already top-left via `isFlipped`).
      Coords are in **points**; no backing-scale division needed for points.
      Set `InputState.MousePoint`, `MouseButton`; set `FrameInput.Mouse`
      (`MouseClick`/`MouseRelease`) and accumulate `FrameInput.Motion`.
      (NSView needs a tracking area for `mouseMoved`.)
- [x] Scroll: `scrollWheel:` → `FrameInput.Scroll` from `scrollingDeltaX/Y`
      (handle precise vs line-based deltas).
- [x] Keys: `keyDown:` → map `NSEvent.keyCode` / special keys to
      `shirei.KeyCode` (use `gioshirei.go`'s `mapKeyCode` table as the spec for
      what shirei expects); set `FrameInput.Key`, maintain `InputState.DownKeys`
      on down/up.
- [x] Modifiers: `flagsChanged:` / `NSEvent.modifierFlags` → `shirei.Modifiers`
      (`ModCtrl/ModCmd/ModShift/ModAlt/ModSuper`).
- **Done when:** hover highlights, button presses, scrolling, and tab-focus
      cycling work in a demo.

### M4 — text / glyphs
- [x] Each text `Surface` carries `FontId` + `GlyphId` + `GlyphOffset` and its
      `Rect`/`Color1`. Get the outline via `shirei.GlyphOutline(fontId, glyphId)`
      and build a `CGPath` from its segments (Move/Line/Quad/Cube — same switch
      as `gioshirei.go:FontGlyphPathSpec`).
- [x] Transform (mirror `gioshirei.go:312-339`): glyph outlines are in font units
      and **y-up**, so apply a local flip; scale by
      `rect.Size[1] * face.InvUPM`; place the baseline at ~`0.82 *
      rect.Size[1]` from the top (empirical — tune visually); offset by
      `GlyphOffset` and `rect.Origin`. Fill with `HSLAColor(Color1)`.
      NB: because the view is flipped (M0), re-verify the glyph y-flip sign by
      eye; the 0.82 factor and flip were tuned for Gio's coordinate setup.
- [ ] **Glyph bitmap cache** (the one cache that matters). Key:
      `(FontId, GlyphId, quantizedSize, colorRGBA, backingScale)`. Rasterize the
      glyph once into a small `CGImage`/bitmap; blit on reuse. Turns text from
      N path-fills into N blits.
- [ ] **Do NOT sweep the cache every frame** (the Gio mistake). Either never
      evict (a few thousand small glyph bitmaps is trivial memory) or stamp a
      last-used frame number and evict lazily only when over a budget.
- **Done when:** mixed-script text renders crisply at 1x and 2x (retina), and a
      text-heavy frame's CPU during caret blink is low.

### M5 — images + shadows
- [x] Image surfaces: `shirei.LookupImage(ImageId).RGBA` → `CGImage` →
      `CGContextDrawImage`. When `ImageScale`, scale to fit by height (mirror
      `gioshirei.go:341-363`). Cache the `CGImage` per `ImageId`.
- [x] Shadows need no special code: shirei core emits the blurred shadow as an
      ordinary **image surface** (`_IMBlurShadow` in `../shadow.go`), so M5's
      image path covers it. Verify shadows appear behind cards.
- **Done when:** demos with images and drop-shadows match the Gio backend.

### M6 — clipboard
- [x] `out.Copy != ""` → write to `[NSPasteboard generalPasteboard]`.
- [x] `out.Paste` → read the pasteboard string, deliver as `FrameInput.Text`
      on the next frame.
- **Done when:** Cmd-C / Cmd-V round-trips in the text input demo.

### M7 — IME via NSTextInputClient (the payoff)
This is the reason the backend exists.

**The implementation spec is `../notes/ime-plan.md` — follow it exclusively.
This section is only an inventory of existing cocoa hooks plus status; it
contains NO protocol design.** An earlier revision of this section carried
the gio-era IMEState/range-edit design (assessment §5) and contradicted the
spec on several methods (doCommandBySelector:, characterIndexForPoint:,
attributedSubstring, retained-editor answers); that design is superseded —
if anything here ever disagrees with ime-plan.md, ime-plan.md wins.

Status (2026-07-06): IN PROGRESS. The widget half now renders
`InputState.Composition`/`CompositionSel` inline and is covered by headless
tests/smoke snapshots. Backend B1 is wired: normal keyDown events route
through `interpretKeyEvents:`, committed text arrives via `insertText:`, and
the old printable-text relay in `shireiKeyDown` is gone. Backend B2 is wired:
`ShireiView` owns marked-text shadow state, answers the core
`NSTextInputClient` queries locally, and notifies Go with UTF-16 selection
offsets converted to rune offsets. Backend B3 is wired: the IME candidate
anchor is converted from `shirei.CaretPos`/`CaretHeight` to AppKit screen
coordinates. Backend B4 is wired: mouse/window interruptions commit marked
text, reset the AppKit input context, and flush the commit before focus-changing
clicks are delivered. The backend half still needs the live Japanese IME
checklist in ime-plan.md W3.

Existing hooks the spec builds on (inventory only):
- `shireiKeyDown` in `cocoa_darwin.go` now relays key identity only; committed
  text is queued by `shireiCommitText` from the AppKit `insertText:` callback.
- Deferred delivery pattern: the `pendingPaste`/`hasPendingPaste` fields in
  `cocoa_darwin.go` now shares the accumulating `pendingText` path used by B1
  committed text.
- Caret anchor for `firstRectForCharacterRange:` is already published by
  the widget as `shirei.CaretPos`/`shirei.CaretHeight`
  (`../widgets/textinput.go` caret block); B3 now uses those values for the
  candidate rect.
- `keymap_darwin_test.go` pins the vkey→KeyCode map that B1's gated
  special-key relay keeps using.

**Done when:** the 15-step human checklist in ime-plan.md W3 passes.

### M8 — frame economy + hidpi polish
- [x] Repaint only when shirei reports change: a 60fps NSTimer calls
      `setNeedsDisplay` only while `gWantsFrame` is set, and `shireiDraw` sets it
      from `out.NextFrameRequested`. Idle UI ⇒ no redraws. (Implemented in M0;
      a `CVDisplayLink` could replace the timer later for vsync alignment.)
- [ ] HiDPI: likely automatic — with `isFlipped` and drawing in points, AppKit's
      retina backing store makes CG render vectors/text at device resolution for
      free. Unverified on a retina panel; revisit when the glyph bitmap cache
      lands (that cache must be keyed by `backingScaleFactor`).
- [ ] Resize: `NSView` gives continuous `drawRect:` during live resize (the
      behavior we wanted over ebiten). Implemented-by-default; not yet eyeballed.
- **Done when:** idle CPU ≈ 0, animation CPU is low and flat, live resize is
      smooth — measured against Gio.

---

## Surface → Core Graphics reference

`shirei.Surface` (see `../shirei.go:287`) is a flat, pointer-free struct. The
draw loop walks `out.Surfaces` in order with two stacks (clip via gstate,
transparency via transparency layers). Pseudocode for one surface:

```
ctx = current CGContext (from the flipped NSView)

# 1. transparency push (opens a layer that gets alpha applied on pop)
if s.Transparency > 0:
    CGContextSaveGState(ctx)
    CGContextSetAlpha(ctx, 1 - s.Transparency)
    CGContextBeginTransparencyLayer(ctx, NULL)

path = roundedRectPath(s.Rect, s.Corners)   # per-corner; reuse shadow.go math

# 2. clip push
if s.Clip == ClipPush:
    CGContextSaveGState(ctx)
    CGContextAddPath(ctx, path); CGContextClip(ctx)

# 3. content — exactly one of: glyph, image, or rect
if s.FontId > 0 and s.GlyphId > 0:
    drawGlyph(ctx, s)                 # outline->CGPath fill, or cached bitmap blit
elif s.ImageId > 0:
    drawImage(ctx, s)                 # CGContextDrawImage, scale by height if ImageScale
else:
    if s.Stroke == 0:
        fill(path) with solid Color1 OR vertical gradient Color1->Color2
    else:
        stroke(path) width=s.Stroke color=Color1   # border

# 4. clip pop  (border/clip-pop surfaces set ClipPop)
if s.Clip == ClipPop:
    CGContextRestoreGState(ctx)

# 5. transparency pop
if s.PopTransparency:
    CGContextEndTransparencyLayer(ctx)
    CGContextRestoreGState(ctx)
```

Per-field cheat sheet:

| `Surface` field | Core Graphics |
| --- | --- |
| `Rect` (Origin, Size) | rect in **points**, top-left origin (flipped view) |
| `Corners` `[tl,tr,br,bl]` | manual rounded `CGPath` (per-corner radii) |
| `Color1`/`Color2` | `HSLAColor()` → `CGColor`; equal = solid, differ = vertical gradient |
| `Stroke` | `> 0` ⇒ stroke the path (border) instead of fill |
| `ImageId` / `ImageScale` | `CGContextDrawImage`; scale-to-fit by height |
| `FontId`+`GlyphId`+`GlyphOffset` | glyph outline → `CGPath` fill (or cached bitmap) |
| `Clip` (`ClipPush`/`ClipPop`) | `SaveGState`+`Clip` / `RestoreGState` |
| `Transparency` / `PopTransparency` | `BeginTransparencyLayer`+`SetAlpha` / `EndTransparencyLayer` |

Coordinate notes:
- **View:** override `isFlipped` → `YES` so rects/images use top-left origin,
  y-down, matching shirei. No per-rect flipping.
- **Glyph outlines:** are y-up in font space — they need their own local flip
  when building the path, independent of the view flip. The exact transform
  (scale `rect.Size[1]*face.InvUPM`, baseline at ~0.82·height, GlyphOffset) is
  ported from `gioshirei.go:312-339`; re-verify the flip sign by eye under the
  flipped view.
- **Gradient direction:** vertical, top(`Color1`)→bottom(`Color2`).

## Open questions / risks

- **cgo/ObjC bridging ergonomics.** Follow Gio's `.m` + `//export` pattern. The
  callback direction (ObjC view → Go) carries window handle; the command
  direction (Go → ObjC) sets title/needs-display. Memory + autorelease pools
  around frame work need care.
- **Retained state for IME.** The OS queries the input client synchronously,
  outside the frame — so M7 keeps view-side ObjC shadow state (marked text +
  range) and only notifies Go, per ime-plan.md B2. (Historical note: the
  original answer here was an `IMEState` shadow editor in shirei core, from
  assessment §5 — superseded; nothing IME-shaped lands in core beyond
  Composition/CompositionSel.)
- **Candidate-window placement** (`firstRectForCharacterRange:`) is the highest
  IME risk — budget time to get it pixel-right.
- **One-frame settling.** Reporting editor state to the OS lags the frame by one
  tick, same fixed-point pattern shirei already documents. Expect it; don't
  fight it.
- **Dirty rects.** shirei currently exposes a single whole-frame change hash
  (`computeSurfacesHash`), not per-region dirt. Full-frame repaint-on-change is
  the model for now; per-region dirty repaint would be a shirei-core enhancement
  to defer until measured necessary.

## Scroll performance (2026-06-28) — the 10fps→60fps chain

demo2 scrolling felt ~10fps. It was NOT the glyph cache; it was the render/present
loop. Diagnosed with `SHIREI_PERF=1` (kept in `perf_darwin.go`) + a throwaway
`fpstest` AppKit app. The fixes, in the order they mattered:

1. **Frame driver: `NSTimer` → `CADisplayLink`.** The 60fps `NSTimer` is low
   priority and was starved to ~2-15/s during scroll. `CADisplayLink` (macOS 14+,
   `[view displayLinkWithTarget:...]`) is display-synced and serviced reliably
   during event tracking. A minimal app proved 60fps is achievable in our exact
   Go `LockOSThread` + `[NSApp run]` setup.
2. **Input is data, not events.** Handlers used to render synchronously per event
   (`produce`); now they only update state + note the time (`noteInput`), and the
   display-link tick renders once per frame from the latest data. Matches shirei's
   model and avoids per-scroll-event full renders.
3. **Render offscreen, blit one image.** THE big one. Drawing ~2000 surfaces
   straight into the window's **layer-backed** `drawRect` context makes its commit
   O(surfaces) — the ops are deferred and rasterized by the window server at
   commit (~40ms, hidden from a CPU-side timer). Render into a plain
   `CGBitmapContext` instead (immediate software raster, same code path as `--png`)
   and blit ONE image to the window → window-context commit is O(1).
   (`cg_offscreen_begin`/`cg_offscreen_blit`.)
4. **Skip invisible content.** After #3 we were honestly CPU-bound at ~40ms paint;
   the breakdown showed ~30ms was ~220 surfaces/frame that were **fully
   transparent plain fills** (shirei emits a background surface per container) —
   rasterizing a rounded-rect path to paint nothing. `surfaceHasVisibleContent`
   skips transparent no-op fills AND anything entirely outside the viewport
   (scroll culling). paint 40ms→~11ms, 60fps.

Lesson for the next perf cliff: measure where the frame time actually goes before
changing code (the layer-backed commit cost is invisible to a `drawSurface`-loop
timer; only timing the full `renderFrame` + bisecting draw-vs-produce found it).

## References

- `../notes/opus-ime-assessment.txt` — IME feasibility study, the Gio CPU
  finding. Historical: its §5 `IMEState` design is superseded by
  `../notes/ime-plan.md`, which is the M7 implementation spec.
- `../giobackend/gioshirei.go` — the working reference for the backend contract,
  input mapping, glyph transform, gradient, clip/alpha stacks.
- `../shadow.go` — per-corner rounded-rect path construction to reuse.
- `../layout_tests/bitmap.go` — the minimal rect rasterizer; M1 mirrors it.
- `../notes/architecture.md` — the backend contract and frame economy.
- Gio's own `gioui.org@v0.8.0/app/os_macos.m` + `os_macos.go` — reference for
  the cgo/NSView/NSTextInputClient plumbing (what to imitate and what to do
  differently re: the per-frame cache sweep).
```
