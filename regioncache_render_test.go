package shirei

import (
	"fmt"
	"image"
	"testing"
)

// The load-bearing invariant of the container raster cache: rendering a region
// into its own buffer and src-over-blitting it back is pixel-identical to
// rendering it inline. We prove it by rendering the same scene through the
// plain inline renderer and through the cache, frame by frame, and asserting
// the framebuffers match byte-for-byte across the cache's whole lifecycle:
// first sight (inline), second sight (populate), and hit (blit).

func rrect(x, y, w, h float32) Rect { return Rect{Origin: Vec2{x, y}, Size: Vec2{w, h}} }
func hsla(h, s, l, a float32) Vec4  { return Vec4{h, s, l, a} }

// cardScene is a rounded, clipped card over an opaque background, with overlapping
// interior fills, a vertical gradient, a nested rounded-clip container that its own
// child overflows (so clipping into corners is exercised), and borders on both the
// card and the nested box. transp opens the card as a transparency group; innerColor
// lets a caller mutate one interior surface between frames to force cache misses.
func cardScene(innerColor Vec4, transp float32) []Surface {
	return []Surface{
		// opaque full-viewport background
		{Rect: rrect(0, 0, 240, 160), Color1: hsla(210, 40, 30, 1), Color2: hsla(210, 40, 30, 1)},

		// the card: rounded, clipped, optionally a transparency group
		{Rect: rrect(30, 20, 150, 110), Color1: hsla(0, 0, 92, 1), Color2: hsla(0, 0, 92, 1),
			Corners: N4(16), Clip: ClipPush, Transparency: transp},

		// overlapping interior fills + a gradient
		{Rect: rrect(40, 30, 120, 40), Color1: innerColor, Color2: innerColor},
		{Rect: rrect(45, 85, 120, 35), Color1: hsla(200, 70, 55, 1), Color2: hsla(30, 80, 55, 1)},

		// nested rounded-clip box whose child overflows it
		{Rect: rrect(55, 55, 90, 30), Color1: hsla(50, 80, 80, 1), Color2: hsla(50, 80, 80, 1),
			Corners: N4(10), Clip: ClipPush},
		{Rect: rrect(60, 60, 140, 18), Color1: hsla(0, 0, 10, 1), Color2: hsla(0, 0, 10, 1)}, // overflows -> clipped
		{Rect: rrect(55, 55, 90, 30), Color1: hsla(0, 0, 40, 1), Color2: hsla(0, 0, 40, 1),
			Corners: N4(10), Stroke: 1, Clip: ClipPop}, // nested border

		// card border, closes the transparency group (iff one was opened)
		{Rect: rrect(30, 20, 150, 110), Color1: hsla(0, 0, 20, 1), Color2: hsla(0, 0, 20, 1),
			Corners: N4(16), Stroke: 2, Clip: ClipPop, PopTransparency: transp > 0},
	}
}

// diffPix returns the number of differing bytes, the first differing offset, and
// the maximum absolute per-byte difference (to tell rounding noise from logic bugs).
func diffPix(a, b []byte) (n, first, maxAbs int) {
	first = -1
	if len(a) != len(b) {
		return len(a) + len(b), 0, 255
	}
	for i := range a {
		if a[i] != b[i] {
			n++
			if first < 0 {
				first = i
			}
			d := int(a[i]) - int(b[i])
			if d < 0 {
				d = -d
			}
			if d > maxAbs {
				maxAbs = d
			}
		}
	}
	return n, first, maxAbs
}

// assertCacheMatchesInline renders each frame's ui.surfaces both ways and requires the
// framebuffers to match within tol per byte. inline uses a fresh renderer per frame;
// cached reuses one renderer so its cross-frame state (populate/evict) is exercised.
//
// tol == 0 is the strict guarantee (opaque content is bit-exact). tol == 2 covers
// translucent-group content: compositing a region's content into its own buffer
// then over the backdrop differs from per-surface compositing by 8-bit
// premultiplied rounding. Inside borders sit fully over the group's content, so
// border AA after the blit can accumulate one extra step of error (±2 total);
// still visually undetectable. Anything larger is a real bug.
func assertCacheMatchesInline(t *testing.T, frames [][]Surface, devW, devH int, scale float32, tol int) {
	t.Helper()
	var inline, cached SoftRenderer
	inline.noRegionCache = true // reference path: original inline rasterizer
	for f, ss := range frames {
		a := inline.Render(ss, devW, devH, scale)
		aPix := append([]byte(nil), a.Pix...)
		b := cached.Render(ss, devW, devH, scale)
		if n, first, maxAbs := diffPix(aPix, b.Pix); maxAbs > tol {
			t.Fatalf("frame %d: cached differs from inline beyond tol=%d in %d/%d bytes (maxAbs=%d), first at %d (px %d, x=%d y=%d)",
				f, tol, n, len(aPix), maxAbs, first, first/4, (first/4)%devW, (first/4)/devW)
		}
	}
}

func TestRegionCacheMatchesInlineOpaque(t *testing.T) {
	// Opaque card (no transparency group): the cache must be BIT-EXACT — this is the
	// haystack case (opaque row backgrounds). tol 0.
	scene := cardScene(hsla(0, 70, 50, 1), 0)
	frames := [][]Surface{scene, scene, scene, scene, scene}
	assertCacheMatchesInline(t, frames, 480, 320, 2, 0)
}

