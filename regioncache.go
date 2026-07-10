package shirei

import (
	"fmt"
	"image"
	"os"

	"github.com/cespare/xxhash/v2"
	g "go.hasen.dev/generic"
)

// Container raster cache (see notes/container-cache-plan.md). A "region" is a
// contiguous ClipPush..ClipPop span in the flat surface list: a clipped container
// plus everything it encloses. Clipping guarantees the span cannot paint outside
// its rect, so it is a natural unit to rasterize once and reuse when its content
// is unchanged frame-to-frame.
//
// The cache is on by default (a renderer opts out with noRegionCache). It is
// correctness-neutral by construction: rendering a region into its own buffer and
// src-over-blitting it back is pixel-identical to rendering it inline (the invariant
// the golden test guards), so the hit/miss/inline policy only ever affects speed,
// never output. collectRegions also produces the stability stats the SHIREI_PERF
// printer reports.

// regionVerify re-rasterizes every cache hit and compares it to the stored bitmap,
// screaming on any real difference. This is the plan's debug-verify: it catches a
// bitmap gone stale because content the hash only sees by id changed underneath it
// (an image finishing its async decode, a glyph re-rastered) — the "dangerous
// middle". It defeats the cache's speedup (it does the work we cache to avoid), so
// it is a correctness switch to run once, not a steady-state mode.
var regionVerify = os.Getenv("SHIREI_REGION_VERIFY") != ""

// RegionStats is a snapshot of the measurement counters, accumulated across frames
// since the last fetch. A backend's perf printer reads and resets it once a second.
type RegionStats struct {
	Frames        int   // painted frames measured
	Regions       int64 // total clip regions seen
	StableRegions int64 // regions whose content hash matched the previous frame
	Surfaces      int64 // total surfaces across the measured frames
	Covered       int64 // surfaces lying under a stable region (would blit, not re-raster)
	MaxDepth      int   // deepest clip nesting observed
	Hits          int64 // cache blits from a stored bitmap
	Populated     int64 // regions rasterized into the cache this frame
	Inlined       int64 // regions rendered inline (first sight / ineligible)
}

// regionInfo: for a ClipPush at index `start`, its matching ClipPop `end` and the
// content hash of the whole span (own surfaces + folded child hashes).
type regionInfo struct {
	end  int
	hash uint64
}

// regionRaster is a rasterized region: premultiplied BGRA in region-local device
// coords (top-left = origin), ready to src-over-blit back at origin.
type regionRaster struct {
	pix    []byte
	w, h   int
	stride int
	origin image.Point // device origin the region was rendered at (dr.Min)
}

type regionEntry struct {
	raster    regionRaster
	lastFrame uint64
}

// keepFrames: drop a cache entry not consulted for this many frames. Small on
// purpose (a fading control or a one-frame hover blip should not accumulate
// garbage); the plan settled on 2 as the max tolerable.
const keepFrames = 2

type regionCache struct {
	// per-frame region map, rebuilt each frame by collectRegions (start -> info).
	byStart map[int]regionInfo

	// bottom-up hashing scratch, one slot per open clip depth (LIFO), reused.
	digests []*xxhash.Digest
	starts  []int
	direct  []int64

	// cross-frame cache state.
	frame      uint64
	prevHashes map[uint64]int // hashes seen last frame (cache-on-second-hit)
	curHashes  map[uint64]int // hashes seen this frame
	entries    map[uint64]*regionEntry

	stats RegionStats
}

// surfaceImageGeneration returns the current pixel generation of the image an image
// surface references, or 0 for a non-image surface. Both the region-cache hash
// (hashSurface) and the whole-frame change detector (computeSurfacesHash) fold it
// in, so pixels changing behind a stable ImageId (async decode, UseImage) are
// detected by both: the whole-frame check must see it or the cocoa backend skips
// the frame as static before the region cache ever runs (see images.go).
func surfaceImageGeneration(s *Surface) uint64 {
	if s.ImageId == 0 {
		return 0
	}
	if d := LookupImage(s.ImageId); d != nil {
		return d.Generation
	}
	return 0
}

// hashSurface folds one surface's pixel-affecting content into h. Mostly the raw
// bytes of the flat, pointer-free Surface (like computeSurfacesHash). This is the
// single place the "what the hash must cover" contract lives; the exception is the
// image generation (content referenced only by id — see surfaceImageGeneration).
func hashSurface(h *xxhash.Digest, s *Surface) {
	h.Write(g.UnsafeRawBytes(s))
	if gen := surfaceImageGeneration(s); gen != 0 {
		var buf [8]byte
		putUint64(&buf, gen)
		h.Write(buf[:])
	}
}

