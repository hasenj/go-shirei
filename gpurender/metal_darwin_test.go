//go:build darwin && !ios

package gpurender

import (
	"bytes"
	"fmt"
	"image"
	"runtime"
	"testing"
	"unsafe"

	"go.hasen.dev/shirei"
)

func TestMetalAndSoftwareTransparentCanvas(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("Metal init: %v", err)
	}
	host := shirei.GetHost()
	old := host.TransparentBackground
	defer func() { host.TransparentBackground = old }()

	red, green, blue := shirei.Vec4{0, 100, 50, .5}, shirei.Vec4{120, 100, 50, .5}, shirei.Vec4{240, 100, 50, 1}
	clip := shirei.Rect{Origin: shirei.Vec2{4, 4}, Size: shirei.Vec2{24, 24}}
	scene := []shirei.Surface{
		{Rect: clip, Corners: shirei.N4(8), Clip: shirei.ClipPush},
		{Rect: shirei.Rect{Size: shirei.Vec2{32, 20}}, Color1: red, Color2: red},
		{Rect: shirei.Rect{Origin: shirei.Vec2{12, 0}, Size: shirei.Vec2{20, 32}}, Color1: green, Color2: green},
		{Rect: clip, Clip: shirei.ClipPop},
		{Rect: shirei.Rect{Origin: shirei.Vec2{34, 8}, Size: shirei.Vec2{6, 10}}, Color1: blue, Color2: blue},
	}
	for _, transparent := range []bool{true, false} {
		host.TransparentBackground = transparent
		for _, scale := range []int{1, 2} {
			// Each native render starts on a fresh goroutine stack.
			t.Run(fmt.Sprintf("transparent=%t/scale=%d", transparent, scale), func(t *testing.T) {
				w, h := 48*scale, 40*scale
				target := testSurface(w, h)
				if target == nil {
					t.Fatal("test IOSurface is nil")
				}
				defer testRelease(target)
				defer Forget(target)
				var software shirei.SoftRenderer
				for frame := range 5 {
					surfaces := scene
					if frame == 4 {
						surfaces = nil
					}
					if err := Render(target, w, h, float32(scale), surfaces, nil, nil, nil, true); err != nil {
						t.Fatal(err)
					}
					fb := software.Render(surfaces, nil, w, h, float32(scale))
					for _, sample := range []struct {
						x, y int
						bgra [4]byte
					}{
						{0, 0, [4]byte{}},
						{4, 4, [4]byte{}},
						{8, 12, [4]byte{0, 0, 128, 128}},
						{18, 12, [4]byte{0, 128, 64, 192}},
						{18, 24, [4]byte{0, 128, 0, 128}},
						{36, 12, [4]byte{255, 0, 0, 255}},
					} {
						want := sample.bgra
						if frame == 4 {
							want = [4]byte{}
						}
						if !transparent {
							for c := range 3 {
								want[c] += 255 - want[3]
							}
							want[3] = 255
						}
						x, y := sample.x*scale, sample.y*scale
						offset := y*fb.Stride + x*4
						for renderer, got := range map[string][4]byte{
							"Metal":    testPixel(target, x, y),
							"software": [4]byte(fb.Pix[offset : offset+4]),
						} {
							for c := range 4 {
								if delta := int(got[c]) - int(want[c]); delta < -2 || delta > 2 {
									t.Fatalf("%s transparent=%t scale=%d frame %d at (%d,%d): BGRA=%v want %v", renderer, transparent, scale, frame, x, y, got, want)
								}
							}
						}
					}
				}
			})
			runtime.GC()
		}
	}
}