func TestRegionCacheMatchesInlineStatic(t *testing.T) {
	// Semi-transparent card, several frames: frame 0 renders inline (first sight),
	// frame 1 populates, frames 2+ are pure hits — all within ±1 (premul rounding).
	scene := cardScene(hsla(0, 70, 50, 1), 0.25)
	frames := [][]Surface{scene, scene, scene, scene, scene}
	assertCacheMatchesInline(t, frames, 480, 320, 2, 2) // retina-ish: scale 2
}

func TestRegionCacheMatchesInlineChanging(t *testing.T) {
	// One interior surface changes every frame, so the card region never stabilizes
	// and always takes the inline/miss path — must match inline EXACTLY (no buffer).
	var frames [][]Surface
	for i := 0; i < 4; i++ {
		frames = append(frames, cardScene(hsla(float32(i*40), 70, 50, 1), 0.25))
	}
	assertCacheMatchesInline(t, frames, 480, 320, 2, 0)
}

func TestRegionCacheMatchesInlineStabilizeThenChange(t *testing.T) {
	// Stable long enough to cache (populate + hit), then an interior change (miss →
	// re-inline), then stable again (populate + hit). Every frame within translucent tol.
	a := cardScene(hsla(0, 70, 50, 1), 0.25)
	b := cardScene(hsla(120, 70, 50, 1), 0.25)
	frames := [][]Surface{a, a, a, b, b, b, a}
	assertCacheMatchesInline(t, frames, 480, 320, 2, 2)
}

func TestRegionCacheMatchesInlineScale1(t *testing.T) {
	// Scale 1 exercises a different device-rounding path. Opaque -> bit-exact.
	scene := cardScene(hsla(280, 60, 45, 1), 0)
	frames := [][]Surface{scene, scene, scene}
	assertCacheMatchesInline(t, frames, 240, 160, 1, 0)
}

func TestRegionCachePartialVisibility(t *testing.T) {
	// Viewport smaller than the card, so the region straddles the right and bottom
	// edges (the half-scrolled-row case). The cache rasterizes the full region into
	// its buffer and blit-clips to the viewport; the inline path clips inner
	// ui.surfaces to the viewport directly. The visible result must be identical.
	scene := cardScene(hsla(0, 70, 50, 1), 0) // opaque -> bit-exact
	frames := [][]Surface{scene, scene, scene, scene}
	assertCacheMatchesInline(t, frames, 140, 100, 1, 0)
}

// TestRegionHashTracksImageGeneration guards the "dangerous middle": an image whose
// pixels change behind a stable ImageId (async decode / UseImage replacement) leaves
// the surface bytes identical, so without the generation fold the region hash would
// be unchanged and a cached bitmap would show the stale pixels forever.
func TestRegionHashTracksImageGeneration(t *testing.T) {
	fill := func(v byte) *image.RGBA {
		im := image.NewRGBA(image.Rect(0, 0, 4, 4))
		for i := range im.Pix {
			im.Pix[i] = v
		}
		return im
	}
	id := UseImage("test-region-gen", fill(0x11))
	scene := []Surface{
		{Clip: ClipPush, Rect: rrect(0, 0, 50, 50)},
		{ImageId: id, Rect: rrect(5, 5, 20, 20)},
		{Clip: ClipPop, Rect: rrect(0, 0, 50, 50)},
	}

	var rc regionCache
	rc.collectRegions(scene)
	h1 := rc.byStart[0].hash

	// Replace the pixels behind the SAME id: surface bytes are unchanged, only the
	// generation moves.
	id2 := UseImage("test-region-gen", fill(0x22))
	if id2 != id {
		t.Fatalf("UseImage should reuse the id for the same key: got %d want %d", id2, id)
	}
	rc.collectRegions(scene)
	h2 := rc.byStart[0].hash

	if h1 == h2 {
		t.Fatalf("region hash unchanged after image pixels changed behind a stable id (h=%016x) — cache would go stale", h1)
	}
}

// The whole-frame change detector must ALSO track the image generation: the cocoa
// backend skips presenting a frame whose hash matches what's on screen, so if the
// hash misses the change the decoded image never gets drawn — regardless of the
// region cache. This is the bug that made demo13's images wait for a scroll.
func TestWholeFrameHashTracksImageGeneration(t *testing.T) {
	fill := func(v byte) *image.RGBA {
		im := image.NewRGBA(image.Rect(0, 0, 4, 4))
		for i := range im.Pix {
			im.Pix[i] = v
		}
		return im
	}
	id := UseImage("test-wholeframe-gen", fill(0x11))
	ss := []Surface{{ImageId: id, Rect: rrect(0, 0, 10, 10)}}

	h1 := computeSurfacesHash(ss)
	UseImage("test-wholeframe-gen", fill(0x22)) // same id, new pixels -> generation bumps
	h2 := computeSurfacesHash(ss)

	if h1 == h2 {
		t.Fatalf("whole-frame hash unchanged after image pixels changed behind a stable id (h=%016x) — frame would be skipped as static", h1)
	}
}

// A quick belt-and-suspenders check that the cache actually engages (otherwise the
// tests above would pass trivially by always taking the inline path).
func TestRegionCacheActuallyCaches(t *testing.T) {
	var cached SoftRenderer // cache is on by default
	scene := cardScene(hsla(0, 70, 50, 1), 0)
	for i := 0; i < 3; i++ {
		cached.Render(scene, 480, 320, 2)
	}
	st := cached.regions.stats
	if st.Populated == 0 || st.Hits == 0 {
		t.Fatalf("expected the cache to populate and hit; got %s", fmt.Sprintf("%+v", st))
	}
}
