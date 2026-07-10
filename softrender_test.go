package shirei

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Snapshot tests for the core software renderer. Each test builds a frame with
// the public API, renders it through SoftRenderer, encodes the buffer as PNG (for
// the test only — the runtime never touches PNG) and compares against a committed
// snapshot in testdata/softrender/. These cover the deterministic, font- and
// platform-independent primitives (fills, gradients, borders, rounded shapes,
// clips, transparency, device scale); glyph and image parity is validated against
// the cocoa oracle in softrender_parity_darwin_test.go.
//
// Missing snapshot -> created and the test passes (review and commit it).
// Mismatch -> fails and writes <name>.actual.png. UPDATE_SNAPSHOTS=1 regenerates.

var softScopeIds = map[string]any{}

// softScope interns the scope string so a test's container ids are stable across
// the two frames (see boxedScopeId in layout_tests/bitmap.go for why).
func softScope(s string) any {
	id, ok := softScopeIds[s]
	if !ok {
		id = s
		softScopeIds[s] = id
	}
	return id
}

func softRenderImage(scope string, w, h int, scale float32, fn FrameFn) *image.RGBA {
	WindowSize = Vec2{float32(w), float32(h)}
	WindowScale = scale
	sid := softScope(scope)

	var out FrameOutputData
	for range 2 {
		out = RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.NoAnimate = true })
			ContainerWithKey(sid, AttrSet{}, fn)
		})
	}

	var rend SoftRenderer
	devW := int(Roundf32(float32(w) * scale))
	devH := int(Roundf32(float32(h) * scale))
	fb := rend.Render(out.Surfaces, devW, devH, scale)
	return fb.ToRGBA()
}

func softSnapshot(t *testing.T, name string, w, h int, scale float32, fn FrameFn) {
	t.Helper()
	path := filepath.Join("testdata", "softrender", name+".png")
	img := softRenderImage(name, w, h, scale, fn)

	if os.Getenv("UPDATE_SNAPSHOTS") != "" {
		writeSoftPNG(t, path, img)
		return
	}

	saved, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		writeSoftPNG(t, path, img)
		t.Logf("created snapshot %s; review it and commit it", path)
		return
	}
	if err != nil {
		t.Fatalf("reading snapshot %s: %v", path, err)
	}
	want, err := png.Decode(bytes.NewReader(saved))
	if err != nil {
		t.Fatalf("decoding snapshot %s: %v", path, err)
	}
	if !sameRGBA(img, softToRGBA(want)) {
		actual := strings.TrimSuffix(path, ".png") + ".actual.png"
		writeSoftPNG(t, actual, img)
		t.Errorf("render does not match snapshot %s; wrote %s", path, actual)
	}
}

func softToRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok && r.Bounds().Min == (image.Point{}) {
		return r
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

func sameRGBA(a, b *image.RGBA) bool {
	return a.Bounds() == b.Bounds() && bytes.Equal(a.Pix, b.Pix)
}

func writeSoftPNG(t *testing.T, path string, img *image.RGBA) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}

// box places a fixed-size element at an absolute window position.
func box(x, y, w, h float32, a AttrSet) {
	a.Floats = true
	a.Float = Vec2{x, y}
	a.MinSize = Vec2{w, h}
	a.MaxSize = Vec2{w, h}
	Element(a)
}

var (
	red   = Vec4{0, 80, 50, 1}
	green = Vec4{120, 70, 45, 1}
	blue  = Vec4{220, 75, 55, 1}
	teal  = Vec4{180, 60, 45, 1}
)

func TestSoftRenderFills(t *testing.T) {
	softSnapshot(t, "fills", 200, 120, 1, func() {
		box(10, 10, 80, 40, AttrSet{Background: red})
		box(110, 10, 80, 40, AttrSet{Background: green})
		box(10, 70, 80, 40, AttrSet{Background: blue})
		// translucent fill over white
		box(110, 70, 80, 40, AttrSet{Background: Vec4{0, 80, 50, 0.4}})
	})
}

func TestSoftRenderGradient(t *testing.T) {
	softSnapshot(t, "gradient", 200, 120, 1, func() {
		// Gradient is the delta added to Background to get the bottom color.
		box(10, 10, 180, 100, AttrSet{
			Background: Vec4{220, 75, 55, 1},
			Gradient:   Vec4{-140, -10, -20, 0}, // shift toward red, darker
		})
	})
}