func TestMetalSharedTextMatchesExpandedGlyphs(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("Metal init: %v", err)
	}
	host := shirei.GetHost()
	oldSize, oldScale, oldBudget := host.WindowSize, host.WindowScale, host.GlyphCacheBudgetBytes
	defer func() {
		host.WindowSize, host.WindowScale, host.GlyphCacheBudgetBytes = oldSize, oldScale, oldBudget
	}()
	host.WindowSize = shirei.Vec2{240, 140}
	host.GlyphCacheBudgetBytes = 16 << 20
	for _, scale := range []float32{1, 2} {
		host.WindowScale = scale
		w, h := int(240*scale), int(140*scale)
		sharedTarget, flatTarget := testSurface(w, h), testSurface(w, h)
		if sharedTarget == nil || flatTarget == nil {
			t.Fatal("test IOSurface is nil")
		}
		defer testRelease(sharedTarget)
		defer testRelease(flatTarget)
		for _, hue := range []float32{0, 210, 0} {
			var out shirei.FrameOutputData
			for range 3 {
				out = shirei.RunFrameFn(func() {
					shirei.ModAttrs(shirei.NoAnimate)
					shirei.Container(shirei.Attrs(shirei.Pad(8), shirei.MaxWidth(190), shirei.Clip, shirei.Corners(7), func(a *shirei.AttrSet) { a.Transparency = .2 }), func() {
						shirei.Label("office العربية 😀", shirei.TextColor(hue, 80, 40, 1))
						style := shirei.TextStyle()
						shirei.Text("Mixed sizes and colors", style,
							shirei.Span(0, 5, shirei.FontSize(22)),
							shirei.Span(6, 11, shirei.TextColor(hue, 80, 40, .6)),
							shirei.Span(12, 15, shirei.Fonts(shirei.Monospace...)))
						shaped := shirei.ShapeTextMax("Selection with wrapped words across several lines", style, 160)
						shirei.ShapedTextLayout(shaped, style, 4, 20)
					})
				})
			}
			var flat []shirei.Surface
			glyphs := 0
			for _, surface := range out.Surfaces {
				if surface.GlyphRunCount == 0 {
					flat = append(flat, surface)
					continue
				}
				for i := 0; i < int(surface.GlyphRunCount); i++ {
					g := surface.GlyphRunAt(i, out.GlyphRuns)
					flat = append(flat, shirei.Surface{Rect: g.Rect, Color1: g.Color, Color2: g.Color,
						FontId: g.FontId, GlyphId: g.GlyphId, GlyphOffset: g.GlyphOffset})
					glyphs++
				}
			}
			if glyphs < 20 {
				t.Fatal("text fixture did not emit enough glyphs")
			}
			if err := Render(sharedTarget, w, h, scale, out.Surfaces, out.GlyphRuns, out.GlyphsAdded, out.GlyphsEvicted, true); err != nil {
				t.Fatal(err)
			}
			if err := Render(flatTarget, w, h, scale, flat, nil, nil, nil, true); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(testPixels(sharedTarget, w, h), testPixels(flatTarget, w, h)) {
				t.Fatalf("Metal text differs at scale=%v hue=%v", scale, hue)
			}
		}
	}
}

func TestMetalTopLeftFill(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("Metal init: %v", err)
	}
	const w, h = 32, 32
	surf := testSurface(w, h)
	if surf == nil {
		t.Fatal("test IOSurface is nil")
	}
	defer testRelease(surf)

	red := shirei.Vec4{0, 100, 50, 1}
	surfaces := []shirei.Surface{{
		Rect:   shirei.Rect{Size: shirei.Vec2{10, 10}},
		Color1: red, Color2: red,
	}}
	if err := Render(surf, w, h, 1, surfaces, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}

	tl := testPixel(surf, 0, 0)
	// BGRA, opaque red ≈ (0, 0, 255, 255)
	if int(tl[2]) < 200 || int(tl[1]) > 40 || int(tl[0]) > 40 || tl[3] < 200 {
		t.Fatalf("top-left BGRA=%v want red", tl)
	}
	br := testPixel(surf, w-1, h-1)
	// cleared white
	if br[0] < 200 || br[1] < 200 || br[2] < 200 || br[3] < 200 {
		t.Fatalf("bottom-right BGRA=%v want white (Y flip?)", br)
	}
	// A pixel just below the 10x10 stamp should still be white if y-down is correct.
	below := testPixel(surf, 0, 20)
	if below[0] < 200 || below[1] < 200 || below[2] < 200 {
		t.Fatalf("pixel (0,20) BGRA=%v want white", below)
	}
}

