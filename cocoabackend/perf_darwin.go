//go:build darwin

package cocoabackend

// Optional frame instrumentation: set SHIREI_PERF=1 to print achieved fps and the
// average produce (RunFrameFn) and paint (offscreen raster + blit) times once a
// second. Zero overhead when unset. Kept because this backend cares about frame
// cost; it's how the 10fps->60fps scroll regression was diagnosed (see PLAN.md).

import (
	"fmt"
	"os"
	"time"

	"go.hasen.dev/shirei"
)

var perfEnabled = os.Getenv("SHIREI_PERF") != ""

var (
	perfFrames    int
	perfProduceNs int64
	perfPaintNs   int64
	perfStart     time.Time
)

// Present-path instrumentation (also gated by SHIREI_PERF). This exists to
// diagnose flashing: the display link is not the only thing that presents —
// AppKit's drawRect: presents too (window expose/occlusion/resize), out of band
// with the display link. Under load the frame cadence and the compositor drift
// out of phase and every off-screen surface can be momentarily in use, so
// pickFreeSurface finds nothing safe to draw into. Rather than tear (write a
// surface the compositor is scanning out) we now defer that present to the next
// tick. Watch `expose` (out-of-band presents) and `defer` (presents postponed to
// avoid a tear); both should be ~0 in steady state.
const (
	presentTick   = 0 // from the CADisplayLink tick (renderFrame)
	presentExpose = 1 // from AppKit drawRect: (expose/occlusion/resize)

	pickClean = 0 // a surface that is neither on-screen nor in use — safe
	pickDefer = 1 // every off-screen surface was still in use — present deferred
)

var (
	perfPresentTick   int
	perfPresentExpose int
	perfPresentSkip   int
	perfPickClean     int
	perfPickDefer     int
)

// Paint-workload breakdown: what the 18ms paint is actually rasterizing. The
// software renderer blends each surface through a coverage mask; for a text
// screen the glyphs dominate — each is its own surface, blended per pixel. These
// count the produced draw list per painted frame so we can see whether paint time
// tracks glyph count / blended glyph pixels (glyph-bound → SIMD/row-cache helps)
// or something else (huge fills / overdraw). Measured from the backend's copy of
// the surface list, so the core renderer stays untouched.
var (
	perfSurfFrames int
	perfSurfaces   int64
	perfGlyphs     int64
	perfFills      int64
	perfBorders    int64
	perfImages     int64
	perfGlyphPx    int64 // sum of blended glyph bitmap areas (device px)
)

// Paint split: is the ~16ms paint CPU rasterization, or the thread blocking on
// the compositor? lock = time in iosurface_lock (waits if the surface is still
// held), render = the actual RenderInto rasterization, unlock = handing the
// CPU-written surface back (memory coherency, cost scales with bytes written),
// setlayer = handing the surface to CoreAnimation. If render dominates it's real
// pixel work; if lock dominates the "paint" cost is a compositor stall, not CPU.
var (
	perfLockNs   int64
	perfRenderNs int64
	perfUnlockNs int64
	perfSetNs    int64
)

func perfRecordProduce(d time.Duration) {
	if perfEnabled {
		perfProduceNs += int64(d)
	}
}

func perfRecordPresentSource(source int) {
	if !perfEnabled {
		return
	}
	if source == presentExpose {
		perfPresentExpose++
	} else {
		perfPresentTick++
	}
}

func perfRecordPresentSkip() {
	if perfEnabled {
		perfPresentSkip++
	}
}

func perfRecordPick(outcome int) {
	if !perfEnabled {
		return
	}
	switch outcome {
	case pickClean:
		perfPickClean++
	case pickDefer:
		perfPickDefer++
	}
}

// perfRecordSurfaces classifies one painted frame's draw list. Called once per
// actual paint (not on skip/defer), so its counters average over painted frames.
func perfRecordSurfaces(surfaces []shirei.Surface) {
	if !perfEnabled {
		return
	}
	perfSurfFrames++
	for i := range surfaces {
		s := &surfaces[i]
		switch {
		case s.FontId > 0 && s.GlyphId > 0:
			perfGlyphs++
			if key, ok := shirei.GlyphKeyForSurface(s); ok {
				if bm, ok := shirei.GlyphBitmap(key); ok {
					perfGlyphPx += int64(bm.W) * int64(bm.H)
				}
			}
		case s.ImageId > 0:
			perfImages++
		case s.Stroke > 0:
			perfBorders++
		default:
			perfFills++
		}
		perfSurfaces++
	}
}

