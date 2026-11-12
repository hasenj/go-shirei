//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"testing"

	"go.hasen.dev/shirei"
)

func resetMouseSample() {
	shirei.ResetInputSession()
	shirei.GetHost().WindowScale = 1
	shirei.GetInputState().MousePoint = shirei.Vec2{8, 9}
}

func TestPressAndReleaseInOneDrainKeepBothBits(t *testing.T) {
	resetMouseSample()

	mouseButton(shirei.MousePrimary, shirei.MouseClick, 8, 9)
	mouseButton(shirei.MousePrimary, shirei.MouseRelease, 18, 9)

	got := shirei.GetFrameInput().Mouse
	if got != shirei.MouseClick|shirei.MouseRelease {
		t.Fatalf("primary sample = %v, want click|release", got)
	}
	if p := shirei.GetInputState().MousePoint; p != (shirei.Vec2{18, 9}) {
		t.Fatalf("MousePoint = %v, want release position", p)
	}
	if m := shirei.GetFrameInput().Motion; m != (shirei.Vec2{10, 0}) {
		t.Fatalf("Motion = %v, want {10, 0}", m)
	}
	if shirei.GetInputState().MouseButton != shirei.MousePrimary {
		t.Fatalf("MouseButton = %v", shirei.GetInputState().MouseButton)
	}

	// The press was already sampled. The later release is only a release.
	*shirei.GetFrameInput() = shirei.FrameInputData{}
	mouseButton(shirei.MousePrimary, shirei.MouseRelease, 18, 14)
	if shirei.GetFrameInput().Mouse != shirei.MouseRelease {
		t.Fatalf("held release = %v, want MouseRelease", shirei.GetFrameInput().Mouse)
	}

	for _, btn := range []shirei.MouseButton{shirei.MouseSecondary, shirei.MouseTertiary} {
		*shirei.GetFrameInput() = shirei.FrameInputData{}
		mouseButton(btn, shirei.MouseClick, 4, 5)
		mouseButton(btn, shirei.MouseRelease, 4, 5)
		if shirei.GetFrameInput().Mouse != shirei.MouseClick|shirei.MouseRelease {
			t.Fatalf("button %v sample = %v", btn, shirei.GetFrameInput().Mouse)
		}
		if shirei.GetInputState().MouseButton != btn {
			t.Fatalf("MouseButton = %v, want %v", shirei.GetInputState().MouseButton, btn)
		}
	}

	// A second click in the same sample collapses onto the same two bits.
	*shirei.GetFrameInput() = shirei.FrameInputData{}
	mouseButton(shirei.MousePrimary, shirei.MouseClick, 1, 1)
	mouseButton(shirei.MousePrimary, shirei.MouseRelease, 1, 1)
	mouseButton(shirei.MousePrimary, shirei.MouseClick, 1, 1)
	mouseButton(shirei.MousePrimary, shirei.MouseRelease, 1, 1)
	if shirei.GetFrameInput().Mouse != shirei.MouseClick|shirei.MouseRelease {
		t.Fatalf("collapsed sample = %v", shirei.GetFrameInput().Mouse)
	}

	// A different button replaces the sample.
	*shirei.GetFrameInput() = shirei.FrameInputData{}
	mouseButton(shirei.MousePrimary, shirei.MouseClick, 1, 1)
	mouseButton(shirei.MouseTertiary, shirei.MouseClick, 2, 2)
	if shirei.GetFrameInput().Mouse != shirei.MouseClick || shirei.GetInputState().MouseButton != shirei.MouseTertiary {
		t.Fatalf("replaced mouse=%v button=%v", shirei.GetFrameInput().Mouse, shirei.GetInputState().MouseButton)
	}

	// Wheel deltas still accumulate and do not touch the button sample.
	*shirei.GetFrameInput() = shirei.FrameInputData{}
	scroll(0, wheelNotchPts)
	scroll(-wheelNotchPts, 0)
	if got := shirei.GetFrameInput().Scroll; got != (shirei.Vec2{-wheelNotchPts, wheelNotchPts}) {
		t.Fatalf("Scroll = %v", got)
	}
	if shirei.GetFrameInput().Mouse != 0 {
		t.Fatalf("wheel set Mouse = %v", shirei.GetFrameInput().Mouse)
	}
}
