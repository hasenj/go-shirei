package shirei

import (
	"testing"
	"time"
)

// Animation channel tests: change a property from X→Y, advance a few frames
// with a non-zero clock, and assert the resolved value is strictly between
// X and Y when that channel is enabled — and equals Y immediately when off.

func TestAnimSizeEases(t *testing.T) {
	testAnimChannel(t, channelCase{
		name:  "size",
		flags: AnimSize,
		// only size on → size eases; also check pos would snap if we moved it
		x: 100, y: 200,
		read: func(id ContainerId) float32 {
			return GetRenderDataOf(id).ResolvedSize[0]
		},
		build: func(target float32, flags AnimFlags) ContainerId {
			var id ContainerId
			// AnimateOnly sets animationsSet — survives parent cascade in Attrs.
			Container(Attrs(AnimateOnly(flags), FixSize(target, 40), Background(0, 0, 50, 1)), func() {
				id = CurrentId()
			})
			return id
		},
	})
}

func TestAnimPosEases(t *testing.T) {
	testAnimChannel(t, channelCase{
		name:  "pos",
		flags: AnimPos,
		x:     10, y: 80,
		read: func(id ContainerId) float32 {
			return GetRenderDataOf(id).RelativeOrigin[0]
		},
		build: func(target float32, flags AnimFlags) ContainerId {
			var id ContainerId
			Container(Attrs(FixSize(200, 100)), func() {
				Container(Attrs(AnimateOnly(flags), Float(target, 10), FixSize(40, 40), Background(0, 0, 50, 1)), func() {
					id = CurrentId()
				})
			})
			return id
		},
	})
}

func TestAnimPadEases(t *testing.T) {
	testAnimChannel(t, channelCase{
		name:  "pad",
		flags: AnimPad,
		x:     4, y: 24,
		read: func(id ContainerId) float32 {
			return GetRenderDataOf(id).Padding[0]
		},
		build: func(target float32, flags AnimFlags) ContainerId {
			var id ContainerId
			Container(Attrs(AnimateOnly(flags), Pad(target), FixSize(80, 80), Background(0, 0, 50, 1)), func() {
				id = CurrentId()
			})
			return id
		},
	})
}

func TestAnimCornersEases(t *testing.T) {
	testAnimChannel(t, channelCase{
		name:  "corners",
		flags: AnimCorners,
		x:     2, y: 20,
		read: func(id ContainerId) float32 {
			return GetRenderDataOf(id).Corners[0]
		},
		build: func(target float32, flags AnimFlags) ContainerId {
			var id ContainerId
			Container(Attrs(AnimateOnly(flags), Corners(target), FixSize(80, 80), Background(0, 0, 50, 1)), func() {
				id = CurrentId()
			})
			return id
		},
	})
}

func TestAnimBorderEases(t *testing.T) {
	testAnimChannel(t, channelCase{
		name:  "border",
		flags: AnimBorder,
		x:     1, y: 12,
		read: func(id ContainerId) float32 {
			return GetRenderDataOf(id).BorderWidth
		},
		build: func(target float32, flags AnimFlags) ContainerId {
			var id ContainerId
			Container(Attrs(AnimateOnly(flags), BorderWidth(target), BorderColor(0, 0, 0, 1),
				FixSize(80, 80), Background(0, 0, 50, 1)), func() {
				id = CurrentId()
			})
			return id
		},
	})
}

func TestAnimAlphaEases(t *testing.T) {
	testAnimChannel(t, channelCase{
		name:  "alpha",
		flags: AnimAlpha,
		x:     0, y: 0.8,
		read: func(id ContainerId) float32 {
			return GetRenderDataOf(id).Transparency
		},
		build: func(target float32, flags AnimFlags) ContainerId {
			var id ContainerId
			Container(Attrs(AnimateOnly(flags), Trans(target), FixSize(80, 80), Background(0, 0, 50, 1)), func() {
				id = CurrentId()
			})
			return id
		},
	})
}