// collectRegions walks the flat surface list once, maintaining a stack of open
// regions. Each surface folds into the innermost open region's digest; on ClipPop
// a region's finished hash folds into its parent (bottom-up: O(N), and an inner
// change propagates up so an outer region is stable only if its whole subtree is).
// Fills byStart{end,hash} for every region and updates the seen-hash sets and
// measurement stats.
func (rc *regionCache) collectRegions(surfaces []Surface) {
	if rc.byStart == nil {
		rc.byStart = make(map[int]regionInfo)
		rc.prevHashes = make(map[uint64]int)
		rc.curHashes = make(map[uint64]int)
	}
	clear(rc.byStart)
	// This frame's hashes become next frame's "previous".
	rc.prevHashes, rc.curHashes = rc.curHashes, rc.prevHashes
	clear(rc.curHashes)

	rc.stats.Frames++
	rc.stats.Surfaces += int64(len(surfaces))

	depth := 0
	for i := range surfaces {
		s := &surfaces[i]

		if s.Clip == ClipPop {
			if depth == 0 {
				continue // unbalanced; renderSurfaces will panic, don't mask it here
			}
			d := depth - 1
			hashSurface(rc.digests[d], s) // the pop/border belongs to this region
			rc.direct[d]++
			h := rc.digests[d].Sum64()
			start := rc.starts[d]
			rc.byStart[start] = regionInfo{end: i, hash: h}

			rc.stats.Regions++
			if rc.prevHashes[h] > 0 {
				rc.stats.StableRegions++
				rc.stats.Covered += rc.direct[d]
			}
			rc.curHashes[h]++
			if d > 0 { // fold into parent so a child change propagates up
				var buf [8]byte
				putUint64(&buf, h)
				rc.digests[d-1].Write(buf[:])
			}
			depth--
			continue
		}

		if s.Clip == ClipPush {
			d := depth
			for len(rc.digests) <= d {
				rc.digests = append(rc.digests, xxhash.New())
				rc.starts = append(rc.starts, 0)
				rc.direct = append(rc.direct, 0)
			}
			rc.digests[d].Reset()
			rc.starts[d] = i
			rc.direct[d] = 1 // the push surface is this region's own background
			hashSurface(rc.digests[d], s)
			depth++
			if depth > rc.stats.MaxDepth {
				rc.stats.MaxDepth = depth
			}
			continue
		}

		if depth > 0 {
			d := depth - 1
			hashSurface(rc.digests[d], s)
			rc.direct[d]++
		}
	}
}

// fetchStats returns the accumulated measurement and resets the counters. The
// prev/cur hash state and cache entries are NOT reset — they must survive readout.
func (rc *regionCache) fetchStats() RegionStats {
	s := rc.stats
	rc.stats = RegionStats{}
	return s
}

// evict drops entries not consulted within keepFrames.
func (rc *regionCache) evict() {
	for h, e := range rc.entries {
		if rc.frame-e.lastFrame > keepFrames {
			delete(rc.entries, h)
		}
	}
}

// -----------------------------------------------------------------------------
//  Cached render path
// -----------------------------------------------------------------------------

// renderCached assembles the frame using the region cache: unchanged regions blit
// from a stored bitmap, changed/new ones render inline (and, on their second
// consecutive sighting, get rasterized into the cache). Assumes renderSurfaces has
// already cleared the buffer and reset the clip/alpha stacks. Output is identical
// to the plain inline path — the cache only decides *how* each region is produced.
func (r *SoftRenderer) renderCached(surfaces []Surface) {
	rc := &r.regions
	if rc.entries == nil {
		rc.entries = make(map[uint64]*regionEntry)
	}
	rc.frame++
	rc.collectRegions(surfaces)

	i := 0
	for i < len(surfaces) {
		s := &surfaces[i]
		if s.Clip == ClipPush {
			if info, ok := rc.byStart[i]; ok {
				if r.tryRegion(surfaces, i, info) {
					i = info.end + 1 // region handled by blit: skip its interior
					continue
				}
				rc.stats.Inlined++ // region rendered in place (first sight / ineligible)
			}
		}
		r.renderOne(s)
		i++
	}
	rc.evict()
}