func TestMetalRectClip(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("Metal init: %v", err)
	}
	const w, h = 32, 32
	surf := testSurface(w, h)
	if surf == nil {
		t.Fatal("test IOSurface is nil")
	}
	defer testRelease(surf)

	red := shirei.Vec4{0, 100, 50, 1}
	surfaces := []shirei.Surface{
		{
			Rect: shirei.Rect{Origin: shirei.Vec2{10, 10}, Size: shirei.Vec2{10, 10}},
			Clip: shirei.ClipPush,
		},
		{Rect: shirei.Rect{Size: shirei.Vec2{32, 32}}, Color1: red, Color2: red},
		{
			Rect: shirei.Rect{Origin: shirei.Vec2{10, 10}, Size: shirei.Vec2{10, 10}},
			Clip: shirei.ClipPop,
		},
	}
	if err := Render(surf, w, h, 1, surfaces, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	inside := testPixel(surf, 15, 15)
	if int(inside[2]) < 200 || inside[3] < 200 {
		t.Fatalf("clipped interior BGRA=%v want red", inside)
	}
	outside := testPixel(surf, 0, 0)
	if outside[0] < 200 || outside[1] < 200 || outside[2] < 200 {
		t.Fatalf("outside clip BGRA=%v want white", outside)
	}
}

func TestMetalTranslucentOverWhite(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("Metal init: %v", err)
	}
	const w, h = 16, 16
	surf := testSurface(w, h)
	if surf == nil {
		t.Fatal("test IOSurface is nil")
	}
	defer testRelease(surf)

	blackHalf := shirei.Vec4{0, 0, 0, 0.5}
	surfaces := []shirei.Surface{
		{Rect: shirei.Rect{Size: shirei.Vec2{w, h}}, Color1: blackHalf, Color2: blackHalf},
	}
	if err := Render(surf, w, h, 1, surfaces, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	p := testPixel(surf, 8, 8)
	// premul black 50% over white → ~127 gray
	for i := 0; i < 3; i++ {
		if int(p[i]) < 90 || int(p[i]) > 160 {
			t.Fatalf("BGRA=%v want ~50%% gray over white", p)
		}
	}
}

func TestMetalRoundedClip(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("Metal init: %v", err)
	}
	const w, h = 32, 32
	surf := testSurface(w, h)
	if surf == nil {
		t.Fatal("test IOSurface is nil")
	}
	defer testRelease(surf)

	red := shirei.Vec4{0, 100, 50, 1}
	surfaces := []shirei.Surface{
		{
			Rect:    shirei.Rect{Origin: shirei.Vec2{6, 6}, Size: shirei.Vec2{20, 20}},
			Corners: shirei.Vec4{10, 10, 10, 10},
			Clip:    shirei.ClipPush,
		},
		{Rect: shirei.Rect{Size: shirei.Vec2{32, 32}}, Color1: red, Color2: red},
		{
			Rect: shirei.Rect{Origin: shirei.Vec2{6, 6}, Size: shirei.Vec2{20, 20}},
			Clip: shirei.ClipPop,
		},
	}
	if err := Render(surf, w, h, 1, surfaces, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	cut := testPixel(surf, 6, 6)
	if cut[0] < 200 || cut[1] < 200 || cut[2] < 200 {
		t.Fatalf("rounded-clip outer corner BGRA=%v want white (scissor-only?)", cut)
	}
	inside := testPixel(surf, 16, 16)
	if int(inside[2]) < 200 || inside[3] < 200 {
		t.Fatalf("rounded-clip interior BGRA=%v want red", inside)
	}
}

func TestMetalRoundedCornerCut(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("Metal init: %v", err)
	}
	const w, h = 32, 32
	surf := testSurface(w, h)
	if surf == nil {
		t.Fatal("test IOSurface is nil")
	}
	defer testRelease(surf)

	red := shirei.Vec4{0, 100, 50, 1}
	surfaces := []shirei.Surface{{
		Rect:    shirei.Rect{Origin: shirei.Vec2{6, 6}, Size: shirei.Vec2{20, 20}},
		Color1:  red,
		Color2:  red,
		Corners: shirei.Vec4{10, 10, 10, 10},
	}}
	if err := Render(surf, w, h, 1, surfaces, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	// Outer TL of the corner square is outside the quarter-disk → white.
	cut := testPixel(surf, 6, 6)
	if cut[0] < 200 || cut[1] < 200 || cut[2] < 200 {
		t.Fatalf("rounded outer corner BGRA=%v want white (still square?)", cut)
	}
	inside := testPixel(surf, 16, 16)
	if int(inside[2]) < 200 || inside[3] < 200 {
		t.Fatalf("rounded interior BGRA=%v want red", inside)
	}
}