func TestAnimOffSnaps(t *testing.T) {
	// With Animations=0, size jumps to target on the first frame after change.
	ResetInputSession()
	ui.Host.WindowSize = Vec2{400, 300}
	target := float32(100)
	var id ContainerId
	build := func() {
		Container(Attrs(NoAnimate, FixSize(target, 40), Background(0, 0, 50, 1)), func() {
			id = CurrentId()
		})
	}
	RunFrameFn(build)
	RunFrameFn(build) // warm identity
	target = 200
	time.Sleep(8 * time.Millisecond)
	RunFrameFn(build)
	got := GetRenderDataOf(id).ResolvedSize[0]
	if got != 200 {
		t.Fatalf("NoAnimate size: got %v, want snap to 200", got)
	}
}

func TestAnimateOnlyIsolatesChannel(t *testing.T) {
	// Only AnimSize: size eases, padding snaps.
	ResetInputSession()
	ui.Host.WindowSize = Vec2{400, 300}
	sz, pad := float32(100), float32(4)
	var id ContainerId
	build := func() {
		Container(Attrs(AnimateOnly(AnimSize), FixSize(sz, 40), Pad(pad), Background(0, 0, 50, 1)), func() {
			id = CurrentId()
		})
	}
	for i := 0; i < 2; i++ {
		RunFrameFn(build)
	}
	sz, pad = 200, 24
	// a few frames mid-tween
	for i := 0; i < 4; i++ {
		time.Sleep(8 * time.Millisecond)
		RunFrameFn(build)
	}
	rd := GetRenderDataOf(id)
	if rd.ResolvedSize[0] <= 100 || rd.ResolvedSize[0] >= 200 {
		t.Fatalf("size should be mid-tween, got %v", rd.ResolvedSize[0])
	}
	if rd.Padding[0] != 24 {
		t.Fatalf("pad should snap (not in AnimateOnly mask), got %v want 24", rd.Padding[0])
	}
}

type channelCase struct {
	name  string
	flags AnimFlags
	x, y  float32
	read  func(ContainerId) float32
	build func(target float32, flags AnimFlags) ContainerId
}

func testAnimChannel(t *testing.T, c channelCase) {
	t.Helper()
	ResetInputSession()
	ui.Host.WindowSize = Vec2{400, 300}

	target := c.x
	var id ContainerId
	frame := func() {
		id = c.build(target, c.flags)
	}

	// warm at X
	RunFrameFn(frame)
	RunFrameFn(frame)
	if got := c.read(id); abs32(got-c.x) > 0.5 {
		t.Fatalf("%s warm: got %v, want ~%v", c.name, got, c.x)
	}

	// switch to Y, advance a few frames with a controlled clock so rate is
	// modest (wall-clock between tests can make rate=1 and snap in one frame).
	target = c.y
	for i := 0; i < 4; i++ {
		ui.frameStart = time.Now().Add(-16 * time.Millisecond) // → timeDelta ≈ 0.016
		RunFrameFn(frame)
	}
	got := c.read(id)
	lo, hi := c.x, c.y
	if lo > hi {
		lo, hi = hi, lo
	}
	// strictly between (with a little slack for float / cutoff on small deltas)
	eps := float32(0.01)
	if got <= lo+eps || got >= hi-eps {
		t.Fatalf("%s mid-tween: got %v, want strictly between %v and %v", c.name, got, c.x, c.y)
	}

	// with that channel off, next change snaps
	ResetInputSession()
	ui.Host.WindowSize = Vec2{400, 300}
	target = c.x
	frameOff := func() {
		id = c.build(target, 0) // AnimateOnly(0) == NoAnimate for this channel set
	}
	RunFrameFn(frameOff)
	RunFrameFn(frameOff)
	target = c.y
	time.Sleep(8 * time.Millisecond)
	RunFrameFn(frameOff)
	got = c.read(id)
	if abs32(got-c.y) > 0.5 {
		t.Fatalf("%s off: got %v, want snap to %v", c.name, got, c.y)
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
