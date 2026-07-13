package shirei

import (
	"image"
	"image/png"
	"os"
	"time"
)

// ResetInputSession restores the neutral input/focus state of a freshly
// launched app: mouse parked offscreen (nothing hovered), no pending input,
// nothing focused or active. Headless render sessions (RenderToImage,
// snapshot tests, benchmarks) call it so repeated invocations in one
// process don't leak the previous invocation's mouse position or focus —
// e.g. a stale nextFocused pointing at a dead container silently
// suppresses AutoFocus in the next invocation.
func ResetInputSession() {
	InputState.MousePoint = Vec2{-1 << 20, -1 << 20}
	InputState.MouseButton = 0
	InputState.DownKeys = nil
	InputState.Modifiers = 0
	InputState.Composition = ""
	InputState.CompositionSel = [2]int{}
	CaretPos = Vec2{}
	CaretHeight = 0
	CompositionPos = Vec2{}
	FrameInput.Mouse = 0
	FrameInput.Motion = Vec2{}
	FrameInput.Scroll = Vec2{}
	FrameInput.Key = 0
	FrameInput.Text = ""
	active = nil
	focused = nil
	prevFocused = nil
	nextFocused = nil
	lastClickTime = time.Time{}
	lastClickPoint = Vec2{}
	clickStreak = 0
}

// HeadlessRender is true only while RenderToImage is producing a frame. Widgets
// consult it to suppress wall-clock-driven visuals (e.g. the text caret blink),
// so headless output is reproducible regardless of how long the settle loop
// took — unlike NoAnimate, which is bundled into Viewport and so can't
// distinguish "offline render" from "an ordinary panel that doesn't animate".
var HeadlessRender bool

// RenderToImage runs fn headlessly at the given logical size and
// software-renders the settled frame — the engine behind RenderToPNG, and
// directly useful for snapshot tests that compare in memory. Not meant to
// run alongside a live window: it overwrites the global WindowSize/
// WindowScale.
//
// It runs at least two frames, then keeps going (capped) while the frame
// requests a follow-up: widgets that size themselves from previous-frame
// data — virtual lists, GetResolvedSize users — need the extra passes to
// settle. The cap keeps self-rerendering content (e.g. a focused text
// input's blinking caret) from looping forever; NoAnimate is forced so
// animations don't count against it.
func RenderToImage(width, height int, fn FrameFn) *image.RGBA {
	// System fonts: scanned in package init (InitFontSubsystem).
	HeadlessRender = true
	defer func() { HeadlessRender = false }()
	WindowSize = Vec2{float32(width), float32(height)}
	WindowScale = 1
	if GlyphCacheBudgetBytes == 0 {
		// text rendering is gated on the glyph cache being enabled (see
		// glyphcache.go); without this, the output has no text at all
		GlyphCacheBudgetBytes = 16 << 20
	}

	ResetInputSession()

	wrapped := func() {
		ModAttrs(func(a *AttrSet) { a.NoAnimate = true })
		fn()
	}

	var out FrameOutputData
	for i := 0; i < 8; i++ {
		out = RunFrameFn(wrapped)
		if i >= 1 && !out.NextFrameRequested {
			break
		}
	}

	var rend SoftRenderer
	fb := rend.Render(out.Surfaces, width, height, 1)
	return fb.ToRGBA()
}

// RenderToPNG runs fn headlessly at the given logical size and writes the
// software-rendered result to path — the standard way to verify UI changes
// without opening a window (apps typically expose it as a --png flag; see
// cocoabackend/example and examples/see_pprof).
func RenderToPNG(path string, width, height int, fn FrameFn) error {
	img := RenderToImage(width, height, fn)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
