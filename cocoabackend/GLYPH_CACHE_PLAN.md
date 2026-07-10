# Glyph bitmap cache — plan

Status: implemented 2026-06-28 (commit 5f71a2a); headless `--png` verified incl. a
simulated scale=2 pass. **Pending: live-window confirmation** on a real Retina
display (crispness + idle CPU). Sibling to PLAN.md; this covers only the
shared glyph-caching work (PLAN.md M4 "glyph bitmap cache" perf item, promoted to
core). Read PLAN.md for the backend as a whole.

## Goal

Stop re-deriving glyphs every frame. Today both backends re-extract a glyph outline
each frame; the cocoa backend additionally rebuilds a CGPath and re-fills it every
frame (see `drawGlyph` in cocoa_darwin.go). Replace this with **rasterize-once →
cache an alpha bitmap → blit the cached bitmap (tinted) each frame**, i.e. a glyph
becomes "just another cached bitmap" that flows through the same blit path as images
and shadows.

This is deliberately a **software-rendering** design (we render glyph coverage in Go,
not via CG's rasterizer). That is a conscious choice — see "Tradeoffs" below.

## The design (decided across discussion — do not relitigate without the user)

Three layers, only some of which are platform-agnostic:

| Layer | What | Home |
|---|---|---|
| Outline extraction (font tables → vector segments) | `shirei.GlyphOutline` | core (already shared) |
| Rasterization (outline → alpha coverage bytes) | NEW | **core** (software, `x/image/vector`) |
| Cache + eviction (LRU, byte-budgeted) | NEW | **core** |
| Wrap bytes into a platform handle + tinted blit | NEW | backend (the only irreducible per-backend bit) |

An **alpha coverage bitmap is genuinely cross-backend** (raw bytes): cocoa wraps it
in a CGImage mask, a future GL backend uploads a texture, `bitmapbackend` memcpys it.
This is unlike a `CGPath`/`clip.PathSpec`, which is a platform handle and therefore
*cannot* live in core. That asymmetry is why the cache belongs in core but the
wrapping does not.

## HARD CONSTRAINTS (user-specified — must hold)

1. **Core never calls a backend-provided callback.** Data flows one way: core →
   `FrameOutputData` → backend. If the backend needs to know what core did this
   frame, core *surfaces it as information*, and the backend reacts on its own side.
   - Concretely: core surfaces two per-frame delta lists,
     `FrameOutputData.GlyphsAdded []GlyphKey` and `GlyphsEvicted []GlyphKey`.
   - The item in the lists is the **key** `{FontId, GlyphId, Px}`, not the bitmap.
     The backend fetches bytes for added keys via a getter when it wants them.
2. **giobackend must stay zero-cost / unaffected.** All core additions are additive.
   The rasterize/evict pass is gated on a byte budget (0 = disabled); gio leaves it
   0, so gio pays nothing and its own path cache is untouched.

## Data flow (one-directional, no callbacks)

```
Core, inside RunFrameFn (only when GlyphCacheBudgetBytes > 0):
  after surfaces are produced, one O(n) pass over them:
    • for each glyph surface: k = GlyphKeyForSurface(s)
        - touch k in the LRU (most-recently-used)
        - on miss: rasterize the glyph to an alpha bitmap, insert  → append k to `added`
    • evict from the LRU tail while over the byte budget            → append k to `evicted`
  fill FrameOutputData.GlyphsAdded / .GlyphsEvicted (reset each frame)
  getter: GlyphBitmap(k) -> (W, H, originX, originY, alpha []byte, ok)

Backend (cocoa), each produced frame, BEFORE drawing:
  for k in out.GlyphsEvicted: CGImageRelease(handles[k]); delete(handles, k)
  for k in out.GlyphsAdded:   b := GlyphBitmap(k); handles[k] = makeMask(b)
  then drawGlyph(s): k = GlyphKeyForSurface(s); blit handles[k] tinted with Color1
```

`handles` is a plain `map[GlyphKey]<CGImage>` with **no eviction logic of its own**;
the two delta lists keep it a perfect mirror of core's cache.

### Correctness invariant

Eviction runs *after* all of this frame's keys are touched, and we never evict a key
touched this frame (it is most-recently-used). So **no key drawn this frame is ever
evicted this frame** → after the backend applies the deltas, every glyph it is about
to draw has a handle present. Frame to frame, `handles` == core cache keys.

### Why this avoids the gio CPU sink

The whole reason for this backend includes Gio's `gpu/caches.go textureCache.frame()`
cost: a **full-map scan + per-entry write-back every frame**. We must NOT reintroduce
that. Per frame our cost is: O(glyphs drawn) touches (paid anyway to draw) + O(added)
+ O(evicted). **No full-map scan, no per-entry write-back.** This requires a real
O(1) LRU (map + intrusive `container/list`), touch = move-to-front, evict from tail
only when over budget. `dboslee/lru` (vendored) likely can't surface evictions *as
data* (no-callback constraint), so core gets a small purpose-built LRU (~40 lines).

## Key decisions / refinements

- **`GlyphKey{FontId, GlyphId, Px}`** where `Px = round(Rect.Size[1] * WindowScale)`
  is the glyph box height in **device pixels**. Device px subsumes the backing scale:
  16pt@2x and 32pt@1x both → Px 32 → one shared bitmap (correct: same physical
  pixels). So a separate `Scale` field is unnecessary. (Width is determined by the
  font for a given Px, so height alone keys the size.)
- **Single source of truth for the key:** `shirei.GlyphKeyForSurface(s) (GlyphKey, bool)`
  used by *both* the core pass and the backend's drawGlyph, so quantization can never
  drift between them. Returns ok=false for non-glyph surfaces.
- **`WindowScale`**: activate the dormant `// ContentScale` idea as `var WindowScale
  float32 = 1`. Backend sets it: `backingScaleFactor` for the live window, `1` for the
  offscreen `--png` path (that context is unscaled).
- **Rasterizer:** `golang.org/x/image/vector` (already a direct dep). Feed it the
  outline segments scaled to device px into a tight bbox (+1px pad); `Draw` into an
  `*image.Alpha`. (`go-text/render` exists but is string/face-oriented and pulls
  `srwiley/rasterx`; `x/image/vector` is the lower-level fit for one-glyph-to-mask.)
- **Bitmap is device-px; dest rect is logical.** The CG context is in points; CG maps
  points→pixels by the backing transform. Draw the device-px bitmap into a logical
  rect of size (W/scale, H/scale) → 1:1 at physical pixels = crisp. The entry stores
  device W/H + the logical offset (bearing) from the pen origin to the bitmap's
  top-left; the backend reuses today's pen origin math (ox, baseY) and adds the
  bearing.
- **Tinting:** the cached bitmap is an **alpha mask** (no color baked in — color must
  NOT be part of the key). Draw it tinted with Color1 via CG clip-to-mask + fill, or
  an image-mask + DrawImage. *Mask polarity (coverage vs. stencil) is a known CG
  footgun — verify against the `--png` and flip if inverted.*
- **Collection happens in a post-production pass** over the `surfaces` slice (not in
  the per-container emit hot path), right where `FrameOutputData` is assembled.

## Tradeoffs (acknowledged, accepted by the user)

- **Uniform software rendering, not native.** Glyphs are rasterized by the Go
  rasterizer for *all* backends incl. cocoa — uniform WYSIWYG across backends, but we
  give up CG's macOS-tuned AA/hinting. The `--png` will look subtly different from
  today's CG path-fill output; that is expected, not a regression.
- **Sub-pixel x-positioning:** initial version snaps to integer device px (no subpixel
  buckets). Add 2–3 horizontal buckets to the key later only if small text looks
  uneven.
- **Cache keyed by size:** continuously animating text size thrashes the cache. Not a
  concern for shirei's discrete sizes.
- **DPI change:** Px is device-px, so moving to a different-scale monitor naturally
  produces new keys; old ones age out via LRU. Backend must update `WindowScale` on
  `backingScaleFactor` change.

## Build order

1. [x] Plan file (this).
2. [x] Core: `WindowScale` (=1 default); memoize `GlyphOutline`. (shirei.go, fonts.go)
3. [x] Core: `GlyphKey`, `GlyphKeyForSurface`, the byte-budgeted LRU bitmap cache,
   the `x/image/vector` rasterizer, the post-production pass, `FrameOutputData`
   delta fields, `GlyphBitmap` getter, `GlyphCacheBudgetBytes` knob (0 = off).
   (glyphcache.go, shirei.go)
4. [x] Cocoa: set `WindowScale` + budget; `map[GlyphKey]CGImage`; apply
   evicted/added each frame; replace path build in `drawGlyph` with tinted mask blit;
   add C helpers (make gray coverage mask, draw-mask-tinted) to cocoa.h/.m.
5. [~] Verify: `example --png` parity DONE (polarity/placement correct; scale=2 sim
   confirms the /scale math). **Live window still to confirm** (Retina crispness +
   idle CPU) — best done by running `go run ./shirei/cocoabackend/example`.

## Implementation notes (as built)

- Tinting: a Quartz **image mask** (`CGImageMaskCreate`) drawn with
  `CGContextDrawImage`, which paints the current fill color through the mask in ONE
  compositing op. Polarity is inverted from coverage (image-mask: 0 = paint, 255 =
  leave), so `cg_make_glyph_mask` stores `255 - coverage`.
  - PERF NOTE: the first cut used `CGContextClipToMask` + fill per glyph, which made
    interaction feel ~10fps. Per-glyph clipping is very expensive in CG; the
    image-mask `DrawImage` is the cheap path and the right way to tint a glyph.
    Never reintroduce per-glyph clip-to-mask.
- The cached CGImage must OWN its bytes (it outlives the make call): the bytes are
  malloc-copied and freed by the data-provider release callback on `CGImageRelease`.
  Do not hand a Go slice pointer to a persisted CGImage (cgo + GC = use-after-free).
- `applyGlyphDeltas` runs after EVERY `RunFrameFn` (shireiProduceFrame AND the
  RenderToPNG loop). The PNG path runs 2 frames; all adds land on frame 1, so
  applying only the last frame's deltas would upload nothing — apply per frame.
- Scale: glyph bitmaps are device-px; `drawGlyph` divides OffX/OffY/W/H by
  `WindowScale` to get logical points. Validated by the scale=2 `--png` sim
  (identical size/placement, just supersampled).

## Verify commands

- Offscreen: `go run ./shirei/cocoabackend/example --png /tmp/out.png`
- Live: `go run ./shirei/cocoabackend/example`
- gio unaffected: it sets no budget → cache pass is skipped; confirm a gio demo still
  builds/renders.
