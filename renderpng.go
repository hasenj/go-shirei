package shirei

import (
	"image"
	"image/png"
	"os"
	"time"

	g "go.hasen.dev/generic"
)

// ResetInputSession restores the neutral input/focus state of a freshly
// launched app: mouse parked offscreen (nothing hovered), no pending input,
// nothing focused or active. Headless render sessions (RenderToImage,
// snapshot tests, benchmarks) call it so repeated invocations in one
// process don't leak the previous invocation's mouse position or focus —
// e.g. a stale nextFocused pointing at a dead container silently
// suppresses AutoFocus in the next invocation.
func ResetInputSession() {
	g.Reset(&ui.Host.Input)
	// Park offscreen so nothing is hovered (zero is a valid top-left point).
	ui.Host.Input.MousePoint = Vec2{-1 << 20, -1 << 20}

	g.Reset(&ui.Host.FrameInput)
	ui.Host.CaretPos = Vec2{}
	ui.Host.CaretHeight = 0
	ui.Host.CompositionPos = Vec2{}
	ui.Host.WantsKeyboard = false
	ui.touchingList = ui.touchingList[:0]

	ui.active = nil
	ui.focused = nil
	ui.prevFocused = nil
	ui.nextFocused = nil
	ui.lastClickTime = time.Time{}
	g.Reset(&ui.lastClickPoint)
	ui.clickStreak = 0
}

// HeadlessRender (Host.HeadlessRender) is true only while RenderToImage is
// producing a frame. Widgets consult it to suppress wall-clock-driven visuals
// (e.g. the text caret blink), so headless output is reproducible regardless
// of how long the settle loop took — unlike NoAnimate, which is bundled into
// Viewport and so can't distinguish "offline render" from "an ordinary panel
// that doesn't animate".

// HeadlessScale is the device-pixel ratio used by RenderToImage / RenderToPNG
// (2 = retina). Layout sizes stay in logical points; output bitmaps are
// scale times larger so text and edges stay sharp when viewed on HiDPI
// displays or scaled down in docs.
const HeadlessScale float32 = 2

// RenderToImage runs fn headlessly at the given logical size and
// software-renders the settled frame — the engine behind RenderToPNG, and
// directly useful for snapshot tests that compare in memory. Not meant to
// run alongside a live window: it overwrites Host.WindowSize / WindowScale.
//
// Output is HeadlessScale device pixels per logical point (retina by default).
//
// It runs at least two frames, then keeps going (capped) while the frame
// requests a follow-up: widgets that size themselves from previous-frame
// data — virtual lists, GetResolvedSize users — need the extra passes to
// settle. The cap keeps self-rerendering content (e.g. a focused text
// input's blinking caret) from looping forever; NoAnimate is forced so
// animations don't count against it.
func RenderToImage(width, height int, fn FrameFn) *image.RGBA {
	// System fonts: scanned in package init (InitFontSubsystem).
	ui.Host.HeadlessRender = true
	defer func() { ui.Host.HeadlessRender = false }()
	ui.Host.WindowSize = Vec2{float32(width), float32(height)}
	ui.Host.WindowScale = HeadlessScale
	if ui.Host.GlyphCacheBudgetBytes == 0 {
		// text rendering is gated on the glyph cache being enabled (see
		// glyphcache.go); without this, the output has no text at all
		ui.Host.GlyphCacheBudgetBytes = 16 << 20
	}

	ResetInputSession()

	wrapped := func() {
		ModAttrs(func(a *AttrSet) { a.Animations = 0 })
		fn()
	}

	var out FrameOutputData
	for i := 0; i < 8; i++ {
		out = RunFrameFn(wrapped)
		if i >= 1 && !out.NextFrameRequested {
			break
		}
	}

	devW := int(Roundf32(float32(width) * HeadlessScale))
	devH := int(Roundf32(float32(height) * HeadlessScale))
	var rend SoftRenderer
	fb := rend.Render(out.Surfaces, devW, devH, HeadlessScale)
	return fb.ToRGBA()
}

// RenderToPNG runs fn headlessly at the given logical size and writes the
// software-rendered result to path — the standard way to verify UI changes
// without opening a window (apps typically expose it as a --png flag; see
// cocoabackend/example and examples/see_pprof). The PNG is HeadlessScale
// device pixels per logical point (same as RenderToImage).
func RenderToPNG(path string, width, height int, fn FrameFn) error {
	img := RenderToImage(width, height, fn)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