// tryRegion handles a region by blit (a cache hit, or a populate-then-blit on the
// second sighting) and returns true; returns false to signal "render this region
// inline" (first sighting, or ineligible). On a blit it also draws the region's
// border (the ClipPop surface) inline, under the parent clip — the border sits on
// the rounded boundary and spills the rect, so it is never part of the cached
// interior (see plan: cache the interior, draw the border inline).
func (r *SoftRenderer) tryRegion(surfaces []Surface, start int, info regionInfo) bool {
	// Never cache inside a fading ancestor: the ancestor's group alpha is applied
	// per-surface by the inline renderer and is NOT in this region's hash, so a
	// baked bitmap would go stale as the ancestor fades. r.alpha is exactly 1
	// outside any transparency group.
	if r.alpha < 1 {
		return false
	}

	rc := &r.regions
	entry := rc.entries[info.hash]
	if entry == nil && rc.prevHashes[info.hash] == 0 {
		return false // first sight this hash: render inline, cache next time
	}

	if entry == nil { // second sight: rasterize into the cache now
		entry = &regionEntry{}
		rc.entries[info.hash] = entry
		r.renderRegionInto(&entry.raster, surfaces, start, info.end)
		rc.stats.Populated++
	} else {
		rc.stats.Hits++
		if regionVerify {
			r.verifyRegion(surfaces, start, info.end, entry)
		}
	}
	entry.lastFrame = rc.frame

	// Blit the interior (ancestor alpha is 1 by the guard above). The region's own
	// Transparency is already baked into the buffer.
	r.blitRegion(&entry.raster)

	// The border is part of the container's transparency group in the inline
	// renderer (drawn before the group's alpha is restored), so draw it at the
	// region's own group alpha.
	push := &surfaces[start]
	pop := &surfaces[info.end]
	saved := r.alpha
	if push.Transparency > 0 {
		r.alpha *= 1 - push.Transparency
	}
	if r.visible(pop) {
		r.drawContent(pop)
	}
	r.alpha = saved
	return true
}

// verifyRegion re-rasterizes a region and compares it to the stored bitmap,
// reporting a stale entry. A difference beyond premultiplied rounding (±1) means
// the hash missed an input that changed the pixels (see regionVerify).
func (r *SoftRenderer) verifyRegion(surfaces []Surface, start, end int, entry *regionEntry) {
	r.renderRegionInto(&r.regionVerifyRaster, surfaces, start, end)
	a, b := entry.raster.pix, r.regionVerifyRaster.pix
	if len(a) != len(b) {
		fmt.Fprintf(os.Stderr, "[region-verify] STALE size start=%d: cached %d bytes, now %d\n",
			start, len(a), len(b))
		return
	}
	maxAbs := 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		if d > maxAbs {
			maxAbs = d
		}
	}
	if maxAbs > 1 {
		fmt.Fprintf(os.Stderr, "[region-verify] STALE bitmap start=%d maxAbs=%d — hash missed a pixel input\n",
			start, maxAbs)
	}
}

// renderRegionInto rasterizes a region's interior ([start .. end), i.e. the push
// background plus all enclosed surfaces, excluding the ClipPop border) into rr's
// own premultiplied-BGRA buffer, in region-local device coords over transparent.
// It replays the surfaces through the same renderOne machinery as the main loop,
// so the result is identical — only the buffer, clip origin, and starting alpha
// differ. The caller guarantees the ancestor alpha is 1, so the buffer bakes
// exactly the region's own (in-hash) group alpha and blits at 1.
func (r *SoftRenderer) renderRegionInto(rr *regionRaster, surfaces []Surface, start, end int) {
	push := &surfaces[start]
	dr := r.devRect(push.Rect) // absolute device rect (devOrigin is 0 in the main render)
	w, h := dr.Dx(), dr.Dy()
	rr.origin = dr.Min
	rr.w, rr.h, rr.stride = w, h, w*4
	if w <= 0 || h <= 0 {
		rr.pix = rr.pix[:0]
		return
	}
	need := rr.stride * h
	if cap(rr.pix) < need {
		rr.pix = make([]byte, need)
	} else {
		rr.pix = rr.pix[:need]
		clear(rr.pix) // start transparent
	}

	// Swap in an isolated render context: a local framebuffer, an integer device
	// origin, and separate clip/alpha stacks + clip-mask arena, so rendering the
	// region cannot disturb the in-progress main render's live clip masks.
	saveFb, saveClip, saveStack := r.fb, r.clip, r.clipStack
	saveAlpha, saveAlphaPrev := r.alpha, r.alphaPrev
	saveOrigin, saveArena := r.devOrigin, r.clipMaskArena

	r.fb = Framebuffer{W: w, H: h, Stride: rr.stride, Pix: rr.pix}
	r.devOrigin = dr.Min
	r.clip = clipState{rect: image.Rect(0, 0, w, h)}
	r.clipStack = r.regionClipStack[:0]
	r.alpha = 1
	r.alphaPrev = r.regionAlphaPrev[:0]
	r.clipMaskArena = r.regionMaskArena

	// Push surface: apply its own Transparency, draw its background, establish its
	// (possibly rounded) clip. Its matching PopTransparency lives on the excluded
	// border, so we simply leave the group open — the stacks are discarded below.
	if push.Transparency > 0 {
		r.alphaPrev = append(r.alphaPrev, r.alpha)
		r.alpha *= 1 - push.Transparency
	}
	if r.visible(push) {
		r.drawContent(push)
	}
	if push.Clip == ClipPush {
		r.pushClip(push)
	}
	for i := start + 1; i < end; i++ {
		r.renderOne(&surfaces[i]) // inner surfaces inline; no nested caching
	}

	// Stash grown scratch back, restore the main render context.
	r.regionClipStack = r.clipStack
	r.regionAlphaPrev = r.alphaPrev
	r.regionMaskArena = r.clipMaskArena
	r.fb, r.clip, r.clipStack = saveFb, saveClip, saveStack
	r.alpha, r.alphaPrev = saveAlpha, saveAlphaPrev
	r.devOrigin, r.clipMaskArena = saveOrigin, saveArena
}

