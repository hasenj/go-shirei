package widgets

import (
	"fmt"
	"time"

	. "go.hasen.dev/shirei"
)

type fpsPanelState struct {
	position Vec2
}

type fpsSample struct {
	t       time.Time
	produce time.Duration
	render  time.Duration
	painted bool
	gpu     bool
}

var fpsPanel = fpsPanelState{position: Vec2{10, 90}}

const fpsWindow = time.Second
const fpsCap = 128

var (
	fpsBuf         [fpsCap]fpsSample
	fpsHead        int
	fpsN           int
	fpsPaintGen    uint64
	fpsShownAt     time.Time
	fpsShowProduce time.Duration
	fpsShowRender  time.Duration
	fpsShowTotal   time.Duration
	fpsShowGPU     bool
)

// FPSCounter is a floating, draggable panel of produce/render times averaged
// over the last second. Call it as a direct statement in the UI; it floats
// like ProfileButton.
//
// No-op unless FPS_COUNTER=1. Safe to leave at every call site permanently.
func FPSCounter() {
	FPSCounterStyled(CurrentColorScheme.Overlay)
}

// FPSCounterStyled supplies the floating panel's surface colors.
func FPSCounterStyled(style SurfaceColors) {
	if !FPS_ENV {
		return
	}

	h := GetHost()
	s := fpsSample{
		t:       time.Now(),
		produce: h.LayoutTime,
		gpu:     h.PaintGPU,
	}
	if h.PaintGen != fpsPaintGen {
		s.painted = true
		s.render = h.PaintTime
		fpsPaintGen = h.PaintGen
	}
	fpsBuf[fpsHead] = s
	fpsHead = (fpsHead + 1) % fpsCap
	if fpsN < fpsCap {
		fpsN++
	}

	if fpsShownAt.IsZero() || s.t.Sub(fpsShownAt) >= fpsWindow {
		cutoff := s.t.Add(-fpsWindow)
		var pSum, rSum time.Duration
		var pN, rN int
		gpu := s.gpu
		for i := 0; i < fpsN; i++ {
			idx := (fpsHead - fpsN + i + fpsCap) % fpsCap
			e := fpsBuf[idx]
			if e.t.Before(cutoff) {
				continue
			}
			pSum += e.produce
			pN++
			if e.painted {
				rSum += e.render
				rN++
				gpu = e.gpu
			}
		}
		if pN > 0 {
			fpsShowProduce = pSum / time.Duration(pN)
		}
		if rN > 0 {
			fpsShowRender = rSum / time.Duration(rN)
		}
		fpsShowTotal = fpsShowProduce + fpsShowRender
		fpsShowGPU = gpu
		fpsShownAt = s.t
	}
	kind := "sw"
	if fpsShowGPU {
		kind = "gpu"
	}

	ContainerWithKey(&fpsPanel, Attrs(FloatVec(fpsPanel.position), InFront, BackgroundVec(style.Background), Corners(4), Pad(4), Gap(2), NoAnimate), func() {
		// Whole panel is the drag handle (labels cover the box; Directly would
		// only hit the 4px pad).
		PressAction()
		if IsActive() {
			fpsPanel.position = Vec2Add(fpsPanel.position, GetFrameInput().Motion)
		}
		sz := GetResolvedSize()
		br := Vec2Add(fpsPanel.position, sz)
		if br[0] > GetHost().WindowSize[0] {
			fpsPanel.position[0] = GetHost().WindowSize[0] - sz[0]
		}
		if br[1] > GetHost().WindowSize[1] {
			fpsPanel.position[1] = GetHost().WindowSize[1] - sz[1]
		}
		mono := func(line string) {
			Label(line, FontSize(10), TextColorVec(style.Text), Fonts(Monospace...))
		}
		us := func(d time.Duration) string { return fmt.Sprintf("%dµs", d.Microseconds()) }
		mono(fmt.Sprintf("frame    %s", us(fpsShowTotal)))
		mono(fmt.Sprintf("produce  %s", us(fpsShowProduce)))
		mono(fmt.Sprintf("render   %s %s", us(fpsShowRender), kind))
	})
}