// TestMetalGradientCircleSmooth renders a white→black vertical gradient on a
// full circle. A circle decomposes into four corner coverage stamps with no
// interior rects, so the gradient must survive the coverage path (mode 1):
// each stamp carries band colors that the shader interpolates per pixel. If
// the coverage path drops color2, the circle renders as two flat halves —
// which is exactly how the regression looked on slider/toggle knobs.
func TestMetalGradientCircleSmooth(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("Metal init: %v", err)
	}
	const w, h = 32, 32
	surf := testSurface(w, h)
	if surf == nil {
		t.Fatal("test IOSurface is nil")
	}
	defer testRelease(surf)

	white := shirei.Vec4{0, 0, 100, 1}
	black := shirei.Vec4{0, 0, 0, 1}
	// Circle: 20×20 rect at (6,6) with radius 10 on every corner.
	surfaces := []shirei.Surface{{
		Rect:    shirei.Rect{Origin: shirei.Vec2{6, 6}, Size: shirei.Vec2{20, 20}},
		Color1:  white,
		Color2:  black,
		Corners: shirei.Vec4{10, 10, 10, 10},
	}}
	if err := Render(surf, w, h, 1, surfaces, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}

	// Down the center column the gradient t = (y+0.5-6)/20; the fill is gray
	// so any channel works. Expect ≈ 255·(1−t) within a loose tolerance.
	gray := func(y int) int { return int(testPixel(surf, 16, y)[0]) }
	checks := []struct{ y, want int }{
		{8, 223}, {12, 172}, {15, 134}, {17, 108}, {20, 70}, {24, 19},
	}
	for _, c := range checks {
		got := gray(c.y)
		if got < c.want-25 || got > c.want+25 {
			t.Errorf("center column y=%d gray=%d, want ≈%d (flat corner bands?)", c.y, got, c.want)
		}
	}
	// The two-flat-halves failure mode also shows as a hard step across the
	// vertical midpoint; the true gradient changes gently there.
	if step := gray(15) - gray(17); step > 40 || step < 0 {
		t.Errorf("gray step across midline = %d, want small positive (two flat bands?)", step)
	}
}