func TestSoftRenderRounded(t *testing.T) {
	softSnapshot(t, "rounded", 200, 120, 1, func() {
		box(10, 10, 80, 90, AttrSet{Background: teal, Corners: N4(16)})
		// pill: radius >= half-extent
		box(110, 30, 80, 40, AttrSet{Background: green, Corners: N4(40)})
		// per-corner radii
		box(110, 80, 80, 30, AttrSet{Background: blue, Corners: Vec4{0, 15, 0, 15}})
	})
}

func TestSoftRenderBorders(t *testing.T) {
	softSnapshot(t, "borders", 240, 120, 1, func() {
		box(10, 10, 80, 40, AttrSet{Border: Border{BorderColor: red, BorderWidth: 4}})
		box(110, 10, 80, 40, AttrSet{Background: Vec4{60, 60, 92, 1},
			Border: Border{BorderColor: green, BorderWidth: 2}})
		box(10, 70, 100, 40, AttrSet{
			Border:  Border{BorderColor: blue, BorderWidth: 6},
			Corners: N4(14),
		})
		// filled + rounded border
		box(130, 70, 100, 40, AttrSet{Background: teal,
			Border:  Border{BorderColor: red, BorderWidth: 3},
			Corners: N4(20),
		})
	})
}

func TestSoftRenderRectClip(t *testing.T) {
	softSnapshot(t, "rect_clip", 160, 120, 1, func() {
		ContainerWithKey(softScope("rect_clip_box"), AttrSet{
			Floats: true, Float: Vec2{20, 20}, MinSize: Vec2{120, 80}, MaxSize: Vec2{120, 80},
			Background: Vec4{0, 0, 88, 1}, Clip: true,
		}, func() {
			box(60, 40, 120, 80, AttrSet{Background: red}) // spills past the lower-right edge
		})
	})
}

func TestSoftRenderRoundedClip(t *testing.T) {
	softSnapshot(t, "rounded_clip", 160, 140, 1, func() {
		ContainerWithKey(softScope("rounded_clip_box"), AttrSet{
			Floats: true, Float: Vec2{20, 20}, MinSize: Vec2{120, 100}, MaxSize: Vec2{120, 100},
			Background: green, Corners: N4(28), Clip: true,
		}, func() {
			// a fill that would bleed into the rounded corners if the clip were square
			box(0, 0, 120, 100, AttrSet{Background: blue})
		})
	})
}

func TestSoftRenderTransparency(t *testing.T) {
	softSnapshot(t, "transparency", 180, 120, 1, func() {
		ContainerWithKey(softScope("trans_group"), AttrSet{
			Floats: true, Float: Vec2{20, 20}, MinSize: Vec2{140, 80}, MaxSize: Vec2{140, 80},
			Transparency: 0.5,
		}, func() {
			box(0, 0, 90, 60, AttrSet{Background: red})
			box(50, 20, 90, 60, AttrSet{Background: blue})
		})
	})
}

// ensureImageAsset writes a deterministic 64x64 source image (four colored
// quadrants with a 1px black frame) if it does not already exist, and returns its
// path. Used by the image snapshot test; committed so the asset is stable.
func ensureImageAsset(t *testing.T) string {
	t.Helper()
	path := filepath.Join("testdata", "softrender", "_img_src.png")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	const n = 64
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	quad := func(x, y int) (r, g, b uint8) {
		switch {
		case x < n/2 && y < n/2:
			return 220, 40, 40 // red
		case x >= n/2 && y < n/2:
			return 40, 160, 60 // green
		case x < n/2 && y >= n/2:
			return 50, 90, 210 // blue
		default:
			return 230, 200, 40 // yellow
		}
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			i := img.PixOffset(x, y)
			if x == 0 || y == 0 || x == n-1 || y == n-1 {
				img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 0, 0, 0, 255
				continue
			}
			r, g, b := quad(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, 255
		}
	}
	writeSoftPNG(t, path, img)
	return path
}

func TestSoftRenderImage(t *testing.T) {
	path := ensureImageAsset(t)
	softSnapshot(t, "image", 200, 100, 1, func() {
		// natural size (1:1 blit, exercises the RGBA->BGRA swizzle + blend)
		Container(AttrSet{Floats: true, Float: Vec2{10, 18},
			MinSize: Vec2{64, 64}, MaxSize: Vec2{64, 64}, Clip: true}, func() {
			current.imageId = imageIdForTest(path)
		})
		// downscaled to fit a 32-tall box (exercises BiLinear scaling)
		Container(AttrSet{Floats: true, Float: Vec2{100, 34},
			MinSize: Vec2{32, 32}, MaxSize: Vec2{32, 32}, Clip: true}, func() {
			current.imageId = imageIdForTest(path)
		})
	})
}