// blitRegion src-over composites a cached region (premultiplied BGRA) onto the
// framebuffer at its stored origin, honoring the current clip and group alpha —
// the same compositing every other primitive uses, so the transparent corners of
// a rounded region let the current backdrop show through correctly.
func (r *SoftRenderer) blitRegion(rr *regionRaster) {
	if len(rr.pix) == 0 {
		return
	}
	dest := image.Rect(rr.origin.X, rr.origin.Y, rr.origin.X+rr.w, rr.origin.Y+rr.h)
	area := dest.Intersect(r.clip.rect)
	if area.Empty() {
		return
	}
	cr := r.clip.rect
	cmask := r.clipMaskFor(area)
	cstride := cr.Dx()
	ga := r.galpha()

	// Fast path: no clip coverage mask and full group alpha — the usual case for a
	// large opaque region (a card, or the whole list viewport). The source is
	// premultiplied BGRA, so an opaque run (A==255) copies verbatim and a fully
	// transparent run (A==0, e.g. rounded corners) is a no-op; only the antialiased
	// edges in between need a per-pixel src-over. This turns a mostly-opaque region
	// blit into runs of memcpy, ~the difference between the ~8ms and ~1-2ms paint.
	if cmask == nil && ga == 255 {
		w4 := (area.Max.X - area.Min.X) * 4
		for y := area.Min.Y; y < area.Max.Y; y++ {
			di := y*r.fb.Stride + area.Min.X*4
			sp := (y-dest.Min.Y)*rr.stride + (area.Min.X-dest.Min.X)*4
			end := sp + w4
			for sp < end {
				a := rr.pix[sp+3]
				if a == 255 { // opaque run -> one memcpy
					n := 4
					for sp+n < end && rr.pix[sp+n+3] == 255 {
						n += 4
					}
					copy(r.fb.Pix[di:di+n], rr.pix[sp:sp+n])
					di += n
					sp += n
				} else if a == 0 { // transparent run -> skip
					n := 4
					for sp+n < end && rr.pix[sp+n+3] == 0 {
						n += 4
					}
					di += n
					sp += n
				} else { // antialiased edge -> blend one pixel
					blendBGRA(r.fb.Pix[di:di+4:di+4],
						uint32(rr.pix[sp+2]), uint32(rr.pix[sp+1]), uint32(rr.pix[sp]), uint32(a))
					di += 4
					sp += 4
				}
			}
		}
		return
	}

	for y := area.Min.Y; y < area.Max.Y; y++ {
		i := y*r.fb.Stride + area.Min.X*4
		sp := (y-dest.Min.Y)*rr.stride + (area.Min.X-dest.Min.X)*4
		ci := 0
		if cmask != nil {
			ci = (y-cr.Min.Y)*cstride + (area.Min.X - cr.Min.X)
		}
		for x := area.Min.X; x < area.Max.X; x++ {
			sbb := uint32(rr.pix[sp])  // premultiplied B
			sg := uint32(rr.pix[sp+1]) // premultiplied G
			sr := uint32(rr.pix[sp+2]) // premultiplied R
			sa := uint32(rr.pix[sp+3]) // A
			f := ga
			if cmask != nil {
				f = f * uint32(cmask[ci]) / 255
				ci++
			}
			if f != 255 {
				sr = sr * f / 255
				sg = sg * f / 255
				sbb = sbb * f / 255
				sa = sa * f / 255
			}
			if sa > 0 {
				blendBGRA(r.fb.Pix[i:i+4:i+4], sr, sg, sbb, sa)
			}
			sp += 4
			i += 4
		}
	}
}

func putUint64(b *[8]byte, v uint64) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
	b[4] = byte(v >> 32)
	b[5] = byte(v >> 40)
	b[6] = byte(v >> 48)
	b[7] = byte(v >> 56)
}