func TestMetalImage(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("Metal init: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0] = 255
		img.Pix[i+1] = 0
		img.Pix[i+2] = 0
		img.Pix[i+3] = 255
	}
	id := shirei.UseImage("gpurender-test-red", img)
	if id == 0 {
		t.Fatal("UseImage returned 0")
	}
	const w, h = 32, 32
	surf := testSurface(w, h)
	if surf == nil {
		t.Fatal("test IOSurface is nil")
	}
	defer testRelease(surf)

	surfaces := []shirei.Surface{{
		Rect:    shirei.Rect{Size: shirei.Vec2{8, 8}},
		ImageId: id,
	}}
	if err := Render(surf, w, h, 1, surfaces, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	tl := testPixel(surf, 0, 0)
	if int(tl[2]) < 200 || int(tl[1]) > 40 || int(tl[0]) > 40 || tl[3] < 200 {
		t.Fatalf("image top-left BGRA=%v want red", tl)
	}
	outside := testPixel(surf, 20, 20)
	if outside[0] < 200 || outside[1] < 200 || outside[2] < 200 {
		t.Fatalf("outside image BGRA=%v want white", outside)
	}
}

func TestMetalTranslucentBlackImageOverWhite(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("Metal init: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0] = 0
		img.Pix[i+1] = 0
		img.Pix[i+2] = 0
		img.Pix[i+3] = 31 // ~12% premul black
	}
	id := shirei.UseImage("gpurender-test-shadow-black", img)
	const w, h = 16, 16
	surf := testSurface(w, h)
	if surf == nil {
		t.Fatal("test IOSurface is nil")
	}
	defer testRelease(surf)

	surfaces := []shirei.Surface{{
		Rect:    shirei.Rect{Size: shirei.Vec2{8, 8}},
		ImageId: id,
	}}
	if err := Render(surf, w, h, 1, surfaces, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	p := testPixel(surf, 4, 4)
	// premul black 12% over white → ~224
	for i := 0; i < 3; i++ {
		if int(p[i]) < 200 || int(p[i]) > 240 {
			t.Fatalf("BGRA=%v want ~12%% darken of white (~224)", p)
		}
	}
	outside := testPixel(surf, 12, 12)
	if outside[0] < 200 || outside[1] < 200 || outside[2] < 200 {
		t.Fatalf("outside BGRA=%v want white", outside)
	}
}

// TestMetalGlyphClipMatchesCrop compares clipped glyph rendering with a crop of
// the unclipped image. The fixture includes italic overhang and a color glyph;
// positions cross all clip edges at integer and fractional display scales.
func TestMetalGlyphClipMatchesCrop(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("Metal init: %v", err)
	}
	host := shirei.GetHost()
	oldSize, oldScale, oldBudget := host.WindowSize, host.WindowScale, host.GlyphCacheBudgetBytes
	defer func() { host.WindowSize, host.WindowScale, host.GlyphCacheBudgetBytes = oldSize, oldScale, oldBudget }()
	host.WindowSize = shirei.Vec2{128, 96}
	host.GlyphCacheBudgetBytes = 16 << 20
	for _, scale := range []float32{1, 1.5, 2} {
		host.WindowScale = scale
		var out shirei.FrameOutputData
		for range 3 {
			out = shirei.RunFrameFn(func() {
				shirei.ModAttrs(shirei.NoAnimate)
				shirei.Container(shirei.Attrs(shirei.FixSize(400, 40)), func() {
					shirei.ModAttrs(shirei.UnsetMaxCross)
					shirei.Label("fij office 😀 fjij", shirei.FontSize(24), shirei.FontStyle(shirei.StyleItalic), shirei.TextColor(220, 75, 40, .7))
				})
			})
		}
		var line shirei.Surface
		for _, s := range out.Surfaces {
			if s.GlyphRunCount > 0 {
				line = s
				break
			}
		}
		if line.GlyphRunCount < 8 {
			t.Fatal("missing shaped glyph fixture")
		}
		w, h := int(128*scale), int(96*scale)
		full, clipped := testSurface(w, h), testSurface(w, h)
		if full == nil || clipped == nil {
			t.Fatal("no IOSurface")
		}
		defer testRelease(full)
		defer testRelease(clipped)
		clip := shirei.Rect{Origin: shirei.Vec2{16, 16}, Size: shirei.Vec2{64, 64}}
		for _, x := range []float32{-100, 0, 70} {
			for _, y := range []float32{-5, 20, 70} {
				line.Rect.Origin = shirei.Vec2{x, y}
				if err := Render(full, w, h, scale, []shirei.Surface{line}, out.GlyphRuns, out.GlyphsAdded, out.GlyphsEvicted, true); err != nil {
					t.Fatal(err)
				}
				surfaces := []shirei.Surface{{Rect: clip, Clip: shirei.ClipPush}, line, {Rect: clip, Clip: shirei.ClipPop}}
				if err := Render(clipped, w, h, scale, surfaces, out.GlyphRuns, nil, nil, true); err != nil {
					t.Fatal(err)
				}
				expected, actual := testPixels(full, w, h), testPixels(clipped, w, h)
				lo, hi := int(16*scale), int(80*scale)
				for py := 0; py < h; py++ {
					for px := 0; px < w; px++ {
						off := (py*w + px) * 4
						if px < lo || px >= hi || py < lo || py >= hi {
							copy(expected[off:off+4], []byte{255, 255, 255, 255})
						}
					}
				}
				if !bytes.Equal(expected, actual) {
					t.Fatalf("glyph crop differs: scale=%v origin=(%v,%v)", scale, x, y)
				}
			}
		}
	}
}

func testPixels(s unsafe.Pointer, w, h int) []byte {
	p := uintptr(s)
	ioSurfaceLock(p, kIOSurfaceLockReadOnly, nil)
	defer ioSurfaceUnlock(p, kIOSurfaceLockReadOnly, nil)
	base, stride := ioSurfaceGetBaseAddress(p), ioSurfaceGetBytesPerRow(p)
	pixels := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		copy(pixels[y*w*4:(y+1)*w*4], unsafe.Slice((*byte)(unsafe.Pointer(base+uintptr(y)*stride)), w*4))
	}
	return pixels
}