// imageIdForTest loads the image (synchronously for small files) and returns its id.
func imageIdForTest(path string) ImageId {
	LoadImage(path)
	return GetImageId(path)
}

// TestSoftRenderIntoMatchesRender verifies that rendering into a caller-owned buffer
// with a padded (over-aligned) row stride produces the same visible pixels as the
// normal Render path — i.e. the renderer honors an arbitrary stride, which a
// backend-owned IOSurface / DIB section may impose.
func TestSoftRenderIntoMatchesRender(t *testing.T) {
	const w, h = 90, 70
	WindowSize = Vec2{w, h}
	WindowScale = 1
	sid := softScope("render_into")
	frame := func() {
		ModAttrs(func(a *AttrSet) { a.NoAnimate = true })
		ContainerWithKey(sid, AttrSet{}, func() {
			box(8, 6, 50, 36, AttrSet{Background: red, Corners: N4(10)})
			box(30, 22, 50, 36, AttrSet{Background: Vec4{220, 75, 55, 0.5}})
			box(0, 50, 90, 20, AttrSet{Background: teal})
		})
	}
	var out FrameOutputData
	for range 2 {
		out = RunFrameFn(frame)
	}

	var r1 SoftRenderer
	want := r1.Render(out.Surfaces, w, h, 1).ToRGBA()

	stride := w*4 + 40 // deliberately padded
	dst := make([]byte, stride*h)
	var r2 SoftRenderer
	r2.RenderInto(dst, stride, w, h, 1, out.Surfaces)
	got := (&Framebuffer{W: w, H: h, Stride: stride, Pix: dst}).ToRGBA()

	if !sameRGBA(want, got) {
		t.Error("RenderInto with padded stride differs from Render")
	}
}

// BenchmarkSoftRender measures pure rasterization throughput (Render only, with
// the surface list built once) on a dense grid of rounded, bordered cells inside
// a rounded scroll clip — the kind of surface count a real UI produces. The plan
// targets the cocoa CG offscreen raster (~11ms for demo2 at device px); this is
// the number to watch and the motivation for the phase-2 corner cache.
func BenchmarkSoftRender(b *testing.B) {
	const (
		w, h  = 1200, 800
		scale = 2.0
		cols  = 30
		rows  = 20
	)
	WindowSize = Vec2{w, h}
	WindowScale = scale
	sid := softScope("bench_grid")

	frame := func() {
		ModAttrs(func(a *AttrSet) { a.NoAnimate = true })
		ContainerWithKey(sid, AttrSet{}, func() {
			ContainerWithKey(softScope("bench_clip"), AttrSet{
				Floats: true, Float: Vec2{0, 0},
				MinSize: Vec2{w, h}, MaxSize: Vec2{w, h},
				Corners: N4(24), Clip: true,
			}, func() {
				cw, ch := float32(w)/cols, float32(h)/rows
				for r := 0; r < rows; r++ {
					for c := 0; c < cols; c++ {
						box(float32(c)*cw+4, float32(r)*ch+4, cw-8, ch-8, AttrSet{
							Background: Vec4{float32((c * 12) % 360), 60, 55, 1},
							Border:     Border{BorderColor: Vec4{0, 0, 20, 1}, BorderWidth: 2},
							Corners:    N4(8),
						})
					}
				}
			})
		})
	}

	var out FrameOutputData
	for range 2 {
		out = RunFrameFn(frame)
	}
	devW := int(Roundf32(w * scale))
	devH := int(Roundf32(h * scale))

	var rend SoftRenderer
	rend.noRegionCache = true // measure raw rasterization, not steady-state cache hits
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		rend.Render(out.Surfaces, devW, devH, scale)
	}
}

// Retina: same shapes at 2x device scale exercise the coordinate scaling and
// device-px corner radii.
func TestSoftRenderRetina(t *testing.T) {
	softSnapshot(t, "retina_2x", 160, 100, 2, func() {
		box(10, 10, 60, 80, AttrSet{Background: teal, Corners: N4(12)})
		box(90, 10, 60, 35, AttrSet{Background: red,
			Border: Border{BorderColor: blue, BorderWidth: 3}, Corners: N4(10)})
		box(90, 55, 60, 35, AttrSet{
			Background: Vec4{220, 75, 55, 1}, Gradient: Vec4{-100, 0, -25, 0}})
	})
}