func perfRecordLock(d time.Duration) {
	if perfEnabled {
		perfLockNs += int64(d)
	}
}

func perfRecordRender(d time.Duration) {
	if perfEnabled {
		perfRenderNs += int64(d)
	}
}

func perfRecordUnlock(d time.Duration) {
	if perfEnabled {
		perfUnlockNs += int64(d)
	}
}

func perfRecordSetLayer(d time.Duration) {
	if perfEnabled {
		perfSetNs += int64(d)
	}
}

func perfRecordPaint(d time.Duration) {
	if !perfEnabled {
		return
	}
	perfFrames++
	perfPaintNs += int64(d)

	now := time.Now()
	if perfStart.IsZero() {
		perfStart = now
		return
	}
	if now.Sub(perfStart) < time.Second {
		return
	}

	f := float64(perfFrames)
	fmt.Fprintf(os.Stderr, "[perf] %d fps | produce %.1fms paint %.1fms | present tick=%d expose=%d skip=%d | pick clean=%d defer=%d\n",
		perfFrames, float64(perfProduceNs)/f/1e6, float64(perfPaintNs)/f/1e6,
		perfPresentTick, perfPresentExpose, perfPresentSkip,
		perfPickClean, perfPickDefer)
	if perfSurfFrames > 0 {
		sf := float64(perfSurfFrames)
		fmt.Fprintf(os.Stderr, "       surfaces %.0f/f | glyphs %.0f/f (%.2f Mpx) | fills %.0f borders %.0f images %.0f\n",
			float64(perfSurfaces)/sf, float64(perfGlyphs)/sf, float64(perfGlyphPx)/sf/1e6,
			float64(perfFills)/sf, float64(perfBorders)/sf, float64(perfImages)/sf)
		fmt.Fprintf(os.Stderr, "       paint split: lock %.1fms render %.1fms unlock %.1fms setlayer %.1fms\n",
			float64(perfLockNs)/sf/1e6, float64(perfRenderNs)/sf/1e6, float64(perfUnlockNs)/sf/1e6, float64(perfSetNs)/sf/1e6)
	}
	if rs := softRenderer.RegionStats(); rs.Frames > 0 {
		rf := float64(rs.Frames)
		var coveredPct float64
		if rs.Surfaces > 0 {
			coveredPct = 100 * float64(rs.Covered) / float64(rs.Surfaces)
		}
		fmt.Fprintf(os.Stderr, "       regions %.0f/f | stable %.0f/f | covered %.0f/%.0f surf (%.0f%%) | depth %d\n",
			float64(rs.Regions)/rf, float64(rs.StableRegions)/rf,
			float64(rs.Covered)/rf, float64(rs.Surfaces)/rf, coveredPct, rs.MaxDepth)
		if rs.Hits+rs.Populated+rs.Inlined > 0 { // raster cache engaged
			entries, bytes := softRenderer.RegionCacheBytes()
			fmt.Fprintf(os.Stderr, "       cache: hit %.0f/f populate %.0f/f inline %.0f/f | %d entries %.1f MB\n",
				float64(rs.Hits)/rf, float64(rs.Populated)/rf, float64(rs.Inlined)/rf,
				entries, float64(bytes)/1e6)
		}
	}
	perfFrames = 0
	perfProduceNs = 0
	perfPaintNs = 0
	perfPresentTick = 0
	perfPresentExpose = 0
	perfPresentSkip = 0
	perfPickClean = 0
	perfPickDefer = 0
	perfSurfFrames = 0
	perfSurfaces = 0
	perfGlyphs = 0
	perfFills = 0
	perfBorders = 0
	perfImages = 0
	perfGlyphPx = 0
	perfLockNs = 0
	perfRenderNs = 0
	perfUnlockNs = 0
	perfSetNs = 0
	perfStart = now
}
