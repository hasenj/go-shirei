package shirei

import "testing"

// Compositing Shirei over a host's own scene: the transparent clear and the
// UI-or-scene click answer.

// embedFrame is one opaque 100x50 card in the top-left of a 400x300 window.
func embedFrame() {
	Container(Attrs(Float(0, 0), FixWidth(100), FixHeight(50), Background(0, 0, 50, 1)), func() {})
}

func embedRender(t *testing.T, transparent bool) *Framebuffer {
	t.Helper()
	ui.Host.WindowSize = Vec2{400, 300}
	ui.Host.WindowScale = 1
	ui.Host.HeadlessRender = true
	defer func() { ui.Host.HeadlessRender = false }()

	var out FrameOutputData
	for range 2 {
		out = RunFrameFn(embedFrame)
	}
	var rend SoftRenderer
	rend.Transparent = transparent
	return rend.Render(out.Surfaces, out.GlyphRuns, 400, 300, 1)
}

func alphaAt(fb *Framebuffer, x, y int) byte {
	return fb.Pix[y*fb.Stride+x*4+int(pixelOrder()[3])]
}

func TestTransparentClearLeavesUnpaintedPixelsClear(t *testing.T) {
	fb := embedRender(t, true)
	if a := alphaAt(fb, 10, 10); a != 0xff {
		t.Errorf("inside the card: alpha = %d, want 255", a)
	}
	if a := alphaAt(fb, 300, 200); a != 0 {
		t.Errorf("outside the card: alpha = %d, want 0", a)
	}
}

func TestDefaultClearStaysOpaque(t *testing.T) {
	fb := embedRender(t, false)
	if a := alphaAt(fb, 300, 200); a != 0xff {
		t.Errorf("outside the card: alpha = %d, want 255 (opaque white canvas)", a)
	}
}

func TestAnyHoveredIgnoresTheRoot(t *testing.T) {
	ui.Host.WindowSize = Vec2{400, 300}
	ui.Host.WindowScale = 1

	run := func(p Vec2) bool {
		ui.Host.Input.MousePoint = p
		for range 2 {
			RunFrameFn(embedFrame)
		}
		return AnyHovered()
	}
	if run(Vec2{50, 25}) != true {
		t.Error("pointer on the card: AnyHovered = false, want true")
	}
	if run(Vec2{300, 200}) != false {
		t.Error("pointer on empty window: AnyHovered = true, want false")
	}
	if run(Vec2{-1, -1}) != false {
		t.Error("pointer outside the window: AnyHovered = true, want false")
	}
}
